package execx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/testutil"
)

// selfExecHelperEnv makes TestSelfExecHelperProcess print what SelfExecArgv chose and exit.
const selfExecHelperEnv = "YOLO_TEST_SELFEXEC_HELPER"

// TestSelfExecHelperProcess is not a test on its own: TestSelfExecIgnoresTheInvokedName runs
// this test binary under another name with selfExecHelperEnv set.
func TestSelfExecHelperProcess(t *testing.T) {
	if os.Getenv(selfExecHelperEnv) != "1" {
		t.Skip("helper process only")
	}
	fmt.Println(SelfExecArgv([]string{"yolo", "internal", "daemon", "x"})[0])
	os.Exit(0)
}

// A launcher reached through a symlink or copy with another name (`just deploy yolo-stable`,
// `just deploy-dev yolo-dev`) self-execs its own binary, never the bare `yolo` on PATH and
// never the name it was invoked as when that name is only a link.
func TestSelfExecIgnoresTheInvokedName(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "yolo-dev")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(dir, "yolo-stable")
	copyFile(t, real, copied)

	for _, tc := range []struct{ invoked, want string }{{link, real}, {copied, copied}} {
		cmd := exec.Command(tc.invoked, "-test.run=^TestSelfExecHelperProcess$")
		cmd.Env = append(os.Environ(), selfExecHelperEnv+"=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: %v", tc.invoked, err)
		}
		got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
		want, _ := filepath.EvalSymlinks(tc.want)
		if got != want {
			t.Errorf("invoked as %s: self-exec target %q, want %q", filepath.Base(tc.invoked), got, want)
		}
	}
}

// The real yolo, reached through a symlink and through a renamed copy, answers --version and
// runs the hidden daemon group (`internal daemon claude-oauth-broker --init-ca`, the command
// the deploy recipes prime the broker with by the launcher's own path). Dispatch is on
// args[1:], so the program's name must not matter.
func TestYoloRunsUnderAnotherName(t *testing.T) {
	built := filepath.Join(testutil.YoloBinaryDir(t), "yolo")
	dir := t.TempDir()
	link := filepath.Join(dir, "yolo-dev")
	if err := os.Symlink(built, link); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(dir, "yolo-stable")
	copyFile(t, built, copied)

	for _, launcher := range []string{link, copied} {
		name := filepath.Base(launcher)
		home := t.TempDir()
		env := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "YOLO_NO_BANNER=1"}

		out, err := runWith(env, launcher, "--version")
		if err != nil || !strings.HasPrefix(out, "yolo-jail ") {
			t.Errorf("%s --version: %v\n%s", name, err, out)
		}
		out, err = runWith(env, launcher, "--version", "--full")
		if err != nil {
			t.Fatalf("%s --version --full: %v\n%s", name, err, out)
		}
		if want := "binary sha256: " + sha256File(t, launcher); !strings.Contains(out, want) {
			t.Errorf("%s --version --full hashed another file; want %q in\n%s", name, want, out)
		}
		out, err = runWith(env, launcher, "internal", "daemon", "claude-oauth-broker", "--init-ca")
		if err != nil {
			t.Fatalf("%s internal daemon claude-oauth-broker --init-ca: %v\n%s", name, err, out)
		}
		ca := filepath.Join(home, ".local/share/yolo-jail/state/claude-oauth-broker/ca.crt")
		if _, err := os.Stat(ca); err != nil {
			t.Errorf("%s: the daemon group did not prime the broker: %v\n%s", name, err, out)
		}
	}

	// The dev wrapper execs a bundle's launcher with argv[0] set to the name it was invoked as
	// (scripts/matched-bundle.py), so `yolo-dev host-run -- yolo-dev host-run ...` is caught as
	// nesting even though the binary on disk is named yolo.
	cmd := exec.Command(built, "host-run", "--", "yolo-dev", "host-run", "--", "true")
	cmd.Args[0] = filepath.Join(dir, "yolo-dev")
	// The in-jail form parses the line and prints it, running and creating nothing, with no
	// terminal check ahead of the parse.
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin", "YOLO_NO_BANNER=1", "YOLO_VERSION=test"}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "is itself `yolo host-run`") {
		t.Errorf("host-run under argv[0] yolo-dev did not refuse a nested yolo-dev host-run: %v\n%s", err, out)
	}
}

func runWith(env []string, argv0 string, args ...string) (string, error) {
	cmd := exec.Command(argv0, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
