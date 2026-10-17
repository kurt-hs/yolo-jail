package capture

// endgroup_test.go pins Options.EndProcessGroup (pi-extensions.md PPX-D49): an installer — the sealed
// macos-user build act's build line — that exits, having left a child running, takes the child with
// it before Run returns, whether it succeeded or failed, and a child that ignores SIGTERM is killed.
// The maintainer's Mac met it: a failed build left a sandbox-account process alive past the act's
// end, so the guest pack tree the act staged was kept.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// processAlive reports whether pid is a running process: a zombie, which is gone but not yet reaped
// by whatever adopted it, counts as gone.
func processAlive(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return false
	}
	if b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
		if i := strings.LastIndexByte(string(b), ')'); i >= 0 && i+2 < len(b) && b[i+2] == 'Z' {
			return false
		}
	}
	return true
}

// leftChild runs an installer that starts a background child, records its pid and exits rc, under
// EndProcessGroup = end, and returns the child's pid and Run's error. The child is killed at
// cleanup if it is still running.
func leftChild(t *testing.T, child string, rc int, end bool) (int, error) {
	t.Helper()
	home := t.TempDir()
	fixtureHome(t, home)
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := writeInstaller(t, "("+child+") & echo $! > '"+pidFile+"'\nexit "+strconv.Itoa(rc)+"\n")
	_, err := Run(Options{Home: home, Out: filepath.Join(t.TempDir(), "out"), Command: cmd, EndProcessGroup: end})
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatalf("the installer recorded no child: %v (run: %v)", rerr, err)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(b)))
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid, err
}

func TestEndProcessGroupEndsWhatTheInstallerLeftRunning(t *testing.T) {
	prev := processGroupGrace
	processGroupGrace = 200 * time.Millisecond
	t.Cleanup(func() { processGroupGrace = prev })
	for _, tc := range []struct {
		name  string
		child string
		rc    int
	}{
		{"a build line that failed", "sleep 30", 3},
		{"a build line that succeeded", "sleep 30", 0},
		{"a child that ignores SIGTERM", "trap '' TERM; sleep 30", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pid, err := leftChild(t, tc.child, tc.rc, true)
			if (err != nil) != (tc.rc != 0) {
				t.Errorf("Run's error = %v, want one iff the installer failed", err)
			}
			if processAlive(pid) {
				t.Errorf("the installer's child %d outlived Run", pid)
			}
		})
	}
	// THE CONTROL: without the option the child outlives the installer, which is what the option is
	// for (and what every other capture still does).
	pid, _ := leftChild(t, "sleep 30", 3, false)
	if !processAlive(pid) {
		t.Errorf("without EndProcessGroup the child %d is gone too, so this test proves nothing", pid)
	}
}
