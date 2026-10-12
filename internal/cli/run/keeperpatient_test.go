package run

// keeperpatient_test.go pins `yolo stop`'s default wait and its --force escalation
// (docs/design/jail-lifetime-last-session-wins.md JL-D34):
//
//   - the default wait is PATIENT: past keeperTeardownWait — the bound the launch-path waits still
//     refuse on — it reports the keeper's facts with the --force remedy and keeps waiting, instead
//     of giving up and failing the stop;
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

// patientStopOptions is a fixture whose reap of an unkept keeper is a no-op: the runtime is never
// reached (Ran:true, empty answers), and the loophole dirs are emptied, as the unkept-jail fixture.
func patientStopOptions(t *testing.T) (*Options, *bytes.Buffer) {
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

// TestAStopWaitsOutALongKeeperInsteadOfGivingUp: the patient default. keeperTeardownWait is shrunk
// so the progress line comes fast, and the keeper is released well past it; the stop must report the
// keeper's facts with the --force remedy and return success only once the keeper is actually gone.
func TestAStopWaitsOutALongKeeperInsteadOfGivingUp(t *testing.T) {
	savedWait, savedEvery := keeperTeardownWait, keeperStillRunningEvery
	keeperTeardownWait, keeperStillRunningEvery = 50*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { keeperTeardownWait, keeperStillRunningEvery = savedWait, savedEvery })

	o, out := patientStopOptions(t)
	const cname = "yolo-patient"
	live, err := holdLivenessLock(cname)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeKeeperRecord(cname, keeperRecord{PID: 4242}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond) // six times the bound: a bounded wait gives up long before
		releaseLock(live)
	}()
	if rc := o.finishStop(cname, "podman", 0, false); rc != 0 {
		t.Fatalf("rc=%d, the stop gave up rather than waiting the keeper out:\n%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "yolo stop --force") {
		t.Errorf("the patient wait must point at --force:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "(pid 4242)") {
		t.Errorf("the progress line must name the keeper's pid:\n%s", out.String())
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

			o, out := patientStopOptions(t)
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

// TestACtrlCDuringThePatientStopStopsFollowing: a SIGINT once the wait has outlasted the old bound
// still means "stop following" — 128+SIGINT, the keeper left running — never a give-up. Pre-change,
// with keeperTeardownWait shrunk this small, the stop returned 1 long before the signal arrived.
func TestACtrlCDuringThePatientStopStopsFollowing(t *testing.T) {
	savedWait := keeperTeardownWait
	keeperTeardownWait = 100 * time.Millisecond
	t.Cleanup(func() { keeperTeardownWait = savedWait })

	o, out := patientStopOptions(t)
	const cname = "yolo-ctrlc"
	live, err := holdLivenessLock(cname)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLock(live)
	catchSignal(t, syscall.SIGINT)
	go func() {
		time.Sleep(500 * time.Millisecond) // past the shrunk bound: only a patient wait is still there
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
