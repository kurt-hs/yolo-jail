package check

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// The broker rows of `yolo check` since HD-R1 (docs/design/host-daemon-ownership.md §5.2):
// each jail runs its own Claude broker, so the rows are one per running copy, found by its
// socket. Every test here is driven through checkLoopholes, not reportBrokerDaemon: the rows are
// reached through the loophole walk's `lp.Name == brokerLoopholeName` call site, and deleting
// that call site must turn these red too.

// brokerRows runs the loophole section with the broker loophole declared and a passing
// self-check, which is what reaches the daemon rows.
func brokerRows(t *testing.T) (*reporter, string) {
	t.Helper()
	moduleRoot := isolatedModuleDir(t)
	selfCheckModule(t, moduleRoot, brokerLoopholeName, []string{"/bin/sh", "-c", "echo OK"})
	r, out := runCheckLoopholes(t, t.TempDir())
	if !strings.Contains(out, "loophole "+brokerLoopholeName+": self-check ok") {
		t.Fatalf("the broker's loophole row never reached the daemon rows:\n%s", out)
	}
	return r, out
}

// frontSocketsIn points paths.FrontSocketDir at a short private dir for one test.
func frontSocketsIn(t *testing.T) {
	t.Helper()
	saved := paths.FrontSocketDir
	paths.FrontSocketDir = shortBrokerSocketDir(t)
	t.Cleanup(func() { paths.FrontSocketDir = saved })
}

// NO JAIL, NO DAEMON, AND THAT PASSES. A PID file an older yolo's broker left at the
// machine-wide path, naming a process that has exited, is not a running copy either: it is
// what the Mac nightly's TestYoloCheckValidConfig once failed on (run 36566584474), and it
// still must not fail `yolo check`, nor be deleted by it.
func TestCheckPassesTheBrokerWhenNoJailRunsOne(t *testing.T) {
	frontSocketsIn(t)
	pid := exitedPID(t)
	pidFile := paths.HostSingletonPIDFile(brokerLoopholeName)
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(pidFile) })

	r, out := brokerRows(t)
	if r.failed != 0 || r.warned != 0 {
		t.Errorf("no jail running a broker is a pass (failed=%d, warned=%d):\n%s", r.failed, r.warned, out)
	}
	if !strings.Contains(out, "no daemon running (each jail starts its own") {
		t.Errorf("the row does not say why nothing runs:\n%s", out)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("`yolo check` removed the PID file (%v); it only reports", err)
	}
}

// A JAIL'S OWN COPY that accepts is a pass naming that jail; one whose socket is left behind
// with nothing accepting is a WARN that names the relaunch — not a FAIL, since a jail that ended
// without its teardown leaves exactly that.
func TestCheckReportsEachJailsBroker(t *testing.T) {
	frontSocketsIn(t)
	live := paths.FrontSocketFile("aaaaaaaa", brokerLoopholeName)
	ln, err := net.Listen("unix", live)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	stale := paths.FrontSocketFile("bbbbbbbb", brokerLoopholeName)
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	r, out := brokerRows(t)
	if r.failed != 0 {
		t.Errorf("a jail's broker graded FAIL (failed=%d):\n%s", r.failed, out)
	}
	for _, want := range []string{
		"jail aaaaaaaa's daemon live", "socket accepting",
		"jail bbbbbbbb's daemon not accepting", "leave it and launch it again",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the broker rows lack %q:\n%s", want, out)
		}
	}
}

// AN OLDER YOLO'S HOST-WIDE BROKER, still running, is a WARN naming both steps: relaunch the
// jails it serves with this yolo, then stop it.
func TestCheckWarnsOfAnOlderYolosHostWideBroker(t *testing.T) {
	frontSocketsIn(t)
	for _, p := range []string{paths.HostSingletonLock(brokerLoopholeName), paths.HostSingletonPIDFile(brokerLoopholeName)} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(p) })
	}

	r, out := brokerRows(t)
	if r.warned == 0 {
		t.Errorf("a host-wide broker an older yolo left is not a warning:\n%s", out)
	}
	for _, want := range []string{"a host-wide daemon an older yolo left is running",
		"Relaunch those jails with this yolo", "yolo host-daemon stop --host-wide " + brokerLoopholeName} {
		if !strings.Contains(out, want) {
			t.Errorf("the row lacks %q:\n%s", want, out)
		}
	}
}

// exitedPID returns the pid of a process that has exited and been reaped.
func exitedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(pid, 0); err == nil {
		t.Skipf("pid %d was recycled before the check could read it", pid)
	}
	return pid
}
