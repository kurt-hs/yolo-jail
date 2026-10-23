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
// By HOME, read from the process's environment as homedaemons_test.go does: every process whose
// environment carries HOME=<this test's temp home>. The HOME is unique to the test, so a match
// cannot be another test's process or the machine's. The process's start time ties the listed pid
// to the same process before a signal, rather than trusting the pid alone. Linux reads all three
// (environment, parent, start time) from /proc; darwin, which has no /proc, from the kernel's
// process table (procinfo_darwin_test.go: kern.proc and kern.procargs2). A process first seen while already
// exiting, its environment released between the two HOME reads (CI run 39886931703: a late
// podman), is never confirmed as this test's; it is waited for, never signalled, once the kernel
// proves it exiting, and anything else unconfirmed is refused. One whose environment stops
// naming the HOME between the reads, by exec'ing a program with a new one (CI run 40012293896:
// a podman child becoming its OCI runtime), is no longer this test's: it is waited for briefly,
// never signalled, and never fails the stop (homeDeparted). Two kinds are left out: the test binary's
// own descendants, which are the exec.Cmd children its own cleanups own (a stand-in, a session it
// still holds), and a scratch remover (`internal scratch-rm`), which ends on its own within its
// bound and is WAITED for, never stopped, since stopping it leaks the volumes it was removing.
//
// DARWIN USED TO BE LEFT OUT, and failed the same way: with only /proc to read, a Mac identified
// nothing and stopped nothing, so the scheduler and its workers outlived the test there, and the
// macOS nightly's run 39943557792 failed TestAnUnsatisfiableNodeFloorRefusesTheLaunch with
// `TempDir RemoveAll cleanup: unlinkat <tempdir>/004/.local/share: directory not empty`. The
// proof that runs on a Mac is the stand-in tests below, which startStandIn no longer skips there
// and ci.yml's check-macos job runs under -short.
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
// down) is stopped too. Ordinarily the stop returns only once nothing under the HOME is alive;
// if its bound expires or a live process cannot be classified safely, it returns an error instead.
// The whole stop is bounded: anything still alive at the bound is reported as an error; only
// targets safely classified for signalling are killed.
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

// PF_EXITING is Linux stat field 9 bit 0x4 (include/linux/sched.h).
const linuxPFExiting uint64 = 0x4

// homeProcess is one live process running under the test's HOME.
type homeProcess struct {
	pid, ppid   int
	args        []string
	argsErr     error
	startTime   string
	statErr     error
	homeErr     error
	lateHomeErr error
	homeMatched bool
	exiting     bool
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

// exitingIdentity names a retained exiting process for a bound report, saying when its HOME was
// never confirmed (provenExitingStopTarget), so the report does not claim it as this test's.
func (p homeProcess) exitingIdentity() string {
	if p.homeMatched {
		return p.String()
	}
	return p.String() + " (HOME ownership unconfirmed: its environment was already released when first read)"
}

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

// processHomeObservation reads pid's HOME from its environment (processEnviron: /proc on Linux,
// kern.procargs2 on darwin) and reports whether it is home.
func processHomeObservation(pid int, home string) (bool, error) {
	env, err := processEnviron(pid)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errHomeObservationUnknown, err)
	}
	if len(env) == 0 {
		return false, fmt.Errorf("%w: %w: pid %d has an empty environment", errHomeOwnershipChanged, errHomeEnvironmentCleared, pid)
	}
	want := "HOME=" + filepath.Clean(home)
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			if "HOME="+filepath.Clean(strings.TrimPrefix(kv, "HOME=")) == want {
				return true, nil
			}
			return false, fmt.Errorf("%w: pid %d now has a different HOME", errHomeOwnershipChanged, pid)
		}
	}
	return false, fmt.Errorf("%w: pid %d no longer has HOME", errHomeOwnershipChanged, pid)
}

