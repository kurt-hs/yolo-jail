package cli

// stop.go is `yolo stop`: end this workspace's running jail deliberately.
//
// The ordinary lifecycle needs this rarely — a jail lives while any session in it
// does, and the last one to leave tears it down (its keeper does, and --rm sweeps
// the container). `yolo stop` ends every session at once: a wedged session, a
// headless straggler, a jail whose keeper is gone. It is also the first half of the recommended replacement
// series — `yolo stop`, then an ordinary `yolo` launch — which is what every
// message that used to recommend the old `--new` flag now names instead
// (--new was removed in 0.9.0, recorded in CHANGELOG.md: its one-command
// replacement hid the kill).

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// stopRuntimeStopTimeoutSeconds is run.TeardownStopTimeoutSeconds, named here because stopJail's
// runner parameter shadows the run package. One grace, whether the keeper or `yolo stop` ends the
// jail.
const stopRuntimeStopTimeoutSeconds = run.TeardownStopTimeoutSeconds

// stopUsage is what `yolo stop --help` prints.
const stopUsage = `Usage: yolo stop [--force]

Stop this workspace's running jail.

Rarely needed: a jail lives while any session in it does, and quitting the
last one tears it down. This ends every session at once — a wedged session, a
headless straggler, a jail whose keeper is gone — and it is the first half of
the recommended replacement series whenever a change cannot reach a running
jail:

    yolo stop && yolo -- <cmd>

Stopping IS the end of that jail's sessions; the next launch starts fresh. It
returns once the jail's teardown is done, or fails after waiting about two
minutes for a keeper that will not finish. Idempotent: with nothing running it
says so and succeeds.

It waits up to about two minutes for the jail's keeper to finish its teardown,
streaming it, and returns once the keeper is gone. A keeper that outlasts that
wait is reported with its pid, its log and its last line, and the stop fails,
naming yolo stop --force as the escalation.
--force ends the jail now instead: it stops the container without the stop
grace and signals the keeper (SIGTERM, then SIGKILL), so a keeper that will
not finish does not hold the stop up.

On the macos-user backend there is no container: it ends the workspace's
macos-user sessions and the keeper that holds their host services, and
removes what a keeper that died left behind. --force is a container-backend
flag; the macos-user stop already signals its keeper.

Flags:
  -f, --force   End the jail now, without waiting out the keeper's teardown.
  --help, -h    Show this help.

Examples:
  yolo stop                           # release this workspace's jail
  yolo stop --force                   # end it now, keeper included
  yolo stop && yolo -- claude         # the replacement series, in full`

// runStop runs `yolo stop`.
func runStop(args []string) int {
	if answerHelp("stop", args, os.Stdout) {
		return 0
	}
	// Misuse exits non-zero with the pointer to this command's help (unknownflags.go), as `yolo ps`
	// and `yolo prune` do. Without it a flag this stop does not define was dropped and the command
	// reported its normal status — a request believed answered, and nothing done.
	if refuseUnknownFlags("stop", args, []string{"stop", "--force", "-f", "--help", "-h"}, os.Stderr) {
		return 2
	}
	force := stopForceRequested(args)
	ws, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "yolo stop: resolving the workspace: %v\n", err)
		return 1
	}
	// The timing gate rides the env opt-ins only — stop has no --timing of its
	// own to grow, and the person running it is often already asking "why is
	// everything slow".
	p := run.TimingLogFor(ws, os.Getenv, os.Stderr)
	rc := stopJail(os.Stdout, os.Stderr, ws, detectListingRuntime(ws), stopExec, p, force)
	// D12, stop's small case of it: recording and REPORTING are different
	// questions. The spans are already in <ws>/.yolo/host-perf.log; the table
	// prints only when the user asked for it on THIS invocation, which for stop
	// means the global --verbose / -v. An inherited YOLO_VERBOSE=1 (a shell
	// profile's "always on") records in silence — same rule the run pipeline's
	// timingReporting() states, same reason.
	if p != nil && explicitVerbose() {
		// Not dashed: TestUsageListsEveryParsedFlag reads `---`-prefixed
		// literals in handlers as flags, and a report header is not one.
		fmt.Fprintln(os.Stderr, "yolo stop timing:")
		p.Report(os.Stderr, time.Now())
	}
	return rc
}

