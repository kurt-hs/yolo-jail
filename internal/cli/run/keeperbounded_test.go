package run

// keeperbounded_test.go pins `yolo stop`'s BOUNDED default wait and its --force escalation
// (docs/design/jail-lifetime-last-session-wins.md JL-D34):
//
//   - the default wait is BOUNDED by keeperTeardownWait: a keeper that outlasts it is left running,
//     its facts and the `yolo stop --force` remedy are printed, and the stop returns non-zero rather
//     than polling on;
//   - --force signals the keeper (SIGTERM, then SIGKILL after stopForceGrace), so a keeper that will
//     not finish no longer holds the stop up;
//   - a Ctrl-C during the wait still means "stop following": 128+SIGINT, the keeper left running.

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// boundedStopOptions is a fixture whose reap of an unkept keeper is a no-op: the runtime is never
// reached (Ran:true, empty answers), and the loophole dirs are emptied, as the unkept-jail fixture.
func boundedStopOptions(t *testing.T) (*Options, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	emptyLoopholeDirs(t)
	o := goldenOptions("/ws", t.TempDir())
	var out bytes.Buffer
	o.Stdout, o.Stderr = &out, &out
	o.Exec = func([]string, string, []string, time.Duration) ExecResult {
		return ExecResult{Ran: true}
	}
	return o, &out
}

// TestABoundedStopReturnsAtTheBoundAndNamesForce: the bounded default. The keeper is held well
// past the shrunk bound and released only after the stop has returned; the stop must NOT wait it
// out — it returns 1 at the bound, having printed the keeper's facts and `yolo stop --force`. On the
// old patient default it waited the keeper out and returned 0, so rc==1 (and the elapsed check)
// fails on that behavior.
func TestABoundedStopReturnsAtTheBoundAndNamesForce(t *testing.T) {
	savedWait := keeperTeardownWait
	keeperTeardownWait = 50 * time.Millisecond
	t.Cleanup(func() { keeperTeardownWait = savedWait })

	o, out := boundedStopOptions(t)
	const cname = "yolo-bounded"
	live, err := holdLivenessLock(cname)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeKeeperRecord(cname, keeperRecord{PID: 4242}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(500 * time.Millisecond) // ten times the bound: only a patient wait is still there
		releaseLock(live)
	}()
	start := time.Now()
	rc := o.finishStop(cname, "podman", 0, false)
	if rc != 1 {
		t.Fatalf("rc=%d, the bounded stop must fail at the bound rather than wait the keeper out:\n%s", rc, out.String())
	}
	// The bound is honored, not merely eventually reached.
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("the stop took %s: it kept polling past keeperTeardownWait (%s)", elapsed, keeperTeardownWait)
	}
	for _, want := range []string{"yolo stop --force", "(pid 4242)", "has not finished"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the bounded report must say %q:\n%s", want, out.String())
		}
	}
}

// TestAForcedStopEscalatesAKeeperThatWillNotFinish: --force SIGTERMs the keeper, gives it
// stopForceGrace, and SIGKILLs it if it has not ended in order.
func TestAForcedStopEscalatesAKeeperThatWillNotFinish(t *testing.T) {
	cases := []struct {
		name   string
		endOn  syscall.Signal
		want   []syscall.Signal
		killed bool
	}{
		{"a keeper that ends in order is not killed", syscall.SIGTERM, []syscall.Signal{syscall.SIGTERM}, false},
		{"a keeper that will not end is SIGKILLed", syscall.SIGKILL, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			savedGrace := stopForceGrace
			stopForceGrace = 50 * time.Millisecond
			t.Cleanup(func() { stopForceGrace = savedGrace })
			savedSignal := signalKeeperProcess
			t.Cleanup(func() { signalKeeperProcess = savedSignal })

			o, out := boundedStopOptions(t)
			const cname = "yolo-forced"
			live, err := holdLivenessLock(cname)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeKeeperRecord(cname, keeperRecord{PID: 4242}); err != nil {
				t.Fatal(err)
			}
			var got []syscall.Signal
			signalKeeperProcess = func(_ int, sig syscall.Signal) error {
				got = append(got, sig)
				if sig == tc.endOn {
					releaseLock(live)
				}
				return nil
			}
			if rc := o.finishStop(cname, "podman", 0, true); rc != 0 {
				t.Fatalf("rc=%d, the forced stop did not end the keeper:\n%s", rc, out.String())
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("signals = %v, want %v", got, tc.want)
			}
			if said := strings.Contains(out.String(), "sent SIGKILL"); said != tc.killed {
				t.Errorf("SIGKILL reported = %v, want %v:\n%s", said, tc.killed, out.String())
			}
		})
	}
}

// TestACtrlCDuringTheStopStopsFollowing: a SIGINT during the wait still means "stop following" —
// 128+SIGINT, the keeper left running — never a give-up. The signal arrives well within the bound,
// so the wait is still running when it lands.
func TestACtrlCDuringTheStopStopsFollowing(t *testing.T) {
	savedWait := keeperTeardownWait
	keeperTeardownWait = 2 * time.Second
	t.Cleanup(func() { keeperTeardownWait = savedWait })

	o, out := boundedStopOptions(t)
	const cname = "yolo-ctrlc"
	live, err := holdLivenessLock(cname)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLock(live)
	catchSignal(t, syscall.SIGINT)
	go func() {
		time.Sleep(100 * time.Millisecond) // within the bound: the stop is still waiting
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	rc := o.finishStop(cname, "podman", 0, false)
	if rc != 128+int(syscall.SIGINT) {
		t.Fatalf("rc=%d, want %d (stop following):\n%s", rc, 128+int(syscall.SIGINT), out.String())
	}
	if !strings.Contains(out.String(), "Stopped following the teardown") {
		t.Errorf("the Ctrl-C must say it stopped following:\n%s", out.String())
	}
}