// homeProcesses lists the live processes running with HOME=home, other than this test binary
// and its descendants.
func homeProcesses(home string) []homeProcess {
	if home == "" {
		return nil
	}
	pids, err := processPIDs()
	if err != nil {
		return nil
	}
	parent := map[int]int{}
	var mine []int
	for _, pid := range pids {
		parent[pid] = processParent(pid)
		// The HOME read first: it is one kernel read, where processGone execs ps per pid on darwin.
		if pid != os.Getpid() && processRunsWithHome(pid, home) && !processGone(pid) {
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
		args, argsErr := processArgs(pid)
		startTime, statErr := processStartTime(pid)
		matchesHome, homeErr := processHomeObservation(pid, home)
		if homeErr == nil && !matchesHome {
			homeErr = fmt.Errorf("%w: pid %d no longer has HOME=%s", errHomeOwnershipChanged, pid, home)
		}
		verifiedStartTime, verifiedStatErr := processStartTime(pid)
		if statErr == nil && verifiedStatErr != nil {
			statErr = verifiedStatErr
		} else if statErr == nil && startTime != verifiedStartTime {
			statErr = errors.New("process identity changed while reading its HOME and arguments")
		}
		if processGone(pid) {
			continue
		}
		out = append(out, homeProcess{
			pid: pid, ppid: parent[pid], args: args, argsErr: argsErr,
			startTime: startTime, statErr: statErr, homeErr: homeErr,
			homeMatched: bracketedHomeMatch(true, matchesHome, homeErr, statErr),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

// stopHomeProcesses stops every process left running under home (see the file header for which,
// names the ones it signalled. It returns once a listing finds nothing under home, or with an
// error when a live process cannot be classified or survives the stop bound (killing only targets
// it can safely signal).
func stopHomeProcesses(home string) ([]string, error) {
	return stopHomeProcessesWithin(home, homeProcessStopBound, preparationWorkerTermGrace)
}

func stopHomeProcessesWithin(home string, bound, grace time.Duration) ([]string, error) {
	return stopHomeProcessesWithinWithProbes(home, bound, grace, stopProcessProbes{
		list: homeProcesses, inspect: inspectStopProcess, home: processHomeObservation, kill: syscall.Kill,
	})
}

type stopProcessSnapshot struct {
	gone               bool
	args               []string
	startTime          string
	state              string
	stateFound         bool
	killErr            error
	stateErr           error
	flags              uint64
	flagsFound         bool
	flagsErr           error
	lateStartTime      string
	lateStartTimeFound bool
	lateStartTimeErr   error
	lateHomeErr        error
	exiting            bool
	reads              []string
	observedAt         time.Time
}

var errHomeObservationUnknown = errors.New("HOME observation was unreadable")
var errHomeOwnershipChanged = errors.New("process no longer has the listed HOME")
var errHomeEnvironmentCleared = errors.New("process environment was cleared")

// errHomeDeparted marks a process let go because its HOME left (homeDeparted): never signalled,
// and the home is listed again before the stop may return.
var errHomeDeparted = errors.New("process environment no longer names this HOME")

// homeDepartureWait bounds how long the stop waits for a process whose HOME left its environment
// to end before letting it be.
const homeDepartureWait = 2 * time.Second

// homeDeparted reports a HOME read that found the environment still populated but no longer
// naming this HOME: without HOME, or with a different one. A process does that by exec'ing a
// program with a new environment, keeping its pid and start time. Podman does exactly that, which
// is how CI run 40012293896 met it: it runs its OCI runtime (its `kill` and `delete` among them)
// through utils.ExecCmdWithStdStreams with an environment of XDG_RUNTIME_DIR alone (read in
// podman 5.8.9's libpod/oci_conmon_common.go; CI ran 4.9.3), and Go's os/exec starts a child
// with CLONE_VM|CLONE_VFORK, so until its execve lands the child reads as podman, argv and
// HOME included. The census's first HOME read caught it then, its bracketed second read after.
//
// By the stop's own criterion (the file header: a process is this test's because its
// environment carries the HOME) such a process is no longer provably this test's, so it is
// NEVER signalled: the start-time guard ties a pid to one process, not to this HOME. Nor does it
// fail the stop, which fails only for a process provably under the HOME. It is waited for, up to
// homeDepartureWait, so a departing runtime child is usually gone before the temp dir is
// removed, and the home is listed again, so a process that comes back under the HOME is stopped
// as any other. An emptied environment is not a departure: that is an exiting process, which
// provenExitingStopTarget handles.
func homeDeparted(err error) bool {
	return errors.Is(err, errHomeOwnershipChanged) && !errors.Is(err, errHomeEnvironmentCleared)
}

// awaitHomeDeparture waits, up to homeDepartureWait and the stop's deadline, for a departed
// process (homeDeparted) to end, to become a different process, or to name this HOME again. It
// never signals, and it reports nothing: whichever happens, the next listing decides.
func awaitHomeDeparture(home string, p homeProcess, deadline time.Time, probes stopProcessProbes) {
	until := time.Now().Add(homeDepartureWait)
	if deadline.Before(until) {
		until = deadline
	}
	for {
		snapshot, err := probes.inspect(p.pid)
		if err != nil || snapshot.gone || p.startTime == "" || snapshot.startTime != p.startTime {
			return
		}
		if probes.home != nil {
			if matches, err := probes.home(p.pid, home); err == nil && matches {
				return
			}
		}
		pause := time.Until(until)
		if pause <= 0 {
			return
		}
		if pause > 50*time.Millisecond {
			pause = 50 * time.Millisecond
		}
		time.Sleep(pause)
	}
}

type uncertainHomeObservationError struct {
	err error
}

func (e *uncertainHomeObservationError) Error() string { return e.err.Error() }
func (e *uncertainHomeObservationError) Unwrap() error { return e.err }

type stopProcessProbes struct {
	list    func(string) []homeProcess
	inspect func(int) (stopProcessSnapshot, error)
	home    func(int, string) (bool, error)
	kill    func(int, syscall.Signal) error
}

func inspectStopProcess(pid int) (stopProcessSnapshot, error) {
	var reads []string
	observe := func(name string) processLivenessObservation {
		reads = append(reads, name)
		return observeProcessLiveness(pid)
	}
	result := func(gone bool, args []string, startTime string, live processLivenessObservation) stopProcessSnapshot {
		return stopProcessSnapshot{
			gone: gone, args: args, startTime: startTime,
			state: live.state, stateFound: live.found, killErr: live.killErr, stateErr: live.stateErr,
			flags: live.flags, flagsFound: live.flagsFound, flagsErr: live.flagsErr,
			lateStartTime: live.startTime, lateStartTimeFound: live.startTimeFound, lateStartTimeErr: live.startTimeErr,
			reads: append([]string(nil), reads...), observedAt: time.Now(),
		}
	}

	live := observe("gone-before")
	if live.gone {
		return result(true, nil, "", live), nil
	}
	reads = append(reads, "start-time")
	startTime, err := processStartTime(pid)
	if err != nil {
		live = observe("gone-after-start-error")
		if live.gone {
			return result(true, nil, "", live), nil
		}
		return stopProcessSnapshot{}, fmt.Errorf("read process identity: %w", err)
	}
	reads = append(reads, "cmdline")
	args, err := processArgs(pid)
	if err != nil {
		live = observe("gone-after-argument-error")
		if live.gone {
			return result(true, nil, startTime, live), nil
		}
		return stopProcessSnapshot{}, fmt.Errorf("read process arguments: %w", err)
	}
	live = observe("gone-after")
	if live.gone {
		return result(true, nil, startTime, live), nil
	}
	return result(false, args, startTime, live), nil
}

type provenExitObservation struct {
	exiting     bool
	reason      string
	lateHomeErr error
}

// provenExitingStopTarget recognizes only a process whose same post-argv stat ties PF_EXITING, a
// valid live state, and field-22 starttime to the listed identity. That process is either
// already owned (its bracketed HOME read matched) or one whose bracketed read found its
// environment already released: first seen while exiting, after the kernel dropped its memory, so
// its HOME can no longer be confirmed but it can run no more code under any HOME. Either is only
// WAITED for, never signalled, so an unconfirmed one claims no ownership; anything else whose
// HOME was not confirmed is refused. A late unreadable HOME observation remains visible and is
// retained only through the existing bound.
func provenExitingStopTarget(home string, p homeProcess, snapshot stopProcessSnapshot, probes stopProcessProbes) (provenExitObservation, error) {
	switch {
	case !p.homeMatched && !errors.Is(p.homeErr, errHomeEnvironmentCleared):
		return provenExitObservation{reason: "HOME ownership was not previously confirmed"}, nil
	case p.homeErr != nil && !errors.Is(p.homeErr, errHomeEnvironmentCleared):
		return provenExitObservation{reason: "prior HOME observation was not coherent"}, nil
	case p.statErr != nil || p.startTime == "":
		return provenExitObservation{reason: "listed process identity was unavailable"}, nil
	case snapshot.startTime != p.startTime:
		return provenExitObservation{reason: "pre-argv process identity changed"}, nil
	case snapshot.lateStartTimeErr != nil || !snapshot.lateStartTimeFound:
		return provenExitObservation{reason: "post-argv process starttime is unavailable"}, nil
	case snapshot.lateStartTime != p.startTime:
		return provenExitObservation{reason: "post-argv process identity changed"}, nil
	case len(snapshot.args) != 0:
		return provenExitObservation{reason: "process arguments are no longer empty"}, nil
	case snapshot.stateErr != nil || !snapshot.stateFound || !validLiveLinuxProcessState(snapshot.state):
		return provenExitObservation{reason: "post-argv process state is not a valid live Linux state"}, nil
	case !snapshot.flagsFound || snapshot.flagsErr != nil || snapshot.flags&linuxPFExiting == 0:
		return provenExitObservation{reason: "post-argv Linux stat does not prove PF_EXITING"}, nil
	case probes.home == nil:
		return provenExitObservation{reason: "HOME observation probe is unavailable"}, errors.New("HOME observation probe is unavailable")
	}

	matches, err := probes.home(p.pid, home)
	if err != nil {
		if errors.Is(err, errHomeEnvironmentCleared) && !matches {
			return provenExitObservation{exiting: true}, nil
		}
		if !matches && errors.Is(err, errHomeObservationUnknown) &&
			(errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EACCES)) {
			return provenExitObservation{exiting: true, lateHomeErr: err}, nil
		}
		return provenExitObservation{}, fmt.Errorf("revalidating exiting %s HOME ownership: %w; %s", p, err, exitingHomeObservationDiagnostic(p, snapshot, err))
	}
	if !matches {
		return provenExitObservation{}, fmt.Errorf("revalidating exiting %s: process no longer has HOME=%s", p, home)
	}
	return provenExitObservation{exiting: true}, nil
}

func validLiveLinuxProcessState(state string) bool {
	return len(state) == 1 && strings.ContainsRune("RSDTtKWPIN", rune(state[0]))
}

func validateStopTarget(home string, p homeProcess, probes stopProcessProbes) (stopProcessSnapshot, bool, error) {
	snapshot, err := probes.inspect(p.pid)
	if err != nil {
		return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: %w", p, err)
	}
	if snapshot.gone {
		return snapshot, false, nil
	}
	if p.homeErr != nil && !errors.Is(p.homeErr, errHomeEnvironmentCleared) {
		if errors.Is(p.homeErr, errHomeObservationUnknown) {
			return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: %w", p, &uncertainHomeObservationError{err: p.homeErr})
		}
		return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: %w", p, p.homeErr)
	}
	if p.statErr != nil || p.startTime == "" {
		if p.statErr != nil {
			return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: original process identity was unreadable: %w", p, p.statErr)
		}
		return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: original process identity was empty", p)
	}
	if snapshot.startTime != p.startTime {
		return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: pid now names a different process", p)
	}
	if p.exiting || p.homeErr != nil {
		proof, err := provenExitingStopTarget(home, p, snapshot, probes)
		if err != nil {
			return stopProcessSnapshot{}, false, err
		}
		if !proof.exiting {
			return stopProcessSnapshot{}, false, fmt.Errorf("revalidating exiting process identity for %s: %s", p, proof.reason)
		}
		snapshot.exiting = true
		snapshot.lateHomeErr = proof.lateHomeErr
	}
	return snapshot, true, nil
}

func reobserveUncertainHome(home string, p homeProcess, deadline time.Time, probes stopProcessProbes) (homeProcess, bool, error) {
	for {
		if time.Now().After(deadline) {
			return homeProcess{}, false, fmt.Errorf("revalidating %s: HOME classification remained unreadable through the stop bound", p)
		}
		latest := probes.list(home)
		var listed *homeProcess
		for i := range latest {
			if latest[i].pid == p.pid {
				listed = &latest[i]
				break
			}
		}
		snapshot, err := probes.inspect(p.pid)
		if err != nil {
			return homeProcess{}, false, fmt.Errorf("revalidating %s after an uncertain HOME observation: %w", p, err)
		}
		if snapshot.gone {
			return homeProcess{}, false, nil
		}
		if listed != nil {
			if listed.statErr != nil {
				return homeProcess{}, false, fmt.Errorf("revalidating %s: current process identity was unreadable: %w", p, listed.statErr)
			}
			if listed.startTime != "" && listed.startTime != p.startTime {
				return homeProcess{}, false, fmt.Errorf("revalidating %s: pid now names a different process", p)
			}
			if homeDeparted(listed.homeErr) {
				awaitHomeDeparture(home, p, deadline, probes)
				return homeProcess{}, false, fmt.Errorf("revalidating %s: %w: %w", p, errHomeDeparted, listed.homeErr)
			}
			if listed.homeErr != nil && !errors.Is(listed.homeErr, errHomeObservationUnknown) {
				return homeProcess{}, false, fmt.Errorf("revalidating %s HOME ownership: %w", p, listed.homeErr)
			}
		}
		if p.statErr != nil || p.startTime == "" {
			return homeProcess{}, false, fmt.Errorf("revalidating %s: original process identity was unreadable or empty", p)
		}
		if snapshot.startTime != p.startTime {
			return homeProcess{}, false, fmt.Errorf("revalidating %s: pid now names a different process", p)
		}
		if len(snapshot.args) == 0 {
			return homeProcess{}, false, fmt.Errorf("revalidating %s: live process has no classifiable arguments", p)
		}
		if probes.home == nil {
			return homeProcess{}, false, fmt.Errorf("revalidating %s: HOME observation probe is unavailable", p)
		}
		matches, err := probes.home(p.pid, home)
		if homeDeparted(err) || (err == nil && !matches) {
			awaitHomeDeparture(home, p, deadline, probes)
			return homeProcess{}, false, fmt.Errorf("revalidating %s: %w: no longer has HOME=%s", p, errHomeDeparted, home)
		}
		if err != nil && !errors.Is(err, errHomeObservationUnknown) {
			return homeProcess{}, false, fmt.Errorf("revalidating %s HOME ownership: %w", p, err)
		}
		if err == nil && matches {
			p.args = snapshot.args
			p.argsErr = nil
			p.homeErr = nil
			if listed != nil {
				p.ppid = listed.ppid
			}
			return p, true, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			continue
		}
		if remaining > 50*time.Millisecond {
			remaining = 50 * time.Millisecond
		}
		time.Sleep(remaining)
	}
}

func stopDiagnosticErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, errProcessStatFlagsUnavailable):
		return "unsupported"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrNotExist):
		return "not-found"
	case errors.Is(err, syscall.ESRCH):
		return "no-such-process"
	case errors.Is(err, errHomeObservationUnknown):
		return "home-unreadable"
	case errors.Is(err, errHomeOwnershipChanged):
		return "home-changed"
	default:
		return "other"
	}
}

func stopDiagnosticState(snapshot stopProcessSnapshot) string {
	if snapshot.stateErr != nil {
		return "unreadable"
	}
	if !snapshot.stateFound {
		return "not-found"
	}
	if snapshot.state == "" {
		return "empty"
	}
	for _, r := range snapshot.state {
		if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && r != '+' && r != '-' {
			return "other"
		}
	}
	return snapshot.state
}