// stopForceRequested reports whether this invocation asked for the escalated stop. A plain scan:
// stop has one flag of its own, and it consumes no value, so it cannot swallow the token after it.
func stopForceRequested(args []string) bool {
	return slices.Contains(args, "--force") || slices.Contains(args, "-f")
}

// stopExec is realStopExec behind a var, so a test can drive runStop against a fake runtime.
var stopExec = realStopExec

// stopRuntimeInstalled reports whether a runtime's CLI is on PATH, behind a var for the tests.
var stopRuntimeInstalled = func(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// realStopExec runs one runtime command, capturing stdout for the probes that
// read it. Returns (stdout, ran, exit code).
func realStopExec(argv []string) (string, bool, int) {
	var out strings.Builder
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String(), true, ee.ExitCode()
		}
		return "", false, 1
	}
	return out.String(), true, 0
}

// stopJail is stop's testable core. A RUNNING container is stopped gracefully
// (the runtime's own TERM-then-KILL discipline; the launcher's --rm removes
// the container as it exits, so the next launch is fresh by construction). A
// stopped or absent container is SUCCESS: stop is idempotent, and any leftover
// is the next launch's stale-removal job, not this command's.
//
// rt is the runtime the config and YOLO_RUNTIME resolve. The one the jail's keeper recorded
// launching it on goes ahead of it (launchedRuntime, JL-D79): a jail launched with
// YOLO_RUNTIME=container in a workspace whose default is podman runs where podman cannot see it.
//
// p is the optional timing collector; a nil p makes every span a no-op, which
// is the everyday off state. force is `yolo stop --force`: the container stop drops
// its grace and the keeper's teardown is not waited out (finishStop signals instead).
func stopJail(stdout, stderr io.Writer, ws, rt string,
	run func(argv []string) (string, bool, int), p *perf.Log, force bool) int {
	launched, known := launchedRuntime(ws)
	if known && launched != rt {
		if rt != "" {
			fmt.Fprintf(stdout, "This workspace's jail was launched on %s, so this stop asks %s, not %s.\n",
				launched, launched, rt)
		}
		rt = launched
	}
	if rt == "" {
		fmt.Fprintln(stderr, "yolo stop: no container runtime found (podman / container).")
		return 1
	}
	if rt == "macos-user" {
		// NO CONTAINER, AND STILL SOMETHING TO STOP (docs/design/jail-lifetime-last-session-wins.md
		// JL-D44): the workspace's macos-user keeper and every session of it, the remedy a refused
		// arrival at an unkept key names. It signals them, streams the keeper's teardown, and removes
		// what a dead keeper left. --force is a container-backend flag: this path already signals its
		// keeper (StopMacosUser).
		return stopMacosUser(stdout, stderr, ws)
	}
	cname := runtime.FromWorkspace(ws)

	// Is anything actually running? `stop` on a non-existent container errors,
	// and an erroring stop would make the stop-then-launch series fail on its
	// first, idempotent half.
	sp := p.Span("stop.inspect")
	running, ran, rc := probeJailRunning(rt, cname, run)
	sp.End()
	if !ran {
		fmt.Fprintf(stderr, "yolo stop: the %s runtime could not be run: `%s` is not on PATH, or would not "+
			"start. Put it on PATH and run `yolo stop` again.\n", rt, rt)
		return 1
	}
	if rt == "container" && rc != 0 { // parity: HonoredBy — `container ls` is AC's answer, and a failed one is "could not ask", where podman's failed inspect is "no such container"
		fmt.Fprintf(stderr, "yolo stop: `container ls` failed (rc %d), so whether this workspace's jail "+
			"(%s) is running is not known. `container ls` shows why; `container stop %s` stops the jail.\n",
			rc, cname, cname)
		return 1
	}
	// The keeper's log as it stands before the stop, so the teardown streamed below is the one this
	// stop causes (run.FinishStop).
	logFrom := keeperLogOffset(ws)
	if !running {
		// A jail whose container is gone and whose keeper is still ending it: the stop waits for that
		// teardown, as it waits for the one it causes, so the next launch finds it done.
		if keeperAlive(ws) {
			fmt.Fprintf(stdout, "This workspace's jail (%s) is already ending.\n", cname)
			return finishStop(stdout, stderr, ws, rt, logFrom, stopCapture(stderr), force)
		}
		// A container of this name that EXISTS but is not running is not "no jail": it holds the
		// name, so the next launch's create is refused (the wedge of a --rm container whose runtime
		// never removed it). End it, or report that it would not go.
		if state, exists, answered := probeJailExists(rt, cname, run); answered && exists {
			return endLeftoverJail(stdout, stderr, rt, cname, state, force, run, p)
		}
		fmt.Fprintf(stdout, "No jail running for this workspace (%s).\n", cname)
		if !known {
			noteOtherRuntimes(stdout, rt, cname)
		}
		return 0
	}

	// Why, for every session the stop ends, recorded before it (recordYoloStop): each prints it
	// once its own session is cut short.
	recordYoloStop(cname)
	sp = p.Span("stop.stop_container")
	// The keeper stops a container with the same grace this does (run.TeardownStopTimeoutSeconds):
	// whatever ends a jail, the runtime gives it the same moment to exit before SIGKILL. Apple
	// Container's `container stop` takes no grace, so only podman gets the flag. --force gives the
	// runtime no grace at all: the jail ends now, and the keeper is signalled in finishStop.
	stopGrace := stopRuntimeStopTimeoutSeconds
	if force {
		stopGrace = 0
	}
	stopArgv := []string{rt, "stop", cname}
	if rt != "container" {
		stopArgv = []string{rt, "stop", "-t", strconv.Itoa(stopGrace), cname}
	}
	_, ran, rc = run(stopArgv)
	sp.End()
	if !ran {
		fmt.Fprintf(stderr, "yolo stop: `%s stop %s` failed (rc %d); any message the runtime printed above "+
			"says why. `yolo stop` again retries it.\n", rt, cname, rc)
		return 1
	}
	if rc != 0 {
		// A NON-ZERO EXIT IS NOT BY ITSELF A FAILED STOP (JL-D82). Podman's SIGKILL/--rm path
		// commonly exits 125 with the container already gone; judged on the exit status alone this
		// would skip the keeper's teardown and report a jail that in fact ended. Judge it the way the
		// keeper does: by the existence PROBE. Only a container the runtime still reports running is
		// a failed stop; one it cannot answer for is not proof of anything (JL-P3: "could not ask" is
		// never "gone"), and is reported as the failure it may be.
		if !probeJailGoneAfterStop(rt, cname, run) {
			fmt.Fprintf(stderr, "yolo stop: `%s stop %s` failed (rc %d); any message the runtime printed "+
				"above says why. `yolo stop` again retries it.\n", rt, cname, rc)
			return 1
		}
	}
	// THE TEARDOWN, before the stop returns (docs/design/jail-lifetime-last-session-wins.md JL-D25):
	// the jail's keeper runs it and this streams it, and a jail whose keeper died gets it from here
	// (JL-D30). Returning at the runtime's stop would let the next launch, or a `yolo config diff`,
	// meet a teardown still running.
	sp = p.Span("stop.keeper_teardown")
	frc := finishStop(stdout, stderr, ws, rt, logFrom, stopCapture(stderr), force)
	sp.End()
	if frc != 0 {
		return frc
	}
	fmt.Fprintf(stdout, "Stopped %s. The next yolo launch starts fresh.\n", cname)
	return 0
}

