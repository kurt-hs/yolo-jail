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
// test's process or the machine's. The /proc start time ties the listed pid to the same process
// before a signal, rather than trusting the pid alone. Two kinds are left out: the test binary's
// own descendants, which are the exec.Cmd children its own cleanups own (a stand-in, a session it
// still holds), and a scratch remover (`internal scratch-rm`), which ends on its own within its
// bound and is WAITED for, never stopped, since stopping it leaks the volumes it was removing. On
// a host without /proc nothing is identified and nothing is stopped.
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

// homeProcess is one live process running under the test's HOME.
type homeProcess struct {
	pid, ppid int
	args      []string
	argsErr   error
	startTime string
	statErr   error
	homeErr   error
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

// processStartTime is the starttime field in /proc/<pid>/stat, which distinguishes a live process
// from a different process that later reused its pid.
func processStartTime(pid int) (string, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(stat)[i+1:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("short /proc/%d/stat", pid)
	}
	return fields[19], nil
}

func processHomeObservation(pid int, home string) (bool, error) {
	if goruntime.GOOS != "linux" {
		return false, fmt.Errorf("%w: /proc is unavailable", errHomeObservationUnknown)
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return false, fmt.Errorf("%w: read /proc/%d/environ: %v", errHomeObservationUnknown, pid, err)
	}
	want := "HOME=" + filepath.Clean(home)
	for _, kv := range strings.Split(string(raw), "\x00") {
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
	gone       bool
	args       []string
	startTime  string
	state      string
	stateFound bool
	killErr    error
	stateErr   error
	reads      []string
	observedAt time.Time
}

var errHomeObservationUnknown = errors.New("HOME observation was unreadable")
var errHomeOwnershipChanged = errors.New("process no longer has the listed HOME")

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

func validateStopTarget(_ string, p homeProcess, probes stopProcessProbes) (stopProcessSnapshot, bool, error) {
	snapshot, err := probes.inspect(p.pid)
	if err != nil {
		return stopProcessSnapshot{}, false, fmt.Errorf("revalidating %s before signalling: %w", p, err)
	}
	if snapshot.gone {
		return snapshot, false, nil
	}
	if p.homeErr != nil {
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
		if err != nil && !errors.Is(err, errHomeObservationUnknown) {
			return homeProcess{}, false, fmt.Errorf("revalidating %s HOME ownership: %w", p, err)
		}
		if err == nil && !matches {
			return homeProcess{}, false, fmt.Errorf("revalidating %s: process no longer has HOME=%s", p, home)
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
	case errors.Is(err, errHomeObservationUnknown):
		return "home-unreadable"
	case errors.Is(err, errHomeOwnershipChanged):
		return "home-changed"
	case errors.Is(err, os.ErrNotExist):
		return "not-found"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, syscall.ESRCH):
		return "no-such-process"
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
	return fmt.Sprintf("empty-argv-observation{pid=%d,start=%s,home=%s,listed-args=%d,listed-args-error=%s,args=%d,args-error=%s,state=%s,state-error=%s,kill-error=%s,reads=%s,observed=%s}",
		p.pid, p.startTime, homeClass, len(p.args), stopDiagnosticErrorClass(p.argsErr),
		len(snapshot.args), stopDiagnosticErrorClass(nil), stopDiagnosticState(snapshot),
		stopDiagnosticErrorClass(snapshot.stateErr), stopDiagnosticErrorClass(snapshot.killErr), readOrder, observedAt)
}

func refreshStopListing(home string, procs []homeProcess, deadline time.Time, probes stopProcessProbes) ([]homeProcess, error) {
	var current []homeProcess
	for _, p := range procs {
		snapshot, live, err := validateStopTarget(home, p, probes)
		var uncertain *uncertainHomeObservationError
		if err != nil && errors.As(err, &uncertain) {
			var refreshed homeProcess
			var stillLive bool
			refreshed, stillLive, err = reobserveUncertainHome(home, p, deadline, probes)
			if err == nil && stillLive {
				p = refreshed
				snapshot, live = stopProcessSnapshot{args: p.args, startTime: p.startTime}, true
			}
		}
		if err != nil {
			if p.argsErr != nil {
				return nil, fmt.Errorf("revalidating %s after its initial argument read failed: %w; revalidation: %v", p, p.argsErr, err)
			}
			return nil, err
		}
		if !live {
			continue
		}
		if len(snapshot.args) == 0 {
			original := fmt.Errorf("revalidating %s before signalling: live process has no classifiable arguments", p)
			return nil, fmt.Errorf("%w; %s", original, emptyArgStopDiagnostic(p, snapshot))
		}
		p.args = snapshot.args
		p.argsErr = nil
		current = append(current, p)
	}
	return current, nil
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
	deadline := time.Now().Add(bound)
	for {
		procs := probes.list(home)
		if len(procs) == 0 {
			return stopped, nil
		}
		var err error
		procs, err = refreshStopListing(home, procs, deadline, probes)
		if err != nil {
			return stopped, err
		}
		if len(procs) == 0 {
			return stopped, nil
		}
		if time.Now().After(deadline) {
			var left, notSignalled, failures []string
			var signalled []homeProcess
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
				return stopped, errors.New(message)
			}
			return stopped, nil
		}
		targets := stopRound(procs)
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

	t.Run("confirmed ownership change is not retried", func(t *testing.T) {
		changed := base
		changed.homeErr = errHomeOwnershipChanged
		listCalls, killCalls := 0, 0
		probes := stopProcessProbes{
			list: func(string) []homeProcess { listCalls++; return []homeProcess{changed} },
			inspect: func(int) (stopProcessSnapshot, error) {
				return stopProcessSnapshot{args: base.args, startTime: base.startTime}, nil
			},
			kill: func(int, syscall.Signal) error { killCalls++; return nil },
		}
		_, err := stopHomeProcessesWithinWithProbes(home, time.Second, 0, probes)
		if err == nil || listCalls != 1 || killCalls != 0 {
			t.Fatalf("stop error %v, lists %d, signals %d; want immediate conservative error", err, listCalls, killCalls)
		}
	})
}

func TestInspectStopProcessRecordsReadOrderForLiveProcess(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("the cleanup identity probe reads /proc on Linux")
	}
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
	if got, want := strings.Join(snapshot.reads, ","), "gone-before,start-time,cmdline,gone-after"; got != want {
		t.Errorf("probe read order = %q, want %q", got, want)
	}
	if snapshot.observedAt.IsZero() {
		t.Error("live-process snapshot has no observation time")
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
				startTime: startTime, state: "S", stateFound: true,
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
		"args=0", "state=S", "reads=gone-before,start-time,cmdline,gone-after", "observed=",
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
