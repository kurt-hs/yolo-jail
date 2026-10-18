package integration

// preparationworkers_test.go stops, at the end of every test that launched yolo, every detached
// process that launch left running under the test's temp HOME, and waits for each to be gone.
//
// # What a launch leaves behind
//
// A launch on the HOST (YOLO_VERSION unset, which is CI) whose selected packs declare a
// program prepares it through yolo's background preparation (internal/cli's
// preparationscheduler.go and shareddemand.go): it registers the program with the long-lived
// `yolo internal preparation-scheduler`, starting one if none runs, and waits for a detached
// `yolo internal background-advance --shared-demand` worker to answer. Both outlive the launch
// by design, and both run with the launch's environment: in this suite, a temp HOME that
// t.TempDir deletes when the test ends. Inventoried on 2026-10-16 by running
// TestAnUnsatisfiableNodeFloorRefusesTheLaunch with YOLO_VERSION unset and listing every process
// whose environment names the test's temp dir once the launch had returned: the scheduler
// (still alive after the test binary exited, its cwd the deleted workspace) and two
// shared-demand workers, one still finishing after the refusal reached the terminal.
//
// A writer like that, writing through a HOME that is being deleted, recreates a directory under
// t.TempDir's RemoveAll: CI run 38856922948 failed that test with only
// `TempDir RemoveAll cleanup: unlinkat <tempdir>: directory not empty`. A jail never shows it
// (registerOrdinaryPreparations and waitOrdinaryPreparation stay in the foreground when
// config.InJail), which is why it surfaces in CI alone.
//
// # Why not by the locks they hold
//
// This stop used to find the workers through the pid locks under the HOME's background-advance
// directory. CI run 39096056377 failed TestPlainBedrockCarriesOmpThroughTheBridge the same way
// after that stop had named two pids, and a listing of every process under the test's HOME just
// before and after the stop, taken with YOLO_VERSION unset in a jail on 2026-10-18, showed why:
// before it, the scheduler, the shared-demand worker the launch spawned (running a
// `program-capture-jail`, with its keeper, `podman run`, `podman exec`, conmon and
// fuse-overlayfs under it), and a SECOND shared-demand worker for the same key that the
// scheduler had spawned; after it, that second worker, which held no lock at the instant of the
// walk, still running, reparented to pid 1. A lock records a holder only while it is held, so a
// worker between locks, or just spawned, or a child doing the work, is invisible to it.
//
// # How a process is identified as this test's
//
// By HOME, read from /proc as homedaemons_test.go does: every process whose environment carries
// HOME=<this test's temp home>. The HOME is unique to the test, so a match cannot be another
// test's process or the machine's. Two kinds are left out: the test binary's own descendants,
// which are the exec.Cmd children its own cleanups own (a stand-in, a session it still holds),
// and a scratch remover (`internal scratch-rm`), which ends on its own within its bound and is
// WAITED for, never stopped, since stopping it leaks the volumes it was removing. On a host
// without /proc nothing is identified and nothing is stopped.
//
// # The order, and why it repeats
//
// The stop works TOP DOWN, in rounds: each round takes one listing, signals the processes in it
// whose parent is not itself in it (the roots), and waits for them. yolo's own processes go
// first, so a worker gets to tear down the capture jail it runs, through its keeper, before
// anything under that jail is touched; podman's conmon and fuse-overlayfs, reparented to pid 1
// and so roots from the start, are signalled only once no yolo process is left. A child that
// outlives its parent is a root in the next round. The rounds repeat until a listing comes back
// empty, so a process spawned while the stop runs (the scheduler starting a worker as it winds
// down) is stopped too, and the stop returns only once nothing under the HOME is alive. The
// whole stop is bounded: what is still alive at the bound is killed and reported as an error.
//
// The signal is SIGTERM, which every yolo background process takes as a request to wind down
// (internal/cli's backgroundSignals), and SIGKILL only after a grace long enough for a worker
// to end the jail it was running.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// preparationWorkerTermGrace bounds how long a stopped process may take to end what it runs, a
// capture jail included, before it is killed.
const preparationWorkerTermGrace = 30 * time.Second

// homeProcessStopBound bounds the whole stop: several rounds of the grace above, and a scratch
// remover's own minute (detachedWriterWait).
const homeProcessStopBound = 3 * time.Minute