// finishStop is run.FinishStop behind a var, so a test can pin that the stop reaches it.
var finishStop = run.FinishStop

// stopMacosUser is run.StopMacosUser behind a var, so a test can pin that a macos-user stop reaches
// it.
var stopMacosUser = run.StopMacosUser

// probeJailRunning asks rt whether cname is running. ran is false when the runtime could not be run, and
// rc is its exit status. Podman answers a Go-template inspect, and a non-zero status there is "no
// such container". Apple Container's `container inspect` takes no --format, so the template read
// nothing there and every AC jail looked stopped (G11, docs/plans/setup-support-gaps.md); its
// `container ls` lists the running containers, the question the attach decision asks it too
// (probeRunningContainer), and a non-zero status there is "could not ask".
func probeJailRunning(rt, cname string, run func(argv []string) (string, bool, int)) (running, ran bool, rc int) {
	if rt == "container" { // parity: HonoredBy — `container ls` lists the running containers, the question podman's template inspect answers
		out, ran, rc := run([]string{"container", "ls"})
		_, live := runtime.ParseContainerLsLive(out)[cname]
		return ran && rc == 0 && live, ran, rc
	}
	state, ran, rc := run([]string{rt, "inspect", "--format", "{{.State.Running}}", cname})
	return ran && rc == 0 && strings.TrimSpace(state) == "true", ran, rc
}