func exitingHomeObservationDiagnostic(p homeProcess, snapshot stopProcessSnapshot, homeErr error) string {
	start := func(value string) string {
		if value == "" || len(value) > 20 {
			return "unavailable"
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return "invalid"
			}
		}
		return value
	}
	state := "unavailable"
	switch {
	case snapshot.stateErr != nil:
		state = "unreadable"
	case !snapshot.stateFound:
		state = "not-found"
	case snapshot.state == "":
		state = "empty"
	case len(snapshot.state) != 1:
		state = "other"
	default:
		r := rune(snapshot.state[0])
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '+' || r == '-' {
			state = snapshot.state
		} else {
			state = "other"
		}
	}
	flags := "unavailable"
	flagsError := stopDiagnosticErrorClass(snapshot.flagsErr)
	if !snapshot.flagsFound && snapshot.flagsErr == nil {
		flagsError = "unavailable"
	}
	pfExiting := "unknown"
	if snapshot.flagsFound && snapshot.flagsErr == nil {
		flags = strconv.FormatUint(snapshot.flags, 10)
		if snapshot.flags&linuxPFExiting != 0 {
			pfExiting = "true"
		} else {
			pfExiting = "false"
		}
	}
	return fmt.Sprintf("exiting-home-observation{pid=%d,expected-start=%s,listed-start=%s,held=%t,before-start=%s,after-start=%s,home-owned-before=%t,home-error=%s,state=%s,state-error=%s,flags=%s,flags-error=%s,pf-exiting=%s}",
		p.pid, start(p.startTime), start(p.startTime), p.exiting, start(snapshot.startTime), start(snapshot.lateStartTime),
		p.homeMatched, stopDiagnosticErrorClass(homeErr), state, stopDiagnosticErrorClass(snapshot.stateErr), flags, flagsError, pfExiting)
}

func emptyArgStopDiagnostic(p homeProcess, snapshot stopProcessSnapshot) string {
	homeClass := "matched"
	if p.homeErr != nil {
		homeClass = stopDiagnosticErrorClass(p.homeErr)
	}
	reads := snapshot.reads
	if len(reads) > 8 {
		reads = reads[:8]
	}
	readOrder := strings.Join(reads, ",")
	if readOrder == "" {
		readOrder = "unavailable"
	}
	observedAt := "unknown"
	if !snapshot.observedAt.IsZero() {
		observedAt = snapshot.observedAt.UTC().Format(time.RFC3339Nano)
	}
	flags := "unavailable"
	flagsError := "unavailable"
	if snapshot.flagsErr != nil {
		flagsError = stopDiagnosticErrorClass(snapshot.flagsErr)
	}
	if snapshot.flagsFound {
		flags = strconv.FormatUint(snapshot.flags, 10)
		flagsError = "none"
	}
	return fmt.Sprintf("empty-argv-observation{pid=%d,start=%s,home=%s,listed-args=%d,listed-args-error=%s,args=%d,args-error=%s,state=%s,state-error=%s,flags=%s,flags-error=%s,kill-error=%s,reads=%s,observed=%s}",
		p.pid, p.startTime, homeClass, len(p.args), stopDiagnosticErrorClass(p.argsErr),
		len(snapshot.args), stopDiagnosticErrorClass(nil), stopDiagnosticState(snapshot),
		stopDiagnosticErrorClass(snapshot.stateErr), flags, flagsError,
		stopDiagnosticErrorClass(snapshot.killErr), readOrder, observedAt)
}

// refreshStopListing revalidates one listing. departed reports that it let a process go because
// its HOME left (homeDeparted), so the caller lists the home again before it may return.
func refreshStopListing(home string, procs []homeProcess, deadline time.Time, probes stopProcessProbes) (current []homeProcess, departed bool, err error) {
	for _, p := range procs {
		if homeDeparted(p.homeErr) {
			awaitHomeDeparture(home, p, deadline, probes)
			departed = true
			continue
		}
		snapshot, live, err := validateStopTarget(home, p, probes)
		var uncertain *uncertainHomeObservationError
		if err != nil && errors.As(err, &uncertain) {
			var refreshed homeProcess
			var stillLive bool
			refreshed, stillLive, err = reobserveUncertainHome(home, p, deadline, probes)
			if errors.Is(err, errHomeDeparted) {
				departed = true
				continue
			}
			if err == nil && stillLive {
				p = refreshed
				snapshot, live = stopProcessSnapshot{args: p.args, startTime: p.startTime}, true
			}
		}
		if err != nil {
			if p.argsErr != nil {
				return nil, false, fmt.Errorf("revalidating %s after its initial argument read failed: %w; revalidation: %v", p, p.argsErr, err)
			}
			return nil, false, err
		}
		if snapshot.lateHomeErr != nil && p.lateHomeErr == nil {
			p.lateHomeErr = snapshot.lateHomeErr
		}
		if !live {
			continue
		}
		if len(snapshot.args) == 0 {
			if !snapshot.exiting {
				proof, err := provenExitingStopTarget(home, p, snapshot, probes)
				if err != nil {
					return nil, false, err
				}
				snapshot.exiting = proof.exiting
				snapshot.lateHomeErr = proof.lateHomeErr
			}
			if snapshot.exiting && !time.Now().Before(deadline) {
				snapshot.exiting = false
			}
			if snapshot.exiting {
				p.exiting = true
				if p.lateHomeErr == nil && snapshot.lateHomeErr != nil {
					p.lateHomeErr = snapshot.lateHomeErr
				}
				current = append(current, p)
				continue
			}
			original := fmt.Errorf("revalidating %s before signalling: live process has no classifiable arguments", p)
			return nil, false, fmt.Errorf("%w; %s", original, emptyArgStopDiagnostic(p, snapshot))
		}
		p.args = snapshot.args
		p.argsErr = nil
		current = append(current, p)
	}
	return current, departed, nil
}

func signalStopTarget(home string, p homeProcess, signal syscall.Signal, probes stopProcessProbes) (bool, error) {
	snapshot, live, err := validateStopTarget(home, p, probes)
	if err != nil || !live {
		return false, err
	}
	if len(snapshot.args) == 0 {
		return false, fmt.Errorf("signalling %s with %s: live process has no readable arguments", p, signal)
	}
	if (homeProcess{args: snapshot.args}).waitOnly() {
		return false, nil
	}
	if err := probes.kill(p.pid, signal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, fmt.Errorf("signalling %s with %s: %w", p, signal, err)
	}
	return true, nil
}