// homeProcess is one live process running under the test's HOME.
type homeProcess struct {
	pid, ppid int
	args      []string
}

// yoloVerb is the verb of a yolo self-exec'd process (`<exe> internal <verb> …`), read the way
// homeHostDaemons reads `internal daemon <name>`: the argument after the first `internal`.
func (p homeProcess) yoloVerb() (string, bool) {
	for i := 0; i+1 < len(p.args); i++ {
		if p.args[i] == "internal" {
			return p.args[i+1], true
		}
	}
	return "", false
}

func (p homeProcess) isYolo() bool { _, ok := p.yoloVerb(); return ok }

// waitOnly reports a scratch remover, which is waited for and never signalled.
func (p homeProcess) waitOnly() bool { v, _ := p.yoloVerb(); return v == "scratch-rm" }

// String names the process for a log line: "pid <n> (<what>)".
func (p homeProcess) String() string {
	what := ""
	if v, ok := p.yoloVerb(); ok {
		what = "internal " + v
		// A daemon is named by the argument after the verb: a capture jail's keeper is one.
		if i := slices.Index(p.args, "daemon"); v == "daemon" && i >= 0 && i+1 < len(p.args) {
			what += " " + p.args[i+1]
		}
	} else if len(p.args) > 0 {
		what = filepath.Base(p.args[0])
	}
	return fmt.Sprintf("pid %d (%s)", p.pid, what)
}

// processParent is pid's parent, from the fourth field of /proc/<pid>/stat; 0 when unreadable.
func processParent(pid int) int {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}