// probeJailGoneAfterStop answers, for a stop whose runtime stop returned non-zero, whether cname's
// jail is gone: run.JailGoneAfterItsEnd over this file's own runtime runner. A runtime that could
// not be run, or (Apple Container) could not answer, is "could not ask" and never "gone" (JL-P3),
// as the pre-stop probe reads it, so the caller reports the possible failure it is. Apple
// Container's `container ls` also keeps listing a jail for a moment after its stop returns
// (JL-D83), which the bounded wait covers; podman's one answer stands, so it is asked once.
func probeJailGoneAfterStop(rt, cname string, runCmd func(argv []string) (string, bool, int)) bool {
	return run.JailGoneAfterItsEnd(rt, func() (running, answered bool) {
		running, ran, rc := probeJailRunning(rt, cname, runCmd)
		if !ran || (rt == "container" && rc != 0) { // parity: HonoredBy — a failed `container ls` is "could not ask", where podman's failed inspect is "no such container"
			return false, false
		}
		return running, true
	})
}

// probeJailExists asks rt whether a container of cname's name exists in ANY state, and in which
// state: the gate that stops `yolo stop` calling a container that exists but is not running "no
// jail". The state is the runtime's own word ("exited", "created", "stopped"). answered is false
// when the runtime could not be asked; exists is true only when it answered and the container is
// there. Podman answers a Go-template inspect, whose non-zero status is its "no such object";
// Apple Container's `container ls` lists running ONLY, so existence is its `--all` listing, whose
// STATE column names the state.
func probeJailExists(rt, cname string, run func(argv []string) (string, bool, int)) (state string, exists, answered bool) {
	if rt == "container" { // parity: HonoredBy — `container ls` lists running containers only; `--all` is the existence question `podman ps -a` answers
		out, ran, rc := run([]string{"container", "ls", "--all"})
		if !ran || rc != 0 {
			return "", false, false
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.Fields(line)
			if len(parts) == 0 || parts[0] != cname {
				continue
			}
			if len(parts) > 4 {
				return parts[4], true, true
			}
			return "present", true, true
		}
		return "", false, true
	}
	out, ran, rc := run([]string{rt, "inspect", "--format", "{{.State.Status}}", cname})
	if !ran {
		return "", false, false
	}
	if rc != 0 { // podman's inspect of an absent container exits non-zero ("no such object")
		return "", false, true
	}
	state = strings.TrimSpace(out)
	if state == "" {
		state = "unknown"
	}
	return state, true, true
}

// endLeftoverJail ends a container of the jail's name that EXISTS but is not running, so it cannot
// hold the name against the next launch. force escalates to the runtime's forceful removal
// (`rm --force`), which is what a container whose process the OCI runtime cannot kill needs. The
// removal is judged by the existence probe, never by its own exit status (JL-D82); a container it
// cannot remove is reported as the failure it is, with the next step named.
func endLeftoverJail(stdout, stderr io.Writer, rt, cname, state string, force bool,
	run func(argv []string) (string, bool, int), p *perf.Log) int {
	fmt.Fprintf(stdout, "The jail container of this workspace (%s) exists in state %q and would block "+
		"the next launch; ending it.\n", cname, state)
	sp := p.Span("stop.remove_leftover")
	argv := []string{rt, "rm", cname}
	if force {
		argv = []string{rt, "rm", "--force", cname}
	}
	_, ran, rc := run(argv)
	sp.End()
	now, exists, answered := probeJailExists(rt, cname, run)
	if answered && !exists {
		fmt.Fprintf(stdout, "Ended the leftover container %s. The next yolo launch starts fresh.\n", cname)
		return 0
	}
	if !ran {
		fmt.Fprintf(stderr, "yolo stop: `%s rm %s` could not be run, so the container that would block "+
			"the next launch may still be there. %s\n", rt, cname, leftoverRemoveHint(rt, cname))
		return 1
	}
	if !answered {
		fmt.Fprintf(stderr, "yolo stop: `%s rm %s` ran (rc %d), but whether the container is gone could "+
			"not be asked. %s\n", rt, cname, rc, leftoverRemoveHint(rt, cname))
		return 1
	}
	fmt.Fprintf(stderr, "yolo stop: %s's container is still there in state %q after `%s rm %s` "+
		"(rc %d), so the next launch would be refused. %s\n", cname, now, rt, cname, rc,
		leftoverRemoveHint(rt, cname))
	return 1
}

