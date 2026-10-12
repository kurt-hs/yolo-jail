package run

// keeperdrain_test.go pins that the keeper's session drain survives a signal-interrupted take
// (docs/design/jail-lifetime-last-session-wins.md JL-P3): a blocking LOCK_EX that returns EINTR must
// be retried, or the drain goroutine returns with its channel never closed and the keeper can never
// end its jail on the count again for the jail's whole life.

import (
	"bytes"
	"syscall"
	"testing"
	"time"
)

// TestTheKeepersDrainSurvivesAnInterruptedTake: the session-lock take a keeper blocks on is retried
// when a signal interrupts it. Before the retry, the take's EINTR left the drain channel unclosed,
// so the keeper was permanently deaf to its last session leaving.
func TestTheKeepersDrainSurvivesAnInterruptedTake(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cname = "yolo-eintr-drain"
	orig := flockSyscall
	t.Cleanup(func() { flockSyscall = orig })
	interrupted := false
	flockSyscall = func(fd, how int) error {
		if how == syscall.LOCK_EX && !interrupted {
			interrupted = true
			return syscall.EINTR
		}
		return orig(fd, how)
	}
	k := &keeper{
		plan: &keeperPlan{Cname: cname, Runtime: "podman"},
		sink: &keeperSink{log: &bytes.Buffer{}},
	}
	drained := k.watchSessions()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("the keeper's drain did not survive an EINTR-interrupted take")
	}
	if !interrupted {
		t.Fatal("the test never interrupted the take")
	}
	if k.sessions == nil {
		t.Error("the keeper did not keep the session lock it drained on")
	}
}
