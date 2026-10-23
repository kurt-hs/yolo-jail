//go:build linux

package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPty returns a pty pair, skipping when the machine cannot allocate one. A copy of
// internal/provision's: the repo vendors no pty library.
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	var unlock int32
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, m.Fd(), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		m.Close()
		t.Skipf("unlockpt: %v", e)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		m.Close()
		t.Skipf("ptsname: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Skipf("open slave: %v", err)
	}
	t.Cleanup(func() { s.Close(); m.Close() })
	return m, s
}

// runMainOnPty runs Main with all three standard streams on a pty, returning the exit status
// and everything the terminal showed.
func runMainOnPty(t *testing.T, argv ...string) (int, string) {
	t.Helper()
	master, slave := openPty(t)
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { io.Copy(&buf, master); close(done) }()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = slave, slave, slave
	rc := Main(append([]string{"yolo"}, argv...))
	os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
	slave.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pty did not drain")
	}
	return rc, buf.String()
}

func TestHostRunRefusesWithNoWorkspaceAtATerminal(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("HOME", evalTempDir(t))
	start := evalTempDir(t)
	t.Chdir(start)
	rc, out := runMainOnPty(t, "host-run", "--", "echo", "hi")
	if rc != 64 || !strings.Contains(out, "-C <workspace>") || !strings.Contains(out, "$YOLO_HOST_DIR") ||
		!strings.Contains(out, "without `yolo host-run`") {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	if ents, _ := os.ReadDir(start); len(ents) != 0 {
		t.Fatalf("created %v", ents)
	}
	rc, out = runMainOnPty(t, "host-run", "-C", "/workspace/definitely-not-here-hostrun", "--", "echo", "hi")
	if rc != 64 || !strings.Contains(out, "no directory") || !strings.Contains(out, "jail's spelling") {
		t.Fatalf("rc %d\n%s", rc, out)
	}
}

// The whole host form through Main: a workspace found from -C, a fake program on an explicit
// PATH, the run delivered. It fails if Main stops calling hostrun.RunHost.
func TestHostRunRunsAtATerminal(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	home := evalTempDir(t)
	t.Setenv("HOME", home)
	ws := evalTempDir(t)
	os.WriteFile(filepath.Join(ws, "yolo-jail.jsonc"), []byte("{}"), 0o644)
	os.Mkdir(filepath.Join(ws, ".yolo"), 0o755)
	bin := filepath.Join(evalTempDir(t), "bin")
	os.Mkdir(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "say"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o755)
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	sleepPath, _ = filepath.EvalSymlinks(sleepPath)
	t.Setenv("PATH", bin+":"+filepath.Dir(sleepPath))
	t.Chdir(home)
	rc, out := runMainOnPty(t, "host-run", "-C", ws, "--name", "t1", "--", "say", "hi")
	if rc != 0 || !strings.Contains(out, "Nothing to paste back.") || !strings.Contains(out, "hi") {
		t.Fatalf("rc %d\n%s", rc, out)
	}
	ents, _ := os.ReadDir(filepath.Join(ws, ".yolo", "host-run"))
	if len(ents) != 1 {
		t.Fatalf("mailbox %v", ents)
	}
	rc, show, errOut := runMainCaptured(t, "host-run", "show", "-C", ws, "--name", "t1")
	if rc != 0 || !strings.Contains(show, "exit:  0") || !strings.Contains(show, "hi") {
		t.Fatalf("show: rc %d %s %s", rc, show, errOut)
	}
}
