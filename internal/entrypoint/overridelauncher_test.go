package entrypoint

// overridelauncher_test.go pins the jail's half of a DEV OVERRIDE (docs/design/pack-buildable-programs.md
// §5): an overridden bin's launcher runs the checkout's file as found, refuses (never falls through)
// when that file is missing or the host refused the override, and checks without running in
// install-only mode, which is how the readiness act refuses the launch.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// overrideFixture generates launchers for a pack declaring board two ways (a pack-tree program) with
// the given override decision for board, and returns the home and board's launcher.
func overrideFixture(t *testing.T, ov ProgramOverride) (home, launcher string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "fx"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"fx","contributes":[{"kind":"program","bin":"board","via":"pack",` +
		`"from":"tools/board-cli","build":"make","run":"dist/board.js"},` +
		`{"kind":"program","bin":"other","via":"pack","from":"tools/other","run":"other"}]}`
	if err := os.WriteFile(filepath.Join(root, "fx", "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	home = t.TempDir()
	e := NewEnv(map[string]string{
		"JAIL_HOME": home, "YOLO_WORKSPACE": filepath.Join(home, "ws"),
		"YOLO_PACK_ROOT": root, CapturesDirEnv: t.TempDir(),
		ProgramOverridesEnv: ProgramOverridesWire(map[string]ProgramOverride{"board": ov}),
	})
	e.Stderr = &bytes.Buffer{}
	if err := GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(e.LaunchDir(), "board")
}

func runOverrideLauncher(t *testing.T, home, launcher string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(launcher, args...)
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

// TestTheOverrideLauncherExecsTheCheckoutsFile is the plan's first unit test: the launcher's text
// names exactly <dir>/<run> and execs it with the invocation's arguments.
func TestTheOverrideLauncherExecsTheCheckoutsFile(t *testing.T) {
	text := overrideLauncher("board", ProgramOverride{Dir: "/ctx/program-overrides/board",
		Run: "dist/board.js", Host: "/home/me/code/board"}, nil)
	for _, want := range []string{
		"PROG=/ctx/program-overrides/board/dist/board.js\n",
		`exec "$PROG" ${YOLO_ARGV[@]+"${YOLO_ARGV[@]}"}`,
		`_yolo_launch_argv "$@"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the override launcher lacks %q:\n%s", want, text)
		}
	}
}

// TestGenerationWritesTheOverrideLauncherInPlaceOfTheVias is the call-site test: red if
// GenerateAgentLaunchers stops consulting ProgramOverridesEnv, because board would then get the
// source launcher. A bin with no override keeps its via's launcher.
func TestGenerationWritesTheOverrideLauncherInPlaceOfTheVias(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "dist", "board.js")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho dev-1 \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home, launcher := overrideFixture(t, ProgramOverride{Dir: dir, Run: "dist/board.js", Host: dir, Build: "make"})
	body, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "DEV OVERRIDE") || strings.Contains(string(body), "Source launcher") {
		t.Fatalf("board did not get the override launcher:\n%s", body)
	}
	other, err := os.ReadFile(filepath.Join(filepath.Dir(launcher), "other"))
	if err != nil || !strings.Contains(string(other), "Source launcher") {
		t.Errorf("a program with no override lost its own launcher (err=%v)", err)
	}

	out, rc := runOverrideLauncher(t, home, launcher, nil, "a", "b c")
	if rc != 0 || strings.TrimSpace(out) != "dev-1 a b c" {
		t.Errorf("rc=%d out=%q, want the checkout's file run with the arguments", rc, out)
	}
	// An edit in the checkout is live: nothing was copied.
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho dev-2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, _ := runOverrideLauncher(t, home, launcher, nil); strings.TrimSpace(out) != "dev-2" {
		t.Errorf("an edit in the checkout did not show at the next invocation: %q", out)
	}
	// Install-only: present, so it stops quietly and runs nothing.
	if out, rc := runOverrideLauncher(t, home, launcher, []string{InstallOnlyEnv + "=1"}); rc != 0 || out != "" {
		t.Errorf("install-only rc=%d out=%q, want a silent success that runs nothing", rc, out)
	}
}

// TestAMissingOverrideFileRefusesInEveryMode: red if the launcher falls through to the pack's own
// copy, or lets the readiness act pass, when the checkout's file is missing.
func TestAMissingOverrideFileRefusesInEveryMode(t *testing.T) {
	dir := t.TempDir()
	home, launcher := overrideFixture(t, ProgramOverride{Dir: dir, Run: "dist/board.js", Host: "/home/me/code/board",
		Build: "npm ci && npm run bundle"})
	for _, env := range [][]string{nil, {InstallOnlyEnv + "=1"}, {FreshRefreshEnv + "=1"}} {
		out, rc := runOverrideLauncher(t, home, launcher, env)
		if rc == 0 {
			t.Errorf("env %v: a missing override file was not refused:\n%s", env, out)
		}
		for _, want := range []string{"refused", "/home/me/code/board/dist/board.js", "programs.overrides.board",
			"npm ci && npm run bundle", "Cannot run"} {
			if !strings.Contains(out, want) {
				t.Errorf("env %v: the refusal lacks %q:\n%s", env, want, out)
			}
		}
	}
	// Present but not executable: refused, naming chmod; nothing in the checkout is changed.
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "board.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, rc := runOverrideLauncher(t, home, launcher, nil)
	if rc == 0 || !strings.Contains(out, "not executable") || !strings.Contains(out, "chmod +x") {
		t.Errorf("rc=%d, want a refusal naming chmod:\n%s", rc, out)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "dist", "board.js")); fi.Mode().Perm()&0o111 != 0 {
		t.Errorf("the launcher wrote into the override's directory")
	}
}

// TestAHostRefusedOverrideNeverRunsThePacksCopy: the host's refusal reaches the user in every mode.
func TestAHostRefusedOverrideNeverRunsThePacksCopy(t *testing.T) {
	home, launcher := overrideFixture(t, ProgramOverride{Host: "/nope",
		Refused: "/nope does not exist or cannot be read (programs.overrides.board in ~/.config/yolo-jail/config.jsonc)"})
	for _, env := range [][]string{nil, {InstallOnlyEnv + "=1"}} {
		out, rc := runOverrideLauncher(t, home, launcher, env)
		if rc != 127 || !strings.Contains(out, "dev override refused: /nope does not exist") ||
			!strings.Contains(out, "Cannot run board") {
			t.Errorf("env %v: rc=%d, want the host's refusal:\n%s", env, rc, out)
		}
	}
}

// TestAnUnreadableOverrideWireAppliesNoneAndSaysSo: a value this build cannot read is named, and
// every program keeps its own launcher.
func TestAnUnreadableOverrideWireAppliesNoneAndSaysSo(t *testing.T) {
	var buf bytes.Buffer
	e := NewEnv(map[string]string{ProgramOverridesEnv: "{not json"})
	e.Stderr = &buf
	if got := programOverrides(e); got != nil {
		t.Errorf("an unreadable wire applied %v", got)
	}
	if !strings.Contains(buf.String(), ProgramOverridesEnv) {
		t.Errorf("the unreadable wire was not named: %q", buf.String())
	}
}