// leftoverRemoveHint names, for a leftover container the runtime would not remove, the next step —
// and, first, the class worth ruling out: a container whose PID NAMESPACE has not drained. The
// kernel will not finish a container's teardown while any task remains in its PID namespace, and
// that task can be a ZOMBIE (Z) whose parent is a `podman exec`'s conmon — a zombie still counts,
// and podman cannot reap another process's zombie. A launcher stopped mid-attach (Tl, e.g. `yolo -p
// … -- pi` under a Ctrl-Z) leaves exactly that, and the container then refuses both stop and rm
// while `podman kill -s KILL` times out. Inspecting for the Tl launcher and the Z task, and killing
// the exec's conmon (which reparents and reaps the zombie), is what lets the namespace finish; a
// runtime restart (and then the removal by force) clears the rest.
func leftoverRemoveHint(rt, cname string) string {
	class := " The container's PID namespace may not have drained: a task inside it (often a zombie " +
		"whose parent is a `podman exec`'s conmon) is pinning it, and podman cannot reap another " +
		"process's zombie. Inspect with `ps -o pid,ppid,stat,cmd` for a stopped launcher (Tl) and a Z " +
		"task; killing the exec's conmon lets the namespace finish."
	if rt == "container" {
		return class + fmt.Sprintf(" Restart Apple Container, then `container rm --force %s`.", cname)
	}
	return class + fmt.Sprintf(" Restart the runtime (`systemctl --user restart podman`), then `podman rm -f %s`.", cname)
}

// launchedRuntime is the container runtime the workspace's jail was launched on, from its keeper's
// start record (run.LaunchedRuntime); ok is false when no record names one of them.
func launchedRuntime(ws string) (string, bool) {
	rt, ok := run.LaunchedRuntime(ws)
	if !ok || !slices.Contains(paths.SupportedRuntimes, rt) {
		return "", false
	}
	return rt, true
}

// noteOtherRuntimes is a no-op stop that cannot know which runtime launched the jail it tracks: no
// keeper's start record names one, and a tracking file says a jail of this name was launched. Each
// other container runtime installed here is a place that jail can run, so it names the stop that
// asks there.
func noteOtherRuntimes(stdout io.Writer, rt, cname string) {
	if _, tracked := runtime.ReadContainerWorkspace(cname); !tracked {
		return
	}
	for _, other := range paths.SupportedRuntimes {
		if other != rt && stopRuntimeInstalled(other) {
			fmt.Fprintf(stdout, "If it was launched on %s, `YOLO_RUNTIME=%s yolo stop` stops it.\n", other, other)
		}
	}
}

// stopCapture is the E3 config capture `yolo stop` hands the reap of an unkept jail, as a launch is
// handed it (commands.go).
func stopCapture(stderr io.Writer) func(workspace, rt string) {
	return func(workspace, rt string) {
		captureOnTerminate(workspace, rt, func(msg string) { fmt.Fprintln(stderr, "Warning: "+msg) })
	}
}

// recordYoloStop records, for the sessions a stop of cname's jail ends, that `yolo stop` ended it
// (run.RecordJailStop). Outside stopJail because that function's runner parameter is named run.
func recordYoloStop(cname string) {
	run.RecordJailStop(cname, run.YoloStopReason(os.Getpid()))
}

// keeperLogOffset and keeperAlive are run.KeeperLogOffset and run.KeeperAlive, outside stopJail for
// recordYoloStop's reason.
func keeperLogOffset(ws string) int64 { return run.KeeperLogOffset(ws) }

func keeperAlive(ws string) bool { return run.KeeperAlive(ws) }
