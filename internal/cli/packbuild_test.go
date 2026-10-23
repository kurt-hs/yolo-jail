package cli

// packbuild_test.go drives `yolo pack build` (docs/design/pack-buildable-programs.md §8, BG-D11)
// through packMain, the verb's dispatch, over a real user config, the real pack selection and the
// launch's own build act (buildFork) with only the build jail's run pipeline substituted, as
// packtreebuild_test.go substitutes it.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

const packBuildFixtureBuild = `printf '#!/bin/sh\necho x-%s\n' "$(cat VERSION)" > x`

// packBuildHome is a temp HOME whose user config selects pack fx, a `file://` pack declaring the
// pack-tree program x (from tools/x, run x, built by build), plus a base pack and a fork of its
// program, and extra raw config keys. It returns fx's root.
func packBuildHome(t *testing.T, build, extra string) string {
	t.Helper()
	home := floortest.ResolvedTemp(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_RUNTIME", "podman")
	t.Chdir(floortest.ResolvedTemp(t))
	previousProbe := probeForkBuildContainer
	probeForkBuildContainer = func(string, string, time.Duration) (bool, bool) { return false, true }
	t.Cleanup(func() { probeForkBuildContainer = previousProbe })
	packs := floortest.ResolvedTemp(t)
	fx := filepath.Join(packs, "fx")
	writeFile(t, filepath.Join(fx, "tools", "x", "VERSION"), "1\n")
	buildJSON := strings.ReplaceAll(strings.ReplaceAll(build, `\`, `\\`), `"`, `\"`)
	writeFile(t, filepath.Join(fx, "pack.json"), `{"name":"fx","contributes":[{"kind":"program","bin":"x",`+
		`"via":"pack","from":"tools/x","build":"`+buildJSON+`","run":"x"}]}`)
	writeFile(t, filepath.Join(packs, "basepack", "pack.json"),
		`{"name":"basepack","contributes":[{"kind":"program","bin":"tool","via":"npm","package":"tool"}]}`)
	writeForkManifest(t, filepath.Join(packs, "forkpack", "pack.json"), "git+https://example.invalid/tool.git?ref=main")
	writeFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), `{`+extra+`"packs":[`+
		`{"source":"file://`+fx+`","name":"fx"},`+
		`{"source":"file://`+filepath.Join(packs, "basepack")+`","name":"basepack"},`+
		`{"source":"file://`+filepath.Join(packs, "forkpack")+`","name":"forkpack"}]}`)
	return fx
}

// fakePackBuildJail stands in for x's build jail: it leaves what the placement script leaves (the
// program's directory and its link) for this machine's jail platform, or exits rc after writing the
// toolchain record (the build line ran and failed). runs counts it.
func fakePackBuildJail(t *testing.T, runs *int, rc int) func(run.Options) int {
	t.Helper()
	return func(o run.Options) int {
		*runs++
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity-abc\n")
		if rc != 0 {
			return rc
		}
		out := filepath.Join(o.Workspace, captureOutLeaf)
		tree := capture.TreeDir(out)
		dir := filepath.Join(tree, ".local", "lib", "yolo-programs", "x")
		writeFile(t, filepath.Join(dir, "x"), "#!/bin/sh\necho x-1\n")
		if err := os.Chmod(filepath.Join(dir, "x"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(tree, ".local", "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../lib/yolo-programs/x/x", filepath.Join(tree, ".local", "bin", "x")); err != nil {
			t.Fatal(err)
		}
		m, err := capture.ManifestForTree(tree, "/home/agent", captureJailPlatform())
		if err != nil {
			t.Fatal(err)
		}
		if err := capture.WriteManifest(out, m); err != nil {
			t.Fatal(err)
		}
		return 0
	}
}

func packBuildRun(t *testing.T, args ...string) (rc int, out, errw string) {
	t.Helper()
	var o, e bytes.Buffer
	rc = packMain(append([]string{"build"}, args...), &o, &e, false)
	return rc, o.String(), e.String()
}

// THE CALL SITE AND THE CACHE: packMain dispatches `build` to the act, which builds x once; a second
// run finds the store's entry under the lock and says up to date with no jail; --force builds again.
// And the entry is the one a launch's identity selects, so the next launch builds nothing. Red if the
// dispatch is removed (the verb becomes "unknown verb", rc 1) or the act is not the launch's.
func TestPackBuildBuildsOnceThenIsUpToDateAndForceRebuilds(t *testing.T) {
	packBuildHome(t, packBuildFixtureBuild, "")
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))

	rc, out, errw := packBuildRun(t)
	if rc != 0 || runs != 1 || !strings.Contains(out, "built") || !strings.Contains(out, "program fx/x") {
		t.Fatalf("first run: rc %d, %d jail runs, want 0 and one build line\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
	if strings.Contains(out, "up to date") {
		t.Errorf("a fresh build said up to date:\n%s", out)
	}
	rc, out, errw = packBuildRun(t, "fx")
	if rc != 0 || runs != 1 || !strings.Contains(out, "up to date") {
		t.Fatalf("second run: rc %d, %d jail runs, want 0, no new run and up to date\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
	rc, out, errw = packBuildRun(t, "--force", "fx")
	if rc != 0 || runs != 2 || !strings.Contains(out, "built") || strings.Contains(out, "up to date") {
		t.Fatalf("--force: rc %d, %d jail runs, want 0 and a rebuild\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}

	// The launch's identity for the same staged bytes selects what this verb built.
	sel := selectConfiguredHostPacks()
	f := packload.PackTreePrograms(sel.packs)[0]
	digest, err := f.Digest()
	if err != nil {
		t.Fatal(err)
	}
	b := forkBuild{Fork: f, Commit: digest, Platform: forkBuildPlatform("")}
	store := &capture.Store{Dir: paths.CapturesDir()}
	if _, _, err := resolveForkBuild(store, "x", b.Platform, b.selectSource(), digest, b.recipe()); err != nil {
		t.Errorf("a launch's hit check finds no build of x: %v", err)
	}
}

// A failed build is one failed line naming its log and the retry, and the verb exits 1.
func TestPackBuildReportsAFailedBuildWithItsLogAndRetry(t *testing.T) {
	packBuildHome(t, packBuildFixtureBuild, "")
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 3))
	rc, out, errw := packBuildRun(t, "fx")
	if rc != 1 || runs != 1 {
		t.Fatalf("rc %d, %d jail runs, want 1 and one run\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
	for _, want := range []string{"failed", "program fx/x", "its log: ", "yolo pack build fx"} {
		if !strings.Contains(out, want) {
			t.Errorf("the failed line lacks %q:\n%s", want, out)
		}
	}
}

// A name the config does not select is refused with 2, naming `yolo pack ls`, and nothing builds.
func TestPackBuildRefusesAnUnknownPack(t *testing.T) {
	packBuildHome(t, packBuildFixtureBuild, "")
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	rc, out, errw := packBuildRun(t, "nope")
	if rc != 2 || runs != 0 || !strings.Contains(errw, "yolo pack ls") || !strings.Contains(errw, `"nope"`) {
		t.Fatalf("rc %d, %d runs, want 2, none, and `yolo pack ls`\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
}

// In a jail (config.InJail) the verb refuses with 2, naming the host command, before reading anything.
func TestPackBuildRefusesInAJail(t *testing.T) {
	packBuildHome(t, packBuildFixtureBuild, "")
	t.Setenv("YOLO_VERSION", "1.2.3")
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	rc, out, errw := packBuildRun(t, "fx", "--force")
	if rc != 2 || runs != 0 || !strings.Contains(errw, "on the host") || !strings.Contains(errw, "`yolo pack build fx --force`") {
		t.Fatalf("rc %d, %d runs, want 2, none, and the host command\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
}

// A fork in a named pack is skipped with a line naming `yolo capture <bin>`, and builds nothing.
func TestPackBuildNamesYoloCaptureForAFork(t *testing.T) {
	packBuildHome(t, packBuildFixtureBuild, "")
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	rc, out, errw := packBuildRun(t, "forkpack")
	if rc != 0 || runs != 0 || !strings.Contains(out, "yolo capture tool") {
		t.Fatalf("rc %d, %d runs, want 0, none, and `yolo capture tool`\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
	rc, out, _ = packBuildRun(t, "basepack")
	if rc != 0 || !strings.Contains(out, "nothing to build in pack basepack") {
		t.Errorf("a selected pack with no pack-tree program: rc %d\n%s", rc, out)
	}
}

// A dev override is noted and does not stop the build.
func TestPackBuildNotesADevOverrideAndStillBuilds(t *testing.T) {
	override := floortest.ResolvedTemp(t)
	packBuildHome(t, packBuildFixtureBuild, `"programs":{"overrides":{"x":{"path":"`+override+`"}}},`)
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	rc, out, errw := packBuildRun(t, "fx")
	if rc != 0 || runs != 1 || !strings.Contains(out, "dev override") || !strings.Contains(out, override) ||
		!strings.Contains(out, "built") {
		t.Fatalf("rc %d, %d runs, want the override noted and a build\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
}

// `--at host` on a Mac is the darwin act, which does not build a pack's tree yet: a failed line, exit
// 1, and no jail. A bad --at is a usage refusal.
func TestPackBuildAtHostOnAMacSaysItIsNotBuiltYet(t *testing.T) {
	packBuildHome(t, packBuildFixtureBuild, "")
	prev := packBuildHostGOOS
	packBuildHostGOOS = "darwin"
	t.Cleanup(func() { packBuildHostGOOS = prev })
	runs := 0
	withFakeCaptureJail(t, fakePackBuildJail(t, &runs, 0))
	rc, out, errw := packBuildRun(t, "--at", "host", "fx")
	if rc != 1 || runs != 0 || !strings.Contains(out, "not built for macOS") ||
		!strings.Contains(out, "yolo pack build fx --at host") {
		t.Fatalf("rc %d, %d runs\nout:\n%s\nerr:\n%s", rc, runs, out, errw)
	}
	if rc, _, errw := packBuildRun(t, "--at", "moon"); rc != 2 || !strings.Contains(errw, "jail") {
		t.Errorf("--at moon: rc %d, want a usage refusal\n%s", rc, errw)
	}
}

// `yolo pack --help` lists the verb.
func TestPackHelpListsBuild(t *testing.T) {
	var out, errw bytes.Buffer
	packMain([]string{"--help"}, &out, &errw, false)
	if !strings.Contains(out.String(), "yolo pack build [<pack>...] [--force] [--at jail|host]") {
		t.Errorf("yolo pack --help does not list build:\n%s", out.String())
	}
}