func waitStopTargetGone(home string, p homeProcess, deadline time.Time, probes stopProcessProbes) (bool, error) {
	for {
		_, live, err := validateStopTarget(home, p, probes)
		if err != nil || !live {
			return !live && err == nil, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func stopHomeProcessesWithinWithProbes(home string, bound, grace time.Duration, probes stopProcessProbes) ([]string, error) {
	var stopped []string
	var exiting []homeProcess
	relistedPastBound := false
	deadline := time.Now().Add(bound)
	for {
		listed := probes.list(home)
		var retained []homeProcess
		heldPIDs := map[int]bool{}
		for _, p := range exiting {
			snapshot, live, err := validateStopTarget(home, p, probes)
			if err != nil {
				return stopped, err
			}
			if !live {
				continue
			}
			if !snapshot.exiting {
				return stopped, fmt.Errorf("revalidating exiting process identity for %s: PF_EXITING evidence was lost", p)
			}
			if snapshot.lateHomeErr != nil && p.lateHomeErr == nil {
				p.lateHomeErr = snapshot.lateHomeErr
			}
			for _, observed := range listed {
				if observed.pid != p.pid {
					continue
				}
				if observed.statErr != nil {
					return stopped, fmt.Errorf("revalidating exiting %s listed identity: %w", p, observed.statErr)
				}
				if observed.startTime != "" && observed.startTime != p.startTime {
					return stopped, fmt.Errorf("revalidating exiting %s: pid now names a different process", p)
				}
				if observed.homeErr != nil && !(errors.Is(observed.homeErr, errHomeObservationUnknown) &&
					(errors.Is(observed.homeErr, syscall.ESRCH) || errors.Is(observed.homeErr, syscall.EACCES))) {
					return stopped, fmt.Errorf("revalidating exiting %s listed HOME ownership: %w", p, observed.homeErr)
				}
			}
			retained = append(retained, p)
			heldPIDs[p.pid] = true
		}
		if len(heldPIDs) > 0 {
			current := listed[:0]
			for _, p := range listed {
				if !heldPIDs[p.pid] {
					current = append(current, p)
				}
			}
			listed = current
		}
		if len(listed) == 0 && len(retained) == 0 {
			return stopped, nil
		}

		procs, departed, err := refreshStopListing(home, listed, deadline, probes)
		if err != nil {
			return stopped, err
		}
		var targets []homeProcess
		exitingByPID := make(map[int]homeProcess, len(retained)+len(procs))
		for _, p := range retained {
			exitingByPID[p.pid] = p
		}
		for _, p := range procs {
			if p.exiting {
				exitingByPID[p.pid] = p
			} else {
				targets = append(targets, p)
			}
		}
		exiting = exiting[:0]
		for _, p := range exitingByPID {
			exiting = append(exiting, p)
		}
		sort.Slice(exiting, func(i, j int) bool { return exiting[i].pid < exiting[j].pid })
		procs = targets

		if len(procs) == 0 {
			if len(exiting) == 0 {
				// A departed process (homeDeparted) was let go: list again, so one back under the
				// HOME is stopped or reported rather than missed; once past the bound, only once.
				if departed && !relistedPastBound {
					relistedPastBound = !time.Now().Before(deadline)
					continue
				}
				return stopped, nil
			}
			if !time.Now().Before(deadline) {
				boundErr := fmt.Errorf("known exiting process identity %s is still live after the stop bound %s; it was not signalled", exiting[0].exitingIdentity(), bound)
				if exiting[0].lateHomeErr != nil {
					return stopped, fmt.Errorf("%w: last HOME observation: %w", boundErr, exiting[0].lateHomeErr)
				}
				return stopped, boundErr
			}
			pause := time.Until(deadline)
			if pause > 10*time.Millisecond {
				pause = 10 * time.Millisecond
			}
			time.Sleep(pause)
			continue
		}

		if time.Now().After(deadline) {
			var left, notSignalled, failures []string
			var signalled []homeProcess
			var retainedHOMEErrors []error
			for _, p := range procs {
				sent, err := signalStopTarget(home, p, syscall.SIGKILL, probes)
				if err != nil {
					failures = append(failures, err.Error())
				} else if sent {
					left = append(left, p.String())
					signalled = append(signalled, p)
				} else {
					_, live, err := validateStopTarget(home, p, probes)
					if err != nil {
						failures = append(failures, err.Error())
					} else if live {
						notSignalled = append(notSignalled, p.String())
					}
				}
			}
			for _, p := range signalled {
				if _, err := waitStopTargetGone(home, p, time.Now().Add(2*time.Second), probes); err != nil {
					failures = append(failures, err.Error())
				}
			}
			for _, p := range exiting {
				failures = append(failures, fmt.Sprintf("known exiting process identity %s is still live and was not signalled", p.exitingIdentity()))
				if p.lateHomeErr != nil {
					retainedHOMEErrors = append(retainedHOMEErrors, fmt.Errorf("last HOME observation for %s: %w", p, p.lateHomeErr))
				}
			}
			if len(left) > 0 || len(notSignalled) > 0 || len(failures) > 0 {
				message := fmt.Sprintf("still running under %s %s after the stop began", home, bound)
				if len(left) > 0 {
					message += ", and killed: " + strings.Join(left, ", ")
				}
				if len(notSignalled) > 0 {
					message += "; still alive, not signalled: " + strings.Join(notSignalled, ", ")
				}
				if len(failures) > 0 {
					message += "; " + strings.Join(failures, "; ")
				}
				boundErrors := append([]error{errors.New(message)}, retainedHOMEErrors...)
				return stopped, errors.Join(boundErrors...)
			}
			return stopped, nil
		}
		targets = stopRound(procs)
		if len(targets) == 0 {
			// Only scratch removers are left: they end on their own.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var signalled []homeProcess
		for _, p := range targets {
			sent, err := signalStopTarget(home, p, syscall.SIGTERM, probes)
			if err != nil {
				return stopped, err
			}
			if sent {
				stopped = append(stopped, p.String())
				signalled = append(signalled, p)
			}
		}
		roundEnd := time.Now().Add(grace)
		for _, p := range signalled {
			if _, err := waitStopTargetGone(home, p, roundEnd, probes); err != nil {
				return stopped, err
			}
		}
		for _, p := range signalled {
			_, live, err := validateStopTarget(home, p, probes)
			if err != nil {
				return stopped, err
			}
			if live {
				if _, err := signalStopTarget(home, p, syscall.SIGKILL, probes); err != nil {
					return stopped, err
				}
				if _, err := waitStopTargetGone(home, p, time.Now().Add(2*time.Second), probes); err != nil {
					return stopped, err
				}
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
// its environment shows that HOME. The test's end stops it if the code under test did not.
func startStandIn(t *testing.T, home string, env []string, argv0, script string, args ...string) int {
	t.Helper()
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

func TestStopHomeProcessesRetriesOnlyUncertainHomeObservation(t *testing.T) {
	const home = "/test/home"
	base := homeProcess{pid: 27182, ppid: 1, args: []string{"/bin/worker"}, startTime: "417"}
	unknown := base
	// This is the lister state after its first HOME read matched and its second read was
	// unreadable; homeProcesses keeps that PID and its start identity for stop-time reobservation.
	unknown.homeErr = fmt.Errorf("could not reverify HOME=%s: %w", home, errHomeObservationUnknown)

	t.Run("coherent current listing resumes worker stop", func(t *testing.T) {
		listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{unknown}
				}
				if listCalls == 2 {
					return []homeProcess{base}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				inspectCalls++
				if killCalls > 0 {
					return stopProcessSnapshot{gone: true}, nil
				}
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			home: func(int, string) (bool, error) { homeCalls++; return true, nil },
			kill: func(int, syscall.Signal) error {
				if homeCalls == 0 {
					t.Error("worker was signalled before a coherent HOME observation")
				}
				killCalls++
				return nil
			},
		}
		stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
		if err != nil || len(stopped) != 1 || killCalls != 1 || inspectCalls < 3 || homeCalls != 1 {
			t.Fatalf("stop = %v, %v (lists %d, inspections %d, HOME observations %d, signals %d); want resumed worker stopped after coherent re-observation", stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
		}
	})

	t.Run("coherent current snapshot resumes remover wait", func(t *testing.T) {
		remover := homeProcess{pid: base.pid, ppid: base.ppid, args: []string{"yolo", "internal", "scratch-rm"}, startTime: base.startTime}
		removerUnknown := remover
		removerUnknown.homeErr = fmt.Errorf("could not reverify HOME=%s: %w", home, errHomeObservationUnknown)
		listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{removerUnknown}
				}
				if listCalls == 2 || listCalls == 3 {
					return []homeProcess{remover}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				inspectCalls++
				return stopProcessSnapshot{args: remover.args, startTime: remover.startTime}, nil
			},
			home: func(int, string) (bool, error) { homeCalls++; return true, nil },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		started := time.Now()
		stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
		if err != nil || len(stopped) != 0 || killCalls != 0 || homeCalls != 1 || inspectCalls < 3 || time.Since(started) < 40*time.Millisecond {
			t.Fatalf("stop = %v, %v (lists %d, inspections %d, HOME observations %d, signals %d); want remover waited without signal after coherent re-observation", stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
		}
	})

	t.Run("empty census does not prove uncertain pid gone", func(t *testing.T) {
		listCalls, inspectCalls, killCalls := 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{unknown}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				inspectCalls++
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			home: func(int, string) (bool, error) { return false, errHomeObservationUnknown },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		started := time.Now()
		stopped, err := stopHomeProcessesWithinWithProbes(home, 30*time.Millisecond, 0, probes)
		if err == nil || !strings.Contains(err.Error(), "HOME") || len(stopped) != 0 || killCalls != 0 {
			t.Fatalf("stop = %v, %v; want bounded HOME-classification error and no signal", stopped, err)
		}
		if elapsed := time.Since(started); elapsed < 20*time.Millisecond || inspectCalls < 2 || listCalls < 2 {
			t.Fatalf("uncertain pid was not explicitly re-observed through the bound (elapsed %s, lists %d, inspections %d)", elapsed, listCalls, inspectCalls)
		}
	})

	t.Run("explicitly gone uncertain pid succeeds", func(t *testing.T) {
		listCalls, inspectCalls, killCalls := 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{unknown}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				inspectCalls++
				if inspectCalls == 1 {
					return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
				}
				return stopProcessSnapshot{gone: true}, nil
			},
			home: func(int, string) (bool, error) { return false, errHomeObservationUnknown },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
		if err != nil || len(stopped) != 0 || killCalls != 0 || inspectCalls != 2 || listCalls < 2 {
			t.Fatalf("stop = %v, %v (lists %d, inspections %d); want success only after explicit re-observation confirms gone", stopped, err, listCalls, inspectCalls)
		}
	})

	t.Run("pid reuse during HOME retry remains an error", func(t *testing.T) {
		listCalls, killCalls, homeCalls := 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{unknown}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: base.args, startTime: "418"}, nil
			},
			home: func(int, string) (bool, error) { homeCalls++; return true, nil },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		_, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
		if err == nil || listCalls < 2 || homeCalls != 0 || killCalls != 0 {
			t.Fatalf("stop error %v, lists %d, HOME observations %d, signals %d; want PID-reuse error before HOME retry or signal", err, listCalls, homeCalls, killCalls)
		}
	})

	t.Run("HOME departing during the retry is never signalled", func(t *testing.T) {
		departure := fmt.Errorf("%w: pid %d no longer has HOME", errHomeOwnershipChanged, base.pid)
		listCalls, killCalls := 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{unknown}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			home: func(int, string) (bool, error) { return false, departure },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		_, err := stopHomeProcessesWithinWithProbes(home, 100*time.Millisecond, 0, probes)
		if err != nil || listCalls < 3 || killCalls != 0 {
			t.Fatalf("stop error %v, lists %d, signals %d; want it let go unsignalled and the home re-listed", err, listCalls, killCalls)
		}
	})

	t.Run("confirmed ownership change is never signalled", func(t *testing.T) {
		changed := base
		changed.homeErr = errHomeOwnershipChanged
		listCalls, killCalls := 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{changed}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			home: func(int, string) (bool, error) { return false, errHomeOwnershipChanged },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		_, err := stopHomeProcessesWithinWithProbes(home, 100*time.Millisecond, 0, probes)
		if err != nil || listCalls != 2 || killCalls != 0 {
			t.Fatalf("stop error %v, lists %d, signals %d; want it let go unsignalled and the home re-listed", err, listCalls, killCalls)
		}
	})
}

// A process whose environment lost this HOME between the census's two reads is no longer this
// test's by the stop's own criterion, so it is never signalled, and it fails the stop only if a
// later listing finds it under the HOME again and it survives. CI run 40012293896 failed
// TestPackTreeProgramBuildFailureRefusesOnlyIt's cleanup with exactly this listing: pid 85307
// "(podman)", whose second HOME read found an environment without HOME (homeDeparted).
func TestStopHomeProcessesLetsAProcessWhoseHOMELeftGoUnsignalled(t *testing.T) {
	const home = "/test/home"
	const pid = 85307
	const startTime = "3141592"
	for _, departure := range []error{
		fmt.Errorf("%w: pid %d no longer has HOME", errHomeOwnershipChanged, pid),
		fmt.Errorf("%w: pid %d now has a different HOME", errHomeOwnershipChanged, pid),
	} {
		listed := homeProcess{pid: pid, ppid: 4000, args: []string{"podman"}, startTime: startTime, homeErr: departure}
		runtime := stopProcessSnapshot{args: []string{"crun", "state", "abc"}, startTime: startTime}

		t.Run(departure.Error()+"/gone after its exec", func(t *testing.T) {
			listCalls, inspectCalls, killCalls := 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls == 1 {
						return []homeProcess{listed}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) {
					inspectCalls++
					if inspectCalls == 1 {
						return runtime, nil
					}
					return stopProcessSnapshot{gone: true}, nil
				},
				home: func(int, string) (bool, error) { return false, departure },
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err != nil || len(stopped) != 0 || killCalls != 0 || inspectCalls != 2 || listCalls != 2 {
				t.Fatalf("stop=%v, %v (lists=%d inspections=%d signals=%d); want it waited for until gone, never signalled, and the home re-listed",
					stopped, err, listCalls, inspectCalls, killCalls)
			}
		})

		t.Run(departure.Error()+"/still live without the HOME", func(t *testing.T) {
			listCalls, killCalls := 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls == 1 {
						return []homeProcess{listed}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) { return runtime, nil },
				home:    func(int, string) (bool, error) { return false, departure },
				kill:    func(int, syscall.Signal) error { killCalls++; return nil },
			}
			started := time.Now()
			stopped, err := stopHomeProcessesWithinWithProbes(home, 200*time.Millisecond, 0, probes)
			if err != nil || len(stopped) != 0 || killCalls != 0 || listCalls != 2 {
				t.Fatalf("stop=%v, %v (lists=%d signals=%d); want it let go unsignalled once no listing names it", stopped, err, listCalls, killCalls)
			}
			if elapsed := time.Since(started); elapsed < 150*time.Millisecond || elapsed > 2*time.Second {
				t.Fatalf("the stop returned after %s; want it to wait for the departed process up to the stop bound, and no longer", elapsed)
			}
		})

		t.Run(departure.Error()+"/back under the HOME is an ordinary target", func(t *testing.T) {
			back := listed
			back.homeErr, back.homeMatched = nil, true
			listCalls, homeCalls, killCalls := 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					switch listCalls {
					case 1:
						return []homeProcess{listed}
					case 2:
						return []homeProcess{back}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) {
					if killCalls > 0 {
						return stopProcessSnapshot{gone: true}, nil
					}
					return stopProcessSnapshot{args: listed.args, startTime: startTime}, nil
				},
				home: func(int, string) (bool, error) {
					homeCalls++
					if homeCalls == 1 {
						return false, departure
					}
					return true, nil
				},
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err != nil || len(stopped) != 1 || killCalls != 1 {
				t.Fatalf("stop=%v, %v (lists=%d signals=%d); want it stopped once a listing confirms it under the HOME again", stopped, err, listCalls, killCalls)
			}
		})
	}
}

func TestInspectStopProcessRecordsReadOrderForLiveProcess(t *testing.T) {
	cmd := exec.Command("sleep", "4987")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start live probe process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Start can return before the kernel has published the new command line. Wait only for this
	// owned fixture to be ready; the cleanup inspector itself remains a single immediate read.
	const fixtureReadyBound = 5 * time.Second
	deadline := time.Now().Add(fixtureReadyBound)
	for !processHasArgs(cmd.Process.Pid, "sleep", "4987") {
		if time.Now().After(deadline) {
			t.Fatalf("sleep fixture did not publish its command line within %s", fixtureReadyBound)
		}
		time.Sleep(10 * time.Millisecond)
	}

	snapshot, err := inspectStopProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("inspect live process: %v", err)
	}
	if snapshot.gone || snapshot.startTime == "" || len(snapshot.args) == 0 || !snapshot.stateFound {
		t.Fatalf("live-process snapshot = %+v, want identity, arguments, and state", snapshot)
	}
	if goruntime.GOOS == "linux" && (!snapshot.flagsFound || snapshot.flagsErr != nil) {
		t.Fatalf("live-process snapshot flags = %d, found %v, err %v; want numeric stat field 9 from the post-argv observation",
			snapshot.flags, snapshot.flagsFound, snapshot.flagsErr)
	}
	if goruntime.GOOS == "linux" && (!snapshot.lateStartTimeFound || snapshot.lateStartTimeErr != nil || snapshot.lateStartTime != snapshot.startTime) {
		t.Fatalf("live-process late starttime = %q, found %v, err %v; want the same identity from the post-argv stat", snapshot.lateStartTime, snapshot.lateStartTimeFound, snapshot.lateStartTimeErr)
	}
	if got, want := strings.Join(snapshot.reads, ","), "gone-before,start-time,cmdline,gone-after"; got != want {
		t.Errorf("probe read order = %q, want %q", got, want)
	}
	if snapshot.observedAt.IsZero() {
		t.Error("live-process snapshot has no observation time")
	}
}

func exitingStopSnapshot(startTime string) stopProcessSnapshot {
	return stopProcessSnapshot{
		startTime: startTime, state: "R", stateFound: true,
		flags: linuxPFExiting, flagsFound: true,
		lateStartTime: startTime, lateStartTimeFound: true,
	}
}

// The first HOME read only finds candidates; ownership requires the bracketed read to match
// between equal starttimes, so a PID reuse cannot inherit the earlier process's HOME.
func bracketedHomeMatch(initialMatch, bracketedMatch bool, homeErr, statErr error) bool {
	return initialMatch && bracketedMatch && homeErr == nil && statErr == nil
}

func TestBracketedHomeMatchRequiresTheIdentityBracket(t *testing.T) {
	cleared := fmt.Errorf("%w: environment was cleared", errHomeEnvironmentCleared)
	cases := []struct {
		name           string
		initialMatch   bool
		bracketedMatch bool
		homeErr        error
		statErr        error
		want           bool
	}{
		{name: "coherent match", initialMatch: true, bracketedMatch: true, want: true},
		{name: "initial match then cleared bracket", initialMatch: true, homeErr: cleared},
		{name: "initial match then mismatched bracket", initialMatch: true, statErr: errors.New("starttime changed")},
		{name: "initial match then unreadable bracket", initialMatch: true, homeErr: errHomeObservationUnknown},
		{name: "no initial match", bracketedMatch: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bracketedHomeMatch(tc.initialMatch, tc.bracketedMatch, tc.homeErr, tc.statErr); got != tc.want {
				t.Fatalf("bracketedHomeMatch() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidLiveLinuxProcessStateExcludesDeadStates(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  bool
	}{
		{state: "R", want: true},
		{state: "S", want: true},
		{state: "D", want: true},
		{state: "T", want: true},
		{state: "t", want: true},
		{state: "Z", want: false},
		{state: "X", want: false},
		{state: "x", want: false},
		{state: "?", want: false},
		{state: "", want: false},
		{state: "RR", want: false},
	} {
		t.Run(tc.state, func(t *testing.T) {
			if got := validLiveLinuxProcessState(tc.state); got != tc.want {
				t.Fatalf("validLiveLinuxProcessState(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// CI run 39886931703: a podman spawned late in the stop was listed by its first HOME read, then
// released its memory before the bracketed read, which found an empty environment. It was still
// exiting, not yet a zombie, and the stop refused it as unconfirmed. Kernel-proven exiting, it is
// waited for until gone and never signalled.
func TestStopHomeProcessesWaitsForAnUnconfirmedHomeFirstSeenExiting(t *testing.T) {
	const home = "/test/home"
	const startTime = "876542"
	cleared := fmt.Errorf("%w: environment disappeared during exit", errHomeEnvironmentCleared)
	listed := homeProcess{
		pid: 5150, ppid: 1, args: []string{"/bin/podman"}, startTime: startTime,
		homeErr:     cleared,
		homeMatched: bracketedHomeMatch(true, false, cleared, nil),
	}
	listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
	probes := stopProcessProbes{
		list: func(string) []homeProcess {
			listCalls++
			if listCalls == 1 {
				return []homeProcess{listed}
			}
			return nil
		},
		inspect: func(int) (stopProcessSnapshot, error) {
			inspectCalls++
			if inspectCalls == 3 {
				return stopProcessSnapshot{gone: true}, nil
			}
			return exitingStopSnapshot(startTime), nil
		},
		home: func(int, string) (bool, error) { homeCalls++; return false, cleared },
		kill: func(int, syscall.Signal) error { killCalls++; return nil },
	}
	stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
	if err != nil || len(stopped) != 0 || killCalls != 0 || listCalls != 3 || inspectCalls != 3 || homeCalls != 2 {
		t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want the exiting process waited for until gone, without signal",
			stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
	}
}

// Waiting for an unconfirmed exiting process stays bounded, and the bound report says its HOME
// was never confirmed rather than claiming it as this test's.
func TestStopHomeProcessesReportsUnconfirmedExitingHomeAtBound(t *testing.T) {
	const home = "/test/home"
	const startTime = "876540"
	cleared := fmt.Errorf("%w: environment disappeared during exit", errHomeEnvironmentCleared)
	listed := homeProcess{
		pid: 5149, ppid: 1, args: []string{"/bin/podman"}, startTime: startTime,
		homeErr: cleared, homeMatched: bracketedHomeMatch(true, false, cleared, nil),
	}
	listCalls, killCalls := 0, 0
	probes := stopProcessProbes{
		// An emptied environment fails the census's first HOME read, so the process is listed once.
		list: func(string) []homeProcess {
			listCalls++
			if listCalls == 1 {
				return []homeProcess{listed}
			}
			return nil
		},
		inspect: func(int) (stopProcessSnapshot, error) { return exitingStopSnapshot(startTime), nil },
		home:    func(int, string) (bool, error) { return false, cleared },
		kill:    func(int, syscall.Signal) error { killCalls++; return nil },
	}
	stopped, err := stopHomeProcessesWithinWithProbes(home, 30*time.Millisecond, 0, probes)
	if err == nil || !strings.Contains(err.Error(), "still live") || !strings.Contains(err.Error(), "HOME ownership unconfirmed") ||
		len(stopped) != 0 || killCalls != 0 {
		t.Fatalf("stop=%v, %v, signals=%d; want a bounded failure naming the unconfirmed HOME, without signal", stopped, err, killCalls)
	}
}

// An unconfirmed HOME is refused unless the kernel proves the process exiting: a live process
// whose environment reads empty, one that still has arguments, or one whose starttime moved
// across the HOME bracket is never waited for as if it were exiting, and never signalled.
func TestStopHomeProcessesRefusesUnconfirmedHomeWithoutExitProof(t *testing.T) {
	const home = "/test/home"
	const startTime = "876542"
	cleared := fmt.Errorf("%w: environment was cleared", errHomeEnvironmentCleared)
	notExiting := exitingStopSnapshot(startTime)
	notExiting.flags = 0
	withArgs := exitingStopSnapshot(startTime)
	withArgs.args = []string{"/bin/worker"}
	listed := homeProcess{pid: 5150, ppid: 1, args: []string{"/bin/worker"}, startTime: startTime, homeErr: cleared}
	moved := listed
	moved.statErr = errors.New("process identity changed while reading its HOME and arguments")
	for _, tc := range []struct {
		name     string
		listed   homeProcess
		snapshot stopProcessSnapshot
		want     string
	}{
		{name: "cleared environment without PF_EXITING", listed: listed, snapshot: notExiting, want: "does not prove PF_EXITING"},
		{name: "cleared environment with arguments", listed: listed, snapshot: withArgs, want: "arguments are no longer empty"},
		{name: "starttime moved across the HOME bracket", listed: moved, snapshot: exitingStopSnapshot(startTime), want: "original process identity was unreadable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			killCalls := 0
			probes := stopProcessProbes{
				list:    func(string) []homeProcess { return []homeProcess{tc.listed} },
				inspect: func(int) (stopProcessSnapshot, error) { return tc.snapshot, nil },
				home:    func(int, string) (bool, error) { return false, cleared },
				kill:    func(int, syscall.Signal) error { killCalls++; return nil },
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(stopped) != 0 || killCalls != 0 {
				t.Fatalf("stop=%v, %v, signals=%d; want immediate refusal containing %q", stopped, err, killCalls, tc.want)
			}
		})
	}
}

func TestStopHomeProcessesDoesNotPromoteInitiallyUnreadableHOMEToOwnership(t *testing.T) {
	const home = "/test/home"
	const startTime = "876541"
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.ESRCH} {
		t.Run(errno.Error(), func(t *testing.T) {
			listed := homeProcess{
				pid: 5150, ppid: 1, args: []string{"/bin/worker"}, startTime: startTime,
				homeErr: fmt.Errorf("%w: %w", errHomeObservationUnknown, errno),
			}
			listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess { listCalls++; return []homeProcess{listed} },
				inspect: func(int) (stopProcessSnapshot, error) {
					inspectCalls++
					return exitingStopSnapshot(startTime), nil
				},
				home: func(int, string) (bool, error) { homeCalls++; return false, nil },
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, 0, 0, probes)
			if err == nil || !strings.Contains(err.Error(), "HOME classification remained unreadable") || len(stopped) != 0 || listCalls != 1 || inspectCalls != 1 || homeCalls != 0 || killCalls != 0 {
				t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want initially unreadable %v refused without adopting ownership or signalling",
					stopped, err, listCalls, inspectCalls, homeCalls, killCalls, errno)
			}
		})
	}
}

func TestStopHomeProcessesRetainsLateHOMEObservationUntilExplicitGone(t *testing.T) {
	const home = "/test/home"
	const pid = 5150
	const startTime = "876542"
	listed := homeProcess{pid: pid, ppid: 1, startTime: startTime, homeMatched: true}
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.ESRCH} {
		t.Run(errno.Error(), func(t *testing.T) {
			lateHomeErr := fmt.Errorf("%w: %w", errHomeObservationUnknown, errno)
			listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls <= 2 {
						return []homeProcess{listed}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) {
					inspectCalls++
					if inspectCalls == 3 {
						return stopProcessSnapshot{gone: true}, nil
					}
					return exitingStopSnapshot(startTime), nil
				},
				home: func(int, string) (bool, error) {
					homeCalls++
					return false, lateHomeErr
				},
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err != nil || len(stopped) != 0 || listCalls != 3 || inspectCalls != 3 || homeCalls != 2 || killCalls != 0 {
				t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want late %v retained only until independent gone, without signal",
					stopped, err, listCalls, inspectCalls, homeCalls, killCalls, errno)
			}
		})
	}
}

func TestStopHomeProcessesRetainsLateEACCESUntilOriginalBound(t *testing.T) {
	const home = "/test/home"
	const startTime = "876540"
	const bound = 30 * time.Millisecond
	listed := homeProcess{pid: 5149, ppid: 1, startTime: startTime, homeMatched: true}
	firstHomeErr := fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.EACCES)
	laterHomeErr := fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.ESRCH)
	listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
	probes := stopProcessProbes{
		list: func(string) []homeProcess {
			listCalls++
			if listCalls == 1 {
				return []homeProcess{listed}
			}
			return nil
		},
		inspect: func(int) (stopProcessSnapshot, error) {
			inspectCalls++
			return exitingStopSnapshot(startTime), nil
		},
		home: func(int, string) (bool, error) {
			homeCalls++
			if homeCalls == 1 {
				return false, firstHomeErr
			}
			return false, laterHomeErr
		},
		kill: func(int, syscall.Signal) error { killCalls++; return nil },
	}
	started := time.Now()
	stopped, err := stopHomeProcessesWithinWithProbes(home, bound, 0, probes)
	elapsed := time.Since(started)
	if err == nil || !errors.Is(err, errHomeObservationUnknown) || !errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ESRCH) ||
		!strings.Contains(err.Error(), "after the stop bound") || len(stopped) != 0 || killCalls != 0 {
		t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want original bound with both HOME causes and no signal",
			stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
	}
	if elapsed < bound || elapsed > time.Second || inspectCalls < 2 || listCalls < 2 || homeCalls < 2 {
		t.Fatalf("elapsed=%s (lists=%d inspections=%d HOME=%d); want held identity rechecked through the original %s bound",
			elapsed, listCalls, inspectCalls, homeCalls, bound)
	}
}

func TestStopHomeProcessesRetainsLateHOMEObservationUntilGoneOrBound(t *testing.T) {
	const home = "/test/home"
	const startTime = "876540"
	listed := homeProcess{pid: 5149, ppid: 1, startTime: startTime, homeMatched: true}
	lateHomeErr := fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.ESRCH)

	t.Run("explicit gone succeeds without signal", func(t *testing.T) {
		listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{listed}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				inspectCalls++
				if inspectCalls == 3 {
					return stopProcessSnapshot{gone: true}, nil
				}
				return exitingStopSnapshot(startTime), nil
			},
			home: func(int, string) (bool, error) { homeCalls++; return false, lateHomeErr },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
		if err != nil || len(stopped) != 0 || listCalls != 3 || inspectCalls != 3 || homeCalls != 2 || killCalls != 0 {
			t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want explicit absence after held reinspection, no signal",
				stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
		}
	})

	t.Run("same-PID unreadable census preserves held proof without adopting listing", func(t *testing.T) {
		for _, errno := range []syscall.Errno{syscall.EACCES, syscall.ESRCH} {
			t.Run(errno.Error(), func(t *testing.T) {
				listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
				unreadable := homeProcess{
					pid: listed.pid, ppid: listed.ppid, startTime: startTime,
					homeErr: fmt.Errorf("%w: %w", errHomeObservationUnknown, errno),
				}
				probes := stopProcessProbes{
					list: func(string) []homeProcess {
						listCalls++
						if listCalls == 1 {
							return []homeProcess{listed}
						}
						if listCalls == 2 {
							return []homeProcess{unreadable}
						}
						return nil
					},
					inspect: func(int) (stopProcessSnapshot, error) {
						inspectCalls++
						if inspectCalls == 3 {
							return stopProcessSnapshot{gone: true}, nil
						}
						return exitingStopSnapshot(startTime), nil
					},
					home: func(int, string) (bool, error) { homeCalls++; return false, lateHomeErr },
					kill: func(int, syscall.Signal) error { killCalls++; return nil },
				}
				stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
				if err != nil || len(stopped) != 0 || listCalls != 3 || inspectCalls != 3 || homeCalls != 2 || killCalls != 0 {
					t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want held proof survive unreadable %v census until independent gone, without adopting or signalling",
						stopped, err, listCalls, inspectCalls, homeCalls, killCalls, errno)
				}
			})
		}
	})

	t.Run("omitted census preserves ESRCH through original bound visibly", func(t *testing.T) {
		listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess {
				listCalls++
				if listCalls == 1 {
					return []homeProcess{listed}
				}
				return nil
			},
			inspect: func(int) (stopProcessSnapshot, error) {
				inspectCalls++
				return exitingStopSnapshot(startTime), nil
			},
			home: func(int, string) (bool, error) { homeCalls++; return false, lateHomeErr },
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		stopped, err := stopHomeProcessesWithinWithProbes(home, 30*time.Millisecond, 0, probes)
		if err == nil || !errors.Is(err, errHomeObservationUnknown) || !errors.Is(err, syscall.ESRCH) ||
			!strings.Contains(err.Error(), "after the stop bound") || len(stopped) != 0 || listCalls < 2 || inspectCalls < 2 || homeCalls < 2 || killCalls != 0 {
			t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want visible ESRCH and original bound, no signal",
				stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
		}
	})
}

func TestStopHomeProcessesMixedBoundRetainsFirstLateHOMEEACCES(t *testing.T) {
	const home = "/test/home"
	const heldPID = 5151
	const ordinaryPID = 5152
	const heldStart = "876543"
	const ordinaryStart = "876544"
	const bound = 40 * time.Millisecond

	held := homeProcess{pid: heldPID, ppid: 1, args: []string{"/bin/worker"}, startTime: heldStart, homeMatched: true}
	ordinary := homeProcess{pid: ordinaryPID, ppid: 1, args: []string{"/bin/worker"}, startTime: ordinaryStart, homeMatched: true}
	lateHomeErr := fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.EACCES)
	started := time.Now()
	ordinaryReadAfterBound := started.Add(bound + 20*time.Millisecond)
	var events []string
	var signals []struct {
		pid int
		sig syscall.Signal
	}
	var heldInspects, ordinaryInspects, heldHomeReads int
	ordinaryGone := false
	probes := stopProcessProbes{
		list: func(string) []homeProcess {
			events = append(events, "list")
			return []homeProcess{held, ordinary}
		},
		inspect: func(pid int) (stopProcessSnapshot, error) {
			if pid == heldPID {
				heldInspects++
				events = append(events, "inspect-held")
				return exitingStopSnapshot(heldStart), nil
			}
			if pid != ordinaryPID {
				t.Fatalf("unexpected inspected PID %d", pid)
			}
			ordinaryInspects++
			events = append(events, "inspect-ordinary")
			if ordinaryGone {
				return stopProcessSnapshot{gone: true}, nil
			}
			if ordinaryInspects == 1 {
				if pause := time.Until(ordinaryReadAfterBound); pause > 0 {
					time.Sleep(pause)
				}
			}
			return stopProcessSnapshot{args: ordinary.args, startTime: ordinaryStart}, nil
		},
		home: func(pid int, _ string) (bool, error) {
			if pid == heldPID {
				heldHomeReads++
				events = append(events, "home-held")
				return false, lateHomeErr
			}
			if pid != ordinaryPID {
				t.Fatalf("unexpected HOME PID %d", pid)
			}
			events = append(events, "home-ordinary")
			return true, nil
		},
		kill: func(pid int, sig syscall.Signal) error {
			events = append(events, fmt.Sprintf("signal-%d-%s", pid, sig))
			signals = append(signals, struct {
				pid int
				sig syscall.Signal
			}{pid: pid, sig: sig})
			if pid == ordinaryPID && sig == syscall.SIGKILL {
				ordinaryGone = true
			}
			return nil
		},
	}

	stopped, err := stopHomeProcessesWithinWithProbes(home, bound, 0, probes)
	elapsed := time.Since(started)
	if err == nil || !errors.Is(err, errHomeObservationUnknown) || !errors.Is(err, syscall.EACCES) {
		t.Fatalf("stop=%v, %v; want mixed-bound error preserving both late HOME causes", stopped, err)
	}
	if !strings.Contains(err.Error(), "after the stop began") || !strings.Contains(err.Error(), bound.String()) {
		t.Fatalf("mixed-bound error %v does not expose the original %s bound", err, bound)
	}
	if elapsed < bound || elapsed > 2*time.Second || heldInspects != 1 || heldHomeReads != 1 || ordinaryInspects != 3 {
		t.Fatalf("elapsed=%s, held inspections=%d HOME reads=%d, ordinary inspections=%d; want one refresh-proof EACCES, original bound, ordinary SIGKILL then gone recheck",
			elapsed, heldInspects, heldHomeReads, ordinaryInspects)
	}
	if len(events) < 4 || events[1] != "inspect-held" || events[2] != "home-held" || events[3] != "inspect-ordinary" {
		t.Fatalf("probe order=%v; want held late-EACCES proof before ordinary-target refresh", events)
	}
	if len(signals) != 1 || signals[0].pid != ordinaryPID || signals[0].sig != syscall.SIGKILL {
		t.Fatalf("signals=%v; want only the ordinary target's existing bound-time SIGKILL and no signal to held PID %d", signals, heldPID)
	}
}

func TestStopHomeProcessesRetainedListingCannotChangeHeldIdentity(t *testing.T) {
	const home = "/test/home"
	const startTime = "876539"
	base := homeProcess{pid: 5148, ppid: 1, startTime: startTime, homeMatched: true}
	for _, tc := range []struct {
		name   string
		mutate func(*homeProcess)
	}{
		{name: "start time drift", mutate: func(p *homeProcess) { p.startTime = "876538" }},
		{name: "positive HOME change", mutate: func(p *homeProcess) { p.homeErr = errHomeOwnershipChanged }},
		{name: "HOME I/O error", mutate: func(p *homeProcess) { p.homeErr = fmt.Errorf("%w: disk I/O", errHomeObservationUnknown) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls == 1 {
						return []homeProcess{base}
					}
					changed := base
					tc.mutate(&changed)
					return []homeProcess{changed}
				},
				inspect: func(int) (stopProcessSnapshot, error) {
					inspectCalls++
					return exitingStopSnapshot(startTime), nil
				},
				home: func(int, string) (bool, error) {
					homeCalls++
					return false, fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.ESRCH)
				},
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err == nil || len(stopped) != 0 || listCalls != 2 || inspectCalls != 2 || homeCalls != 2 || killCalls != 0 {
				t.Fatalf("stop=%v, %v (lists=%d inspections=%d HOME=%d signals=%d); want strict refusal of contradictory/unreadable listing, no signal",
					stopped, err, listCalls, inspectCalls, homeCalls, killCalls)
			}
		})
	}
}

