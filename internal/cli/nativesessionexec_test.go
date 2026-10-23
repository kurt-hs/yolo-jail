package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/nativehome"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// This subprocess goes through the real internal dispatcher and syscall.Exec. Its shell
// and the shell's child inherit stock relocation and the existing 16-hex identity.
func TestNativeSessionExecHelper(t *testing.T) {
	dir := os.Getenv("YOLO_TEST_NATIVE_EXEC_DIR")
	if dir == "" {
		return
	}
	os.Exit(runInternal([]string{nativehome.ExecVerb, dir, "launch", "/bin/sh", "-c",
		`test "$YOLO_SESSION_ID" = 0123456789abcdef && test "$PI_CODING_AGENT_DIR" = "$1/agent" && /bin/sh -c 'printf "%s\n%s" "$YOLO_SESSION_ID" "$PI_CODING_AGENT_DIR"' > "$1/child-env" && touch "$1/ready" && read line < "$1/release"`, "native-child", dir}))
}

func TestNativeSessionExecExportsToShellAndChildrenAndHoldsLease(t *testing.T) {
	dir, err := nativehome.Prepare(t.TempDir(), t.TempDir(), "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := nativehome.Complete(dir, map[string]string{"YOLO_SESSION_ID": "0123456789abcdef", "PI_CODING_AGENT_DIR": filepath.Join(dir, "agent")}); err != nil {
		t.Fatal(err)
	}
	// An inherited input ID cannot substitute for bootstrap's identity.
	t.Setenv("YOLO_TEST_NATIVE_EXEC_DIR", dir)
	t.Setenv("YOLO_SESSION_ID", "fedcba9876543210")
	if err := syscall.Mkfifo(filepath.Join(dir, "release"), 0600); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestNativeSessionExecHelper$")
	// TMPDIR: the child's TestMain staged tree is removed with this test's dir (reexectmpdir_test.go).
	c.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c.Process != nil {
			_ = c.Process.Kill()
		}
	})
	waitNativeFile(t, filepath.Join(dir, "ready"))
	if state, release, _ := packload.ProbeLease(dir); state != packload.LeaseHeld {
		release()
		t.Fatalf("exec lease = %s", state)
	} else {
		release()
	}
	data, err := os.ReadFile(filepath.Join(dir, "child-env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789abcdef\n"+filepath.Join(dir, "agent") {
		t.Fatalf("child env = %q", data)
	}
	f, err := os.OpenFile(filepath.Join(dir, "release"), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("done\n")
	_ = f.Close()
	if err := c.Wait(); err != nil {
		t.Fatal(err)
	}
	release, gone := nativehome.ClaimGone(dir)
	defer release()
	if !gone {
		t.Fatal("ended exec still held lease")
	}
}

func waitNativeFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native command did not become ready")
}