// homeProcesses lists the live processes running with HOME=home, other than this test binary
// and its descendants.
func homeProcesses(home string) []homeProcess {
	if home == "" || goruntime.GOOS != "linux" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	parent := map[int]int{}
	var mine []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		parent[pid] = processParent(pid)
		if pid != os.Getpid() && !processGone(pid) && processRunsWithHome(pid, home) {
			mine = append(mine, pid)
		}
	}
	ownDescendant := func(pid int) bool {
		for seen := 0; pid > 1 && seen < 64; seen++ {
			if pid == os.Getpid() {
				return true
			}
			pid = parent[pid]
		}
		return false
	}
	var out []homeProcess
	for _, pid := range mine {
		if ownDescendant(pid) {
			continue
		}
		args, _ := processArgs(pid)
		out = append(out, homeProcess{pid: pid, ppid: parent[pid], args: args})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

// stopHomeProcesses stops every process left running under home (see the file header for which,
// and in what order), waits for each to be gone, and names the ones it signalled. It returns only
// once a listing finds nothing under home, or at homeProcessStopBound, when it kills what is left
// and says so in its error.
func stopHomeProcesses(home string) ([]string, error) {
	return stopHomeProcessesWithin(home, homeProcessStopBound, preparationWorkerTermGrace)
}

func stopHomeProcessesWithin(home string, bound, grace time.Duration) ([]string, error) {
	var stopped []string
	deadline := time.Now().Add(bound)
	for {
		procs := homeProcesses(home)
		if len(procs) == 0 {
			return stopped, nil
		}
		if time.Now().After(deadline) {
			var left []string
			for _, p := range procs {
				_ = syscall.Kill(p.pid, syscall.SIGKILL)
				left = append(left, p.String())
			}
			for _, p := range procs {
				awaitProcessGone(p.pid, 2*time.Second)
			}
			return stopped, fmt.Errorf("still running under %s %s after the stop began, and killed: %s",
				home, bound, strings.Join(left, ", "))
		}
		targets := stopRound(procs)
		if len(targets) == 0 {
			// Only scratch removers are left: they end on their own.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		for _, p := range targets {
			_ = syscall.Kill(p.pid, syscall.SIGTERM)
			stopped = append(stopped, p.String())
		}
		roundEnd := time.Now().Add(grace)
		for _, p := range targets {
			for !processGone(p.pid) && time.Now().Before(roundEnd) {
				time.Sleep(50 * time.Millisecond)
			}
		}
		for _, p := range targets {
			if !processGone(p.pid) {
				_ = syscall.Kill(p.pid, syscall.SIGKILL)
				awaitProcessGone(p.pid, 2*time.Second)
			}
		}
	}
}

// stopRound picks one round's targets from a listing: its roots (no parent in the listing),
// yolo's own processes first, and never a scratch remover.
func stopRound(procs []homeProcess) []homeProcess {
	in := map[int]bool{}
	for _, p := range procs {
		in[p.pid] = true
	}
	var yolo, other []homeProcess
	for _, p := range procs {
		if p.waitOnly() || in[p.ppid] {
			continue
		}
		if p.isYolo() {
			yolo = append(yolo, p)
		} else {
			other = append(other, p)
		}
	}
	if len(yolo) > 0 {
		return yolo
	}
	return other
}

// --- the tests ---

// standInScript is a stand-in's body: it sleeps until a SIGTERM, which it takes as a real
// process does, ending its own child with it.
const standInScript = `trap 'kill $!; exit 143' TERM; sleep 600 & wait`

// startStandIn starts `sh -c script args...` with argv[0] argv0 and HOME=home (plus env),
// DETACHED — the shell that starts it exits at once, so it is no child of this test binary, as a
// real worker is not once the launch that spawned it has returned — and returns its pid once
// /proc shows that HOME. The test's end stops it if the code under test did not.
func startStandIn(t *testing.T, home string, env []string, argv0, script string, args ...string) int {
	t.Helper()
	if goruntime.GOOS != "linux" {
		t.Skip("the harness identifies a process's HOME through /proc, which this host lacks " +
			"(preparationworkers_test.go's header); it stops nothing here, so there is nothing to test")
	}
	outer := `a0=$1; s=$2; shift 2; (exec -a "$a0" sh -c "$s" "$@" </dev/null >/dev/null 2>&1) & echo $!`
	out, err := runEnvCommand(append([]string{"HOME=" + home}, env...),
		append([]string{"bash", "-c", outer, "_", argv0, script}, args...)...)
	if err != nil {
		t.Fatalf("starting the stand-in %s %v: %v", argv0, args, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("the stand-in %s %v printed no pid: %q", argv0, args, out)
	}
	t.Cleanup(func() { stopStandIn(pid) })
	for deadline := time.Now().Add(10 * time.Second); !processRunsWithHome(pid, home) || !standInShows(pid, argv0); {
		if time.Now().After(deadline) {
			t.Fatalf("the stand-in %s %v (pid %d) never showed HOME=%s", argv0, args, pid, home)
		}
		time.Sleep(time.Millisecond)
	}
	return pid
}

// standInShows reports that pid's argv[0] is argv0 yet, so the exec has landed.
func standInShows(pid int, argv0 string) bool {
	args, err := processArgs(pid)
	return err == nil && len(args) > 0 && args[0] == argv0
}

// stopStandIn ends a stand-in the code under test left alive: SIGTERM, so it takes its sleep
// with it, then SIGKILL.
func stopStandIn(pid int) {
	if pid <= 0 || processGone(pid) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if !awaitProcessGone(pid, 5*time.Second) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		awaitProcessGone(pid, 5*time.Second)
	}
}

// runEnvCommand runs argv with env added to this process's environment and returns its stdout.
func runEnvCommand(env []string, argv ...string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), fmt.Errorf("%v: %s", err, exit.Stderr)
	}
	return string(out), err
}

// Every process under this home is stopped, holding a lock or not, yolo's own first; one under
// another home, and this test binary's own child under this home, are left alone.
func TestStopHomeProcessesStopsEveryoneUnderTheHome(t *testing.T) {
	mine, theirs := t.TempDir(), t.TempDir()
	scheduler := startStandIn(t, mine, nil, "sh", standInScript, "internal", "preparation-scheduler")
	worker := startStandIn(t, mine, nil, "sh", standInScript, "internal", "background-advance", "--shared-demand")
	conmon := startStandIn(t, mine, nil, "conmon", standInScript)
	other := startStandIn(t, theirs, nil, "sh", standInScript, "internal", "preparation-scheduler")
	child := fakeHostDaemonProcess(t, "the test's own child", mine).Process.Pid

	stopped, err := stopHomeProcessesWithin(mine, 30*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		fmt.Sprintf("pid %d (internal preparation-scheduler)", scheduler),
		fmt.Sprintf("pid %d (internal background-advance)", worker),
	}
	if worker < scheduler {
		want[0], want[1] = want[1], want[0]
	}
	want = append(want, fmt.Sprintf("pid %d (conmon)", conmon))
	if fmt.Sprint(stopped) != fmt.Sprint(want) {
		t.Errorf("stopped = %v, want %v: yolo's own processes, then conmon once they were gone", stopped, want)
	}
	for _, pid := range []int{scheduler, worker, conmon} {
		if !processGone(pid) {
			t.Errorf("process %d running under %s is still alive", pid, mine)
		}
	}
	if left := homeProcesses(mine); len(left) > 0 {
		t.Errorf("the stop returned with processes still under the home: %v", left)
	}
	if processGone(other) {
		t.Errorf("a process running under ANOTHER home (%s) was stopped", theirs)
	}
	if processGone(child) {
		t.Error("this test binary's own child, which its own cleanup owns, was stopped")
	}
	if stopped, err := stopHomeProcesses(filepath.Join(mine, "absent")); err != nil || len(stopped) != 0 {
		t.Errorf("a home nothing runs under: stopped %v (%v), want nothing", stopped, err)
	}
}

