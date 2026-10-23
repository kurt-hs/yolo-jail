package cli

// hostfloorpacktree_test.go drives a PACK-TREE PROGRAM (docs/design/pack-buildable-programs.md, `via:
// "pack"`) and a DEV OVERRIDE through `yolo host` on Linux (the plan's Task 9), from the call sites a
// user reaches: `yolo host -- <bin>` and the readiness act of `yolo host -- <other>`. The production
// floor wiring runs — the pack staged as a launch stages it, the digest, the build act (buildFork),
// the store's admission, the relocating materialize — with only the build jail (captureRunPipeline)
// and the floor's platform (Linux) standing in.

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// hostPackTreeHome is a temp HOME whose user config selects only pack fx, a `file://` pack declaring
// the pack-tree program x from tools/x, run x, built by build ("" for none), with extra raw config
// keys. tools/x/x is a script printing x-<VERSION>, for the copy with no build line. It returns fx's
// root.
func hostPackTreeHome(t *testing.T, build, extra string) string {
	t.Helper()
	home := floortest.ResolvedTemp(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("YOLO_VERSION", "")
	t.Setenv(paths.AllowMissingProgramsEnv, "")
	t.Chdir(floortest.ResolvedTemp(t))
	previousProbe := probeForkBuildContainer
	probeForkBuildContainer = func(string, string, time.Duration) (bool, bool) { return false, true }
	t.Cleanup(func() { probeForkBuildContainer = previousProbe })
	fx := filepath.Join(floortest.ResolvedTemp(t), "fx")
	writeFile(t, filepath.Join(fx, "tools", "x", "VERSION"), "1\n")
	writePackTreeScript(t, fx, "1")
	buildKey := ""
	if build != "" {
		buildKey = `"build":"` + strings.ReplaceAll(strings.ReplaceAll(build, `\`, `\\`), `"`, `\"`) + `",`
	}
	writeFile(t, filepath.Join(fx, "pack.json"), `{"name":"fx","contributes":[{"kind":"program","bin":"x",`+
		`"via":"pack","from":"tools/x",`+buildKey+`"run":"x"}]}`)
	writeFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), `{"host_management":"own",`+
		`"host_apply_on_launch":false,`+extra+`"packs":[{"source":"file://`+fx+`","name":"fx"}]}`)
	return fx
}

// writePackTreeScript writes fx's tools/x/x, a script printing x-<version>.
func writePackTreeScript(t *testing.T, fx, version string) {
	t.Helper()
	x := filepath.Join(fx, "tools", "x", "x")
	writeFile(t, x, "#!/bin/sh\necho x-"+version+"\n")
	if err := os.Chmod(x, 0o755); err != nil {
		t.Fatal(err)
	}
}

// runFloorLauncher runs the floor's launcher for bin with no environment and returns its output.
func runFloorLauncher(t *testing.T, bin string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(paths.HostFloorDir(), "bin", bin))
	cmd.Env = []string{}
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the floor's %s: %q %v", bin, got, err)
	}
	return string(got)
}

// THE CALL SITE, WITH NO BUILD LINE: `yolo host -- x` copies the pack's tree into the store (no jail),
// materializes it into the floor and execs the floor's copy; an edit to the pack's tree is a new
// digest, so the next launch reinstalls it once; a launch with nothing changed installs nothing. Red
// if the floor's PackTree wiring is removed (x has no floor entry and the launch refuses with 127).
func TestYoloHostDeliversAPackTreeProgramAndFollowsItsTree(t *testing.T) {
	fx := hostPackTreeHome(t, "", "")
	withForkFloor(t)
	jail := 0
	withFakeCaptureJail(t, func(run.Options) int { jail++; return 1 })
	got := captureHostExec(t)
	launcher := filepath.Join(paths.HostFloorDir(), "bin", "x")

	var errw bytes.Buffer
	if rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil); rc != 0 || got.target != launcher {
		t.Fatalf("rc=%d target=%s, want the floor's copy %s\n%s", rc, got.target, launcher, errw.String())
	}
	if out := runFloorLauncher(t, "x"); out != "x-1\n" || jail != 0 {
		t.Fatalf("the floor's x ran %q with %d build jails, want x-1 and none (no build line)", out, jail)
	}
	rec := floorRecord(t, "x")
	if rec.Via != "pack" || !strings.HasPrefix(rec.Version, "tree ") || len(rec.Revision) != 64 {
		t.Errorf("record = %+v, want a pack-tree record keyed by the tree digest", rec)
	}
	first := rec.Revision

	writePackTreeScript(t, fx, "2")
	errw.Reset()
	if rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil); rc != 0 {
		t.Fatalf("after an edit: rc=%d\n%s", rc, errw.String())
	}
	if out := runFloorLauncher(t, "x"); out != "x-2\n" {
		t.Fatalf("after an edit the floor's x ran %q, want x-2", out)
	}
	if rec := floorRecord(t, "x"); rec.Revision == first || !strings.Contains(errw.String(), "changed") {
		t.Errorf("the edit was not a new digest and a reinstall: %s\n%s", rec.Revision, errw.String())
	}

	errw.Reset()
	if rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil); rc != 0 || strings.Contains(errw.String(), "installing x") {
		t.Errorf("a launch with nothing changed: rc=%d, and it installed again:\n%s", rc, errw.String())
	}
}

// THE CALL SITE, WITH A BUILD LINE: the floor's install runs the sealed build act once (the stand-in
// jail), the next launch is a store hit with no jail, and the floor runs what the build left.
func TestYoloHostBuildsAPackTreeProgramOnceInTheSealedAct(t *testing.T) {
	hostPackTreeHome(t, packBuildFixtureBuild, "")
	withForkFloor(t)
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	captureHostExec(t)
	var errw bytes.Buffer
	if rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil); rc != 0 || runs != 1 {
		t.Fatalf("rc=%d with %d build jails, want 0 and one\n%s", rc, runs, errw.String())
	}
	if out := runFloorLauncher(t, "x"); out != "x-1\n" {
		t.Fatalf("the floor's x ran %q, want the build's x-1", out)
	}
	errw.Reset()
	if rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil); rc != 0 || runs != 1 {
		t.Errorf("the second launch: rc=%d with %d build jails, want no new build\n%s", rc, runs, errw.String())
	}
}

// THE HOST FLOOR'S REFUSAL: a pack-tree program whose build fails stops `yolo host -- <other>` through
// the readiness act, naming the program, the build's log, the retry `yolo pack build fx --at host` and
// the bypass; with the bypass the launch starts without it. Red if the floor still has no pack-tree
// arm (the refusal would say the floor does not build it, with no log and no retry).
func TestAFailedPackTreeBuildRefusesAHostLaunchNamingItsLogAndRetry(t *testing.T) {
	hostPackTreeHome(t, packBuildFixtureBuild, "")
	withForkFloor(t)
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 3))
	got := captureHostExec(t)
	stubBins(t, "other")
	var errw bytes.Buffer
	rc := hostExec(nil, []string{"other"}, io.Discard, &errw, nil)
	if rc == 0 || got.execed {
		t.Fatalf("rc=%d execed=%v, want a refusal\n%s", rc, got.execed, errw.String())
	}
	for _, want := range []string{"program x (pack fx)", "building x from pack fx's tools/x", "its log: ",
		"yolo pack build fx --at host", paths.AllowMissingProgramsEnv + "=1 yolo host -- other"} {
		if !strings.Contains(errw.String(), want) {
			t.Errorf("the refusal lacks %q:\n%s", want, errw.String())
		}
	}
	t.Setenv(paths.AllowMissingProgramsEnv, "1")
	errw.Reset()
	if rc := hostExec(nil, []string{"other"}, io.Discard, &errw, nil); rc != 0 || !got.execed {
		t.Errorf("with the bypass: rc=%d execed=%v, want the launch\n%s", rc, got.execed, errw.String())
	}
}

// THE OVERRIDE ARM AT THE CALL SITE: with programs.overrides.x naming a checkout, `yolo host -- x`
// prints the which-bytes line, never builds the pack's copy, and execs the floor's launcher, which runs
// the checkout's file live. `yolo host env` prints the same line. A checkout whose file is missing
// refuses the launch with 127, naming the file and the key, and never runs the pack's copy.
func TestYoloHostRunsAProgramFromItsDevOverride(t *testing.T) {
	dev := floortest.ResolvedTemp(t)
	hostPackTreeHome(t, packBuildFixtureBuild, `"programs":{"overrides":{"x":{"path":"`+dev+`"}}},`)
	withForkFloor(t)
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	got := captureHostExec(t)
	launcher := filepath.Join(paths.HostFloorDir(), "bin", "x")

	var errw bytes.Buffer
	rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil)
	if rc != 127 || got.execed || !strings.Contains(errw.String(), filepath.Join(dev, "x")) ||
		!strings.Contains(errw.String(), "programs.overrides.x") {
		t.Fatalf("a checkout with no x: rc=%d execed=%v, want 127 naming the file and the key\n%s", rc, got.execed, errw.String())
	}
	// The readiness act of another launch refuses it too, and never offers `host_floor`, which the
	// override wins over.
	stubBins(t, "other")
	errw.Reset()
	if rc := hostExec(nil, []string{"other"}, io.Discard, &errw, nil); rc == 0 || got.execed ||
		!strings.Contains(errw.String(), "programs.overrides.x") || strings.Contains(errw.String(), "host_floor") {
		t.Fatalf("yolo host -- other with a broken override: rc=%d execed=%v, want a refusal naming the key and "+
			"no `host_floor`\n%s", rc, got.execed, errw.String())
	}

	writeFile(t, filepath.Join(dev, "x"), "#!/bin/sh\necho dev-1\n")
	if err := os.Chmod(filepath.Join(dev, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	errw.Reset()
	line := "x: dev override — runs " + filepath.Join(dev, "x") + " (programs.overrides in ~/.config/yolo-jail/config.jsonc)"
	if rc := hostExec(nil, []string{"x"}, io.Discard, &errw, nil); rc != 0 || got.target != launcher ||
		!strings.Contains(errw.String(), line) {
		t.Fatalf("rc=%d target=%s, want the floor's launcher and the line %q\n%s", rc, got.target, line, errw.String())
	}
	if runs != 0 {
		t.Errorf("the pack's build ran %d times beside its override", runs)
	}
	if out := runFloorLauncher(t, "x"); out != "dev-1\n" {
		t.Fatalf("the floor's x ran %q, want the checkout's dev-1", out)
	}
	writeFile(t, filepath.Join(dev, "x"), "#!/bin/sh\necho dev-2\n")
	if out := runFloorLauncher(t, "x"); out != "dev-2\n" {
		t.Errorf("after an edit the floor's x ran %q, want dev-2 with no launch", out)
	}

	var out, envErr bytes.Buffer
	if rc := hostMain([]string{"env"}, &out, &envErr, false, nil); rc != 0 || !strings.Contains(envErr.String(), line) {
		t.Errorf("yolo host env: rc=%d, want the line\n%s", rc, envErr.String())
	}
}
