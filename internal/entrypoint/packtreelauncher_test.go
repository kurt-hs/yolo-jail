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

// packTreeLauncherWith is packTreeLauncher with the host's delivery for board baked in, as a boot
// with YOLO_FORK_BUILDS set bakes it, into the same home.
func packTreeLauncherWith(t *testing.T, home string, d ForkDelivery) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "fx"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"fx","contributes":[{"kind":"program","bin":"board","via":"pack",` +
		`"from":"tools/board-cli","build":"make","run":"dist/board.js"}]}`
	if err := os.WriteFile(filepath.Join(root, "fx", "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	captures := filepath.Join(home, "captures")
	if err := os.MkdirAll(captures, 0o755); err != nil {
		t.Fatal(err)
	}
	e := NewEnv(map[string]string{
		"JAIL_HOME": home, "YOLO_WORKSPACE": filepath.Join(home, "ws"),
		"YOLO_PACK_ROOT": root, CapturesDirEnv: captures,
		ForkBuildsEnv: ForkBuildsWire(map[string]ForkDelivery{"board": d}),
	})
	e.Stderr = &bytes.Buffer{}
	if err := GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(e.LaunchDir(), "board")
}

// fakeMaterializingYolo is a `yolo` whose capture-materialize lays out the pack-tree program the
// key names, as the store's entry would: key "A" a tree with a file only A's build made, key "B" a
// tree without it, and key "bad" nothing, failing. Every call is logged.
func fakeMaterializingYolo(t *testing.T, home string) (dir, log string) {
	t.Helper()
	dir, log = t.TempDir(), filepath.Join(home, "yolo.log")
	body := `#!/bin/bash
printf 'YOLO %s\n' "$*" >> '` + log + `'
[ "$2" = capture-materialize ] || exit 0
for a in "$@"; do [ "$a" = --verify-installed ] && exit 0; done
key=""
for a in "$@"; do case "$a" in --key=*) key="${a#--key=}" ;; esac; done
[ "$key" = bad ] && exit 1
d="$HOME/.local/lib/yolo-programs/board"
mkdir -p "$d/dist" "$HOME/.local/bin"
printf '#!/bin/sh\necho board-%s\n' "$key" > "$d/dist/board.js"
chmod +x "$d/dist/board.js"
[ "$key" = A ] && echo stale > "$d/only-in-A.txt"
ln -sfn ../lib/yolo-programs/board/dist/board.js "$HOME/.local/bin/board"
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "yolo"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, log
}

// BG-D12: MATERIALIZING A NEW KEY REPLACES THE PROGRAM'S DIRECTORY WHOLE. Key B after key A, in one
// home, leaves no file of A's tree behind; the receipt's declared source is the pack's build source,
// never a fork's empty address. Red if the launcher stops clearing the program's directory before a
// different key, or the generator stops synthesizing it into the launcher's produces.
func TestAPackTreeLauncherReplacesAnOlderBuildWhole(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	home := t.TempDir()
	yolo, log := fakeMaterializingYolo(t, home)
	run := func(launcher string, env ...string) (string, int) {
		cmd := exec.Command(launcher)
		cmd.Dir = home
		cmd.Env = append([]string{"HOME=" + home, "PATH=" + yolo + ":" + os.Getenv("PATH")}, env...)
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatalf("the launcher could not be run: %v\n%s", err, out)
		}
		return string(out), 0
	}
	stale := filepath.Join(home, ".local", "lib", "yolo-programs", "board", "only-in-A.txt")

	out, rc := run(packTreeLauncherWith(t, home, ForkDelivery{Key: "A"}))
	if rc != 0 || !strings.Contains(out, "board-A") {
		t.Fatalf("key A: rc=%d %q, want board-A", rc, out)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("the fixture's key A left no file of its own: %v", err)
	}
	out, rc = run(packTreeLauncherWith(t, home, ForkDelivery{Key: "B"}))
	if rc != 0 || !strings.Contains(out, "board-B") {
		t.Fatalf("key B: rc=%d %q, want board-B", rc, out)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file of key A's build survived key B's materialize")
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "--declared=pack:fx/tools/board-cli") {
		t.Errorf("the materialize did not record the pack's build source:\n%s", data)
	}

	// A materialize that fails names the pack's build, never "fork 's build".
	out, rc = run(packTreeLauncherWith(t, home, ForkDelivery{Key: "bad"}))
	if rc == 0 || !strings.Contains(out, "pack fx's build of board from tools/board-cli (bad) could not be put in place") ||
		strings.Contains(out, "fork") {
		t.Errorf("rc=%d, a failed materialize said %q, want the pack's build named and no fork", rc, out)
	}
}