// A scratch remover is waited for and never signalled: stopping it would leak the volumes it is
// removing. This one ignores SIGTERM, so a signal would show as a stop that took its grace.
func TestStopHomeProcessesWaitsForAScratchRemover(t *testing.T) {
	home := t.TempDir()
	remover := startStandIn(t, home, nil, "sh", `trap '' TERM; sleep 1; :`, "internal", "scratch-rm")

	stopped, err := stopHomeProcessesWithin(home, 30*time.Second, 5*time.Second)
	if err != nil || len(stopped) != 0 {
		t.Errorf("stopped %v (%v), want the remover waited for and nothing signalled", stopped, err)
	}
	if !processGone(remover) {
		t.Error("the stop returned while the scratch remover was still running")
	}
}

// A process spawned WHILE the stop runs is stopped too, since the stop repeats until a listing is
// empty: this stand-in scheduler starts a worker as it winds down, detached, as a real one can.
func TestStopHomeProcessesStopsWhatIsSpawnedDuringTheStop(t *testing.T) {
	home := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "late.pid")
	scheduler := startStandIn(t, home,
		[]string{"LATE_PID=" + pidFile, "WORKER=" + standInScript},
		"sh", `trap '(sh -c "$WORKER" internal background-advance </dev/null >/dev/null 2>&1 & echo $! > "$LATE_PID"); kill $!; exit 143' TERM; sleep 600 & wait`,
		"internal", "preparation-scheduler")
	t.Cleanup(func() {
		if raw, err := os.ReadFile(pidFile); err == nil {
			late, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
			stopStandIn(late)
		}
	})

	stopped, err := stopHomeProcessesWithin(home, 30*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the stand-in scheduler spawned no worker as it wound down (%v); stopped %v", err, stopped)
	}
	late, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	want := []string{
		fmt.Sprintf("pid %d (internal preparation-scheduler)", scheduler),
		fmt.Sprintf("pid %d (internal background-advance)", late),
	}
	if fmt.Sprint(stopped) != fmt.Sprint(want) {
		t.Errorf("stopped = %v, want %v", stopped, want)
	}
	if !processGone(late) {
		t.Errorf("the worker spawned during the stop (pid %d) outlived it", late)
	}
	if left := homeProcesses(home); len(left) > 0 {
		t.Errorf("the stop returned with processes still under the home: %v", left)
	}
}

// THE CALL SITE, BY BEHAVIOR: a runCommand launch's cleanup stops a preparation worker left
// running under the launch's HOME, holding no lock. The launch is `yolo --version`, since what is
// pinned is the harness's stop and not the spawn. Delete the stopHomeProcesses call from
// awaitDetachedWriters and the stand-in outlives the subtest.
func TestALaunchesCleanupStopsItsHomesPreparationWorkers(t *testing.T) {
	requireJail(t)
	ws := t.TempDir()
	worker := startStandIn(t, os.Getenv("HOME"), nil, "sh", standInScript,
		"internal", "background-advance", "--shared-demand")
	t.Run("launch", func(t *testing.T) {
		if res := runYoloCLI(t, ws, "--version"); res.rc != 0 {
			t.Fatalf("yolo --version: rc=%d\n%s", res.rc, res.combined())
		}
	})
	if !processGone(worker) {
		t.Error("a preparation worker running under the launch's HOME outlived the launch's " +
			"cleanup: awaitDetachedWriters no longer calls stopHomeProcesses")
	}
}