func TestStopHomeProcessesRetainsKernelProvenExitingIdentity(t *testing.T) {
	const home = "/test/home"
	const pid = 5151
	const startTime = "876543"
	listed := homeProcess{pid: pid, ppid: 1, startTime: startTime, homeMatched: true}
	listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
	probes := stopProcessProbes{
		list: func(string) []homeProcess {
			listCalls++
			if listCalls == 1 {
				return []homeProcess{listed}
			}
			return nil
		},
		inspect: func(int) (stopProcessSnapshot, error) {
			inspectCalls++
			if inspectCalls == 3 {
				return stopProcessSnapshot{gone: true}, nil
			}
			return exitingStopSnapshot(startTime), nil
		},
		home: func(int, string) (bool, error) {
			homeCalls++
			return false, fmt.Errorf("%w: environment disappeared during exit", errHomeEnvironmentCleared)
		},
		kill: func(int, syscall.Signal) error { killCalls++; return nil },
	}

	stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
	if err != nil || len(stopped) != 0 || killCalls != 0 {
		t.Fatalf("stop = %v, %v, signals=%d; want held identity retained until explicit gone without signal", stopped, err, killCalls)
	}
	if listCalls != 3 || inspectCalls != 3 || homeCalls != 2 {
		t.Errorf("lists=%d inspections=%d HOME observations=%d; want census omission to trigger identity checks through explicit gone", listCalls, inspectCalls, homeCalls)
	}
}

