//go:build linux

package run

// sessionholders_linux_test.go pins the Linux half of the diagnostics (sessionholders_linux.go):
// the real holder of a jail's session lock, named by pid from /proc/locks, replaces the phantom
// count a failed runtime read would otherwise fabricate (JL-P3).

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// TestAStaysUpLineNamesTheSessionLockHolder: the count a failed runtime read cannot give is replaced
// by the real holder of the session lock, named by pid — never the phantom "1 other session". The
// holder here is this test process, which takes the lock shared as another session would.
func TestAStaysUpLineNamesTheSessionLockHolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cname = "yolo-holder-name"
	other, _, err := takeSessionLock(cname)
	if err != nil {
		t.Fatal(err)
	}
	defer other.release()
	o := goldenOptions("/ws", t.TempDir())
	var errBuf bytes.Buffer
	o.Stderr = &errBuf
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		if strings.Contains(strings.Join(argv, " "), "ExecIDs") {
			return ExecResult{Ran: true, Stdout: "0\n"}
		}
		return ExecResult{Ran: true, RC: 125} // the environment read failed
	}
	o.noteJailStaysUp(cname, "podman")
	got := errBuf.String()
	if strings.Contains(got, "1 other session") {
		t.Errorf("a failed count fabricated a session:\n%s", got)
	}
	if !strings.Contains(got, fmt.Sprintf("pid %d", os.Getpid())) {
		t.Errorf("the line must name the session-lock holder (pid %d):\n%s", os.Getpid(), got)
	}
}

// TestTheKeeperLogsTheSessionLockHolderWhenItKeepsTheJail: the same holders a session's stays-up
// line names reach the keeper's log at the moment the keeper decides to keep the jail — its
// main-process client exiting under a container that still runs (JL-P3's diagnostics). The holder
// is this test process's shared session lock.
func TestTheKeeperLogsTheSessionLockHolderWhenItKeepsTheJail(t *testing.T) {
	held := heldDir(t)
	die := filepath.Join(held, "die")
	var session *sessionLock
	f := startKeeperFixture(t, true, func(p *keeperPlan) {
		lock, _, err := takeSessionLock(p.Cname)
		if err != nil {
			t.Fatal(err)
		}
		session = lock
		p.RunCmd = []string{"sh", "-c", recordPID(held) + `echo "` + entrypoint.BootReadyLine + `" >&2; ` +
			holdUntil(die) + `; exit 1`}
	})
	if !f.relay() {
		t.Fatalf("the relay ended before ready:\n%s", f.errOut.String())
	}
	// The client dies on its own; the fake runtime still says the container runs.
	if err := os.WriteFile(die, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("pid %d", os.Getpid())
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(f.keeperLog(), "keeping the jail") {
		time.Sleep(20 * time.Millisecond)
	}
	// The holders must be on the SAME line as the decision: the keeper also logs its own pid when it
	// starts, so a whole-log search would pass on that alone.
	keep := ""
	for _, l := range strings.Split(f.keeperLog(), "\n") {
		if strings.Contains(l, "keeping the jail") {
			keep = l
		}
	}
	if keep == "" || !strings.Contains(keep, want) {
		t.Errorf("the keeper's decision to keep the jail did not name its session-lock holder (pid %d):\n%s",
			os.Getpid(), f.keeperLog())
	}
	session.release()
	if rc := f.wait(); rc != 0 {
		t.Errorf("the keeper ended %d", rc)
	}
}
