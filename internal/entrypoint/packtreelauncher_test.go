package entrypoint

// packtreelauncher_test.go pins the jail's half of the pack-tree hazard (docs/design/pack-buildable-programs.md,
// `via: "pack"`): GenerateAgentLaunchers writes a pack-tree program the source launcher, as for a
// fork, but with no key from the host its refusal names the pack's tree — never a fork pack, a pin
// or the fork route the launch did not take — and its update-mode line says what moves it.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func packTreeLauncher(t *testing.T) (home, launcher string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "fx"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"fx","contributes":[{"kind":"program","bin":"board","via":"pack",` +
		`"from":"tools/board-cli","build":"make","run":"dist/board.js"}]}`
	if err := os.WriteFile(filepath.Join(root, "fx", "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	home = t.TempDir()
	e := NewEnv(map[string]string{
		"JAIL_HOME": home, "YOLO_WORKSPACE": filepath.Join(home, "ws"),
		"YOLO_PACK_ROOT": root, CapturesDirEnv: t.TempDir(),
	})
	e.Stderr = &bytes.Buffer{}
	if err := GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(e.LaunchDir(), "board")
}

// TestAPackTreeLauncherWithNoKeyNamesThePacksTree: red if the generator stops writing a source
// launcher for a pack-tree program, or hands it a fork's reason.
func TestAPackTreeLauncherWithNoKeyNamesThePacksTree(t *testing.T) {
	home, launcher := packTreeLauncher(t)
	body, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatalf("no launcher for the pack-tree program: %v", err)
	}
	if !strings.Contains(string(body), "Source launcher") {
		t.Fatalf("the pack-tree program did not get the source launcher:\n%s", body)
	}
	run := func(env ...string) (string, int) {
		cmd := exec.Command(launcher)
		cmd.Dir = home
		cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}, env...)
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatalf("the launcher could not be run: %v\n%s", err, out)
		}
		return string(out), 0
	}
	out, rc := run()
	if rc == 0 || !strings.Contains(out, "built no board from pack fx's own tree (tools/board-cli)") {
		t.Errorf("rc=%d, want a refusal naming the pack's tree:\n%s", rc, out)
	}
	if strings.Contains(out, "fork") || strings.Contains(out, "pin") {
		t.Errorf("a pack-tree program's refusal reads as a fork's:\n%s", out)
	}
	out, rc = run("YOLO_PACK_UPDATE=1")
	if rc != 0 || !strings.Contains(out, "board: built from its pack's own tree (tools/board-cli)") ||
		strings.Contains(out, "fork pack") {
		t.Errorf("rc=%d, update mode = %q, want the pack-tree note and no fork pack", rc, out)
	}
}