func TestStopHomeProcessesReportsExitingIdentityAtBound(t *testing.T) {
	const home = "/test/home"
	const pid = 5152
	const startTime = "876544"
	listed := homeProcess{pid: pid, ppid: 1, startTime: startTime, homeMatched: true}
	listCalls, inspectCalls, killCalls := 0, 0, 0
	probes := stopProcessProbes{
		list: func(string) []homeProcess {
			listCalls++
			if listCalls == 1 {
				return []homeProcess{listed}
			}
			return nil
		},
		inspect: func(int) (stopProcessSnapshot, error) {
			inspectCalls++
			return exitingStopSnapshot(startTime), nil
		},
		home: func(int, string) (bool, error) {
			return false, fmt.Errorf("%w: environment disappeared during exit", errHomeEnvironmentCleared)
		},
		kill: func(int, syscall.Signal) error { killCalls++; return nil },
	}
	started := time.Now()
	stopped, err := stopHomeProcessesWithinWithProbes(home, 30*time.Millisecond, 0, probes)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "exiting") || !strings.Contains(err.Error(), "still live") {
		t.Fatalf("stop = %v, %v; want visible bounded exiting-process failure", stopped, err)
	}
	if len(stopped) != 0 || killCalls != 0 || inspectCalls < 2 || listCalls < 2 {
		t.Errorf("stop = %v, %v (lists %d inspections %d signals %d); want retained identity, no signal, and bounded reinspection", stopped, err, listCalls, inspectCalls, killCalls)
	}
	if elapsed < 30*time.Millisecond || elapsed > time.Second {
		t.Errorf("bounded wait took %s; want about 30ms and under 1s", elapsed)
	}
}

func TestStopHomeProcessesDoesNotClassifyUncertainExitEvidence(t *testing.T) {
	const home = "/test/home"
	const startTime = "876545"
	base := homeProcess{pid: 5153, ppid: 1, startTime: startTime, homeMatched: true}
	cases := []struct {
		name     string
		snapshot stopProcessSnapshot
		home     func(int, string) (bool, error)
	}{
		{name: "late stat belongs to reused pid", snapshot: func() stopProcessSnapshot { s := exitingStopSnapshot(startTime); s.lateStartTime = "876546"; return s }(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "late stat missing", snapshot: func() stopProcessSnapshot {
			s := exitingStopSnapshot(startTime)
			s.lateStartTimeFound = false
			return s
		}(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "late stat malformed", snapshot: func() stopProcessSnapshot {
			s := exitingStopSnapshot(startTime)
			s.lateStartTimeErr = errors.New("malformed stat")
			return s
		}(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "flags unavailable", snapshot: func() stopProcessSnapshot {
			s := exitingStopSnapshot(startTime)
			s.flagsFound = false
			s.flagsErr = errors.New("malformed flags")
			return s
		}(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "flags unsupported platform", snapshot: func() stopProcessSnapshot {
			s := exitingStopSnapshot(startTime)
			s.flagsFound = false
			s.flagsErr = errProcessStatFlagsUnavailable
			return s
		}(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "flags clear", snapshot: func() stopProcessSnapshot { s := exitingStopSnapshot(startTime); s.flags = 0; return s }(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "PF-clear late EACCES is not retained", snapshot: func() stopProcessSnapshot { s := exitingStopSnapshot(startTime); s.flags = 0; return s }(), home: func(int, string) (bool, error) {
			return false, fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.EACCES)
		}},
		{name: "invalid process state", snapshot: func() stopProcessSnapshot { s := exitingStopSnapshot(startTime); s.state = "?"; return s }(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "dead exited state", snapshot: func() stopProcessSnapshot { s := exitingStopSnapshot(startTime); s.state = "X"; return s }(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "dead exited lowercase state", snapshot: func() stopProcessSnapshot { s := exitingStopSnapshot(startTime); s.state = "x"; return s }(), home: func(int, string) (bool, error) { return true, nil }},
		{name: "HOME changed", snapshot: exitingStopSnapshot(startTime), home: func(int, string) (bool, error) { return false, errHomeOwnershipChanged }},
		{name: "HOME read error", snapshot: exitingStopSnapshot(startTime), home: func(int, string) (bool, error) { return false, errors.New("environ I/O error") }},
		{name: "HOME EIO remains strict", snapshot: exitingStopSnapshot(startTime), home: func(int, string) (bool, error) {
			return false, fmt.Errorf("%w: %w", errHomeObservationUnknown, syscall.EIO)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listCalls, inspectCalls, killCalls := 0, 0, 0
			probes := stopProcessProbes{
				list:    func(string) []homeProcess { listCalls++; return []homeProcess{base} },
				inspect: func(int) (stopProcessSnapshot, error) { inspectCalls++; return tc.snapshot, nil },
				home:    tc.home,
				kill:    func(int, syscall.Signal) error { killCalls++; return nil },
			}
			_, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err == nil || inspectCalls != 1 || listCalls != 1 || killCalls != 0 {
				t.Fatalf("stop error=%v, lists=%d inspections=%d signals=%d; want one fatal observation and no signal", err, listCalls, inspectCalls, killCalls)
			}
		})
	}
}

func TestStopHomeProcessesRetainedExitMustKeepKernelEvidence(t *testing.T) {
	const home = "/test/home"
	const startTime = "876547"
	listed := homeProcess{pid: 5154, ppid: 1, startTime: startTime, homeMatched: true}
	for _, tc := range []struct {
		name   string
		mutate func(*stopProcessSnapshot)
	}{
		{name: "flags disappear", mutate: func(s *stopProcessSnapshot) { s.flags = 0 }},
		{name: "late identity unavailable", mutate: func(s *stopProcessSnapshot) {
			s.lateStartTimeFound = false
			s.lateStartTimeErr = errors.New("missing starttime")
		}},
		{name: "late identity changes", mutate: func(s *stopProcessSnapshot) { s.lateStartTime = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls == 1 {
						return []homeProcess{listed}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) {
					inspectCalls++
					s := exitingStopSnapshot(startTime)
					if inspectCalls > 1 {
						tc.mutate(&s)
					}
					return s, nil
				},
				home: func(int, string) (bool, error) { homeCalls++; return false, errHomeEnvironmentCleared },
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			_, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err == nil || !strings.Contains(err.Error(), "exiting process identity") || killCalls != 0 || inspectCalls != 2 {
				t.Fatalf("stop error=%v (inspections=%d HOME=%d signals=%d); want fail-closed held-identity error", err, inspectCalls, homeCalls, killCalls)
			}
		})
	}
}

func TestStopHomeProcessesZeroBoundKeepsLiveEmptyFatal(t *testing.T) {
	const home = "/test/home"
	const startTime = "876548"
	listed := homeProcess{pid: 5155, ppid: 1, startTime: startTime, homeMatched: true}
	listCalls, inspectCalls, killCalls := 0, 0, 0
	probes := stopProcessProbes{
		list:    func(string) []homeProcess { listCalls++; return []homeProcess{listed} },
		inspect: func(int) (stopProcessSnapshot, error) { inspectCalls++; return exitingStopSnapshot(startTime), nil },
		home:    func(int, string) (bool, error) { return true, nil },
		kill:    func(int, syscall.Signal) error { killCalls++; return nil },
	}
	stopped, err := stopHomeProcessesWithinWithProbes(home, 0, 0, probes)
	if err == nil || !strings.Contains(err.Error(), "live process has no classifiable arguments") ||
		!strings.Contains(err.Error(), "flags=4") || len(stopped) != 0 || listCalls != 1 || inspectCalls != 1 || killCalls != 0 {
		t.Fatalf("stop=%v, %v (lists=%d inspections=%d signals=%d); want one original fatal observation and no signal", stopped, err, listCalls, inspectCalls, killCalls)
	}
}

func TestStopHomeProcessesRetainedExitRejectsHOMEChangeAndReadError(t *testing.T) {
	const home = "/test/home"
	const startTime = "876549"
	listed := homeProcess{pid: 5156, ppid: 1, startTime: startTime, homeMatched: true}
	for _, tc := range []struct {
		name   string
		second error
	}{
		{name: "HOME changed", second: errHomeOwnershipChanged},
		{name: "HOME read failed", second: errors.New("environ I/O error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listCalls, inspectCalls, homeCalls, killCalls := 0, 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls == 1 {
						return []homeProcess{listed}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) { inspectCalls++; return exitingStopSnapshot(startTime), nil },
				home: func(int, string) (bool, error) {
					homeCalls++
					if homeCalls == 1 {
						return false, errHomeEnvironmentCleared
					}
					return false, tc.second
				},
				kill: func(int, syscall.Signal) error { killCalls++; return nil },
			}
			_, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
			if err == nil || inspectCalls != 2 || homeCalls != 2 || killCalls != 0 {
				t.Fatalf("stop error=%v (lists=%d inspections=%d HOME=%d signals=%d); want fatal held-identity HOME failure", err, listCalls, inspectCalls, homeCalls, killCalls)
			}
		})
	}
}

func TestStopHomeProcessesLiveEmptyArgumentsIncludesBoundedDiagnostic(t *testing.T) {
	const home = "/private/test-home"
	const pid = 4242
	const startTime = "987654"
	listCalls, inspectCalls, killCalls := 0, 0, 0
	probes := stopProcessProbes{
		list: func(string) []homeProcess {
			listCalls++
			return []homeProcess{{pid: pid, startTime: startTime}}
		},
		inspect: func(int) (stopProcessSnapshot, error) {
			inspectCalls++
			return stopProcessSnapshot{
				startTime: startTime, state: "R", stateFound: true, flags: 4, flagsFound: true,
				reads: []string{"gone-before", "start-time", "cmdline", "gone-after"}, observedAt: time.Now(),
			}, nil
		},
		home: func(int, string) (bool, error) { return true, nil },
		kill: func(int, syscall.Signal) error { killCalls++; return nil },
	}

	stopped, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
	if err == nil || !strings.Contains(err.Error(), "live process has no classifiable arguments") {
		t.Fatalf("stop error = %v, want the original live-empty fatal error", err)
	}
	for _, evidence := range []string{
		"empty-argv-observation{", "pid=4242", "start=987654", "home=matched",
		"args=0", "state=R", "flags=4", "flags-error=none",
		"reads=gone-before,start-time,cmdline,gone-after", "observed=",
	} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("diagnostic does not contain %q: %v", evidence, err)
		}
	}
	if strings.Contains(err.Error(), home) || strings.Contains(err.Error(), "argument-content") {
		t.Errorf("diagnostic exposed HOME or argument contents: %v", err)
	}
	if len(stopped) != 0 || listCalls != 1 || inspectCalls != 1 || killCalls != 0 {
		t.Errorf("stop = %v, %v (lists %d, inspections %d, signals %d); want original fail-closed decision and no signal",
			stopped, err, listCalls, inspectCalls, killCalls)
	}
}

func TestStopHomeProcessesRevalidatesBeforeSignalling(t *testing.T) {
	const home = "/test/home"
	base := homeProcess{pid: 27182, ppid: 1, args: []string{"/bin/worker"}, startTime: "417"}
	cases := []struct {
		name             string
		listed           homeProcess
		inspect          func(int) (stopProcessSnapshot, error)
		killErr          error
		wantErr          bool
		wantSignal       bool
		zeroBound        bool
		wantInspectCalls int
	}{
		{
			name:    "gone after empty-argv listing",
			listed:  homeProcess{pid: base.pid, ppid: base.ppid, startTime: base.startTime},
			inspect: func(int) (stopProcessSnapshot, error) { return stopProcessSnapshot{gone: true}, nil },
		},
		{
			name:   "known scratch remover now has empty live argv",
			listed: homeProcess{pid: base.pid, ppid: base.ppid, args: []string{"yolo", "internal", "scratch-rm"}, startTime: base.startTime},
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{startTime: base.startTime}, nil
			},
			wantErr:          true,
			zeroBound:        true,
			wantInspectCalls: 1,
		},
		{
			name:   "expired bound with live scratch remover",
			listed: homeProcess{pid: base.pid, ppid: base.ppid, args: []string{"yolo", "internal", "scratch-rm"}, startTime: base.startTime},
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: []string{"yolo", "internal", "scratch-rm"}, startTime: base.startTime}, nil
			},
			wantErr:   true,
			zeroBound: true,
		},
		{
			name:    "empty argv while still live",
			listed:  homeProcess{pid: base.pid, ppid: base.ppid, startTime: base.startTime},
			inspect: func(int) (stopProcessSnapshot, error) { return stopProcessSnapshot{startTime: base.startTime}, nil },
			wantErr: true,
		},
		{
			name:   "gone at final signal validation",
			listed: base,
			inspect: func(call int) (stopProcessSnapshot, error) {
				if call == 1 {
					return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
				}
				return stopProcessSnapshot{gone: true}, nil
			},
		},
		{
			name:   "empty argv at final signal validation",
			listed: base,
			inspect: func(call int) (stopProcessSnapshot, error) {
				if call == 1 {
					return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
				}
				return stopProcessSnapshot{startTime: base.startTime}, nil
			},
			wantErr: true,
		},
		{
			name:    "unreadable live identity",
			listed:  base,
			inspect: func(int) (stopProcessSnapshot, error) { return stopProcessSnapshot{}, errors.New("permission denied") },
			wantErr: true,
		},
		{
			name:   "pid reused",
			listed: base,
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: []string{"/bin/other"}, startTime: "418"}, nil
			},
			wantErr: true,
		},
		{
			name:   "signal permission error",
			listed: base,
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			killErr:    syscall.EPERM,
			wantErr:    true,
			wantSignal: true,
		},
		{
			name:   "gone at signal delivery",
			listed: base,
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			killErr:    syscall.ESRCH,
			wantSignal: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listCalls, inspectCalls, killCalls := 0, 0, 0
			probes := stopProcessProbes{
				list: func(string) []homeProcess {
					listCalls++
					if listCalls == 1 {
						return []homeProcess{tc.listed}
					}
					return nil
				},
				inspect: func(int) (stopProcessSnapshot, error) {
					inspectCalls++
					return tc.inspect(inspectCalls)
				},
				kill: func(int, syscall.Signal) error {
					killCalls++
					return tc.killErr
				},
			}
			bound := time.Second
			if tc.zeroBound {
				bound = 0
			}
			stopped, err := stopHomeProcessesWithinWithProbes(home, bound, 0, probes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("stop error = %v, wantErr %v", err, tc.wantErr)
			}
			if (killCalls > 0) != tc.wantSignal {
				t.Errorf("signal attempts = %d, wantSignal %v", killCalls, tc.wantSignal)
			}
			if tc.wantInspectCalls > 0 && inspectCalls != tc.wantInspectCalls {
				t.Errorf("inspection calls = %d, want %d (reject current argv during listing refresh)", inspectCalls, tc.wantInspectCalls)
			}
			if len(stopped) != 0 {
				t.Errorf("reported stopped %v, want none", stopped)
			}
		})
	}
}

// A process spawned WHILE the stop runs is stopped too, since the stop repeats until a listing is
// empty: this stand-in scheduler starts a worker as it winds down, detached, as a real one can.
func TestStopHomeProcessesStopsWhatIsSpawnedDuringTheStop(t *testing.T) {
	home := t.TempDir()
	pidDir := t.TempDir()
	pidFile := filepath.Join(pidDir, "late.pid")
	readyFile := filepath.Join(pidDir, "scheduler-ready")
	scheduler := startStandIn(t, home,
		[]string{"LATE_PID=" + pidFile, "READY_FILE=" + readyFile, "WORKER=" + standInScript},
		"sh", `trap '(sh -c "$WORKER" internal background-advance </dev/null >/dev/null 2>&1 & echo $! > "$LATE_PID"); kill $!; exit 143' TERM; : > "$READY_FILE"; sleep 600 & wait`,
		"internal", "preparation-scheduler")
	t.Cleanup(func() {
		stopStandIn(scheduler)
		if raw, err := os.ReadFile(pidFile); err == nil {
			late, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
			stopStandIn(late)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat scheduler readiness marker: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stand-in scheduler did not install its stop handler within 10s")
		}
		time.Sleep(time.Millisecond)
	}

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
