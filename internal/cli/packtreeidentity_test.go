package cli

// packtreeidentity_test.go pins a pack-tree program's BUILD KEY (docs/design/pack-buildable-programs.md
// §6.1, BG-D3) at the fork route's own identity function, and the call sites that must keep a
// pack-tree program off the fork path: the build act's checkout and the host floor's fork pins.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

func packTreeForkValue() packload.Fork {
	p := &packload.Pack{Name: "fx", Root: "/staged/fx", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "x", Via: packdecl.ViaPack, From: "tools/x", Build: "make", Run: "x",
	}}}}
	return packload.PackTreePrograms([]*packload.Pack{p})[0]
}

// TestAPackTreeBuildIsKeyedOnItsTreeRecipeAndPlatform: the id is the fork route's formula with
// source "pack:<pack>/<from>", the tree digest as revision, the tagged recipe and the platform —
// and a change to any one of them is another build.
func TestAPackTreeBuildIsKeyedOnItsTreeRecipeAndPlatform(t *testing.T) {
	f := packTreeForkValue()
	digest := strings.Repeat("d", 64)
	b := forkBuild{Fork: f, Commit: digest, Platform: "linux/amd64"}
	if b.buildSource() != "pack:fx/tools/x" {
		t.Errorf("buildSource = %q, want pack:fx/tools/x", b.buildSource())
	}
	if b.recipe() != packdecl.PackTreeRecipe("tools/x", "make", "x") {
		t.Errorf("recipe = %q, want packdecl.PackTreeRecipe", b.recipe())
	}
	want := buildID("pack:fx/tools/x", digest, packdecl.PackTreeRecipe("tools/x", "make", "x"), "linux/amd64")
	if b.id() != want {
		t.Errorf("id = %s, want %s", b.id(), want)
	}
	other := func(mut func(*forkBuild)) string {
		c := b
		mut(&c)
		return c.id()
	}
	for name, id := range map[string]string{
		"another digest":   other(func(c *forkBuild) { c.Commit = strings.Repeat("e", 64) }),
		"another platform": other(func(c *forkBuild) { c.Platform = "linux/arm64" }),
		"another build":    other(func(c *forkBuild) { c.Fork.Build = "make all" }),
		"another run":      other(func(c *forkBuild) { c.Fork.Run = "bin/x" }),
		"another pack":     other(func(c *forkBuild) { c.Fork.Pack = "fy" }),
		"another from":     other(func(c *forkBuild) { c.Fork.From = "tools/y" }),
	} {
		if id == b.id() {
			t.Errorf("%s kept the build id %s", name, id)
		}
	}
	// A plain fork's identity is untouched: its source is its address and its recipe ForkSourceRecipe.
	fork := packload.Fork{Pack: "pi-matt", Base: "pi", Bin: "pi", Source: "git+https://example.com/pi?ref=main",
		Build: "make install", Produces: []string{".local/bin/pi"}}
	fb := forkBuild{Fork: fork, Commit: strings.Repeat("a", 40), Platform: "linux/amd64"}
	if fb.id() != buildID(fork.Source, fb.Commit, packdecl.ForkSourceRecipe(fork.Source, fork.Build, fork.Produces), "linux/amd64") {
		t.Error("a plain fork's build id moved")
	}
}

// TestTheBuildActRefusesAPackTreeProgramBeforeAnyCheckout: buildFork, the one build function, must
// never check out a pack-tree program's empty source address as a fork's. It refuses before the
// lock, so no lock file is made either.
func TestTheBuildActRefusesAPackTreeProgramBeforeAnyCheckout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	b := forkBuild{Fork: packTreeForkValue(), Commit: strings.Repeat("d", 64), Platform: "linux/amd64"}
	var out bytes.Buffer
	entry, err := buildFork(b, buildMode{lock: pidlock.Mode{}}, &out, io.Discard, false)
	if entry != nil || err == nil || !strings.Contains(err.Error(), "pack fx's own tree (tools/x)") {
		t.Fatalf("buildFork = %v, %v; want a refusal naming the pack's tree", entry, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(paths.GlobalStorage(), "locks", "fork-build-*")); len(matches) != 0 {
		t.Errorf("the refused build took a lock: %v", matches)
	}
}

// TestCaptureRefusesAPackTreeProgramNamingTheLaunch: `yolo capture <bin>` for a pack-tree program
// is neither a fork's build (no fork pack declares it) nor an npm program; it is refused, naming the
// pack's tree and the launch that builds it.
func TestCaptureRefusesAPackTreeProgramNamingTheLaunch(t *testing.T) {
	const tree = `{"kind":"program","bin":"board","via":"pack","from":"tools/board","build":"make","run":"board"},`
	malformedPackHome(t, tree, false, "")
	target, err := resolveCaptureTarget("board")
	if err == nil {
		t.Fatalf("a pack-tree program resolved as a capture target: %+v", target)
	}
	for _, want := range []string{"pack bad's own tree (tools/board)", "yolo -- board"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the capture refusal must contain %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "registry") {
		t.Errorf("the refusal files a pack-tree program with the npm programs: %v", err)
	}
}

// TestPackUpdateDoesNotRefreshAPackTreeProgram: `yolo pack update` in a jail runs no launcher for
// a pack-tree program and says what moves it — its tree — never a fork pack's pin.
func TestPackUpdateDoesNotRefreshAPackTreeProgram(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fx", "pack.json"),
		`{"name":"fx","contributes":[{"kind":"program","bin":"board","via":"pack","from":"tools/board","run":"board"}]}`)
	e := entrypoint.NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": root})
	if err := os.MkdirAll(e.LaunchDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.LaunchDir(), "board"), "#!/bin/sh\nexit 0\n")
	t.Cleanup(packload.OverrideSkewTolerance(false))
	var ran []string
	var out, errw bytes.Buffer
	rc := refreshPrograms(e, richtext.Printer{W: &out}, &errw,
		func(bin, path string) error { ran = append(ran, bin); return nil })
	if rc != 0 || len(ran) != 0 {
		t.Fatalf("rc=%d ran=%v, want no launcher run for a pack-tree program\n%s", rc, ran, errw.String())
	}
	if !strings.Contains(out.String(), "built from pack fx's own tree (tools/board)") ||
		strings.Contains(out.String(), "fork pack") {
		t.Errorf("the refresh does not say what moves a pack-tree program:\n%s", out.String())
	}
}

// TestTheFloorsForkPinsLeaveOutAPackTreeProgram: floorForkPins, the host floor's one read of the
// fork lock, lists a fork's program and never a pack-tree program's — which would otherwise carry a
// Fork with an empty source address and a "no usable pin" reason into the floor.
func TestTheFloorsForkPinsLeaveOutAPackTreeProgram(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tree := (&packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "x", Via: packdecl.ViaPack, From: "tools/x", Run: "x",
	}}}).InstallContributions()[0]
	fork := packdecl.Install{Kind: packdecl.InstallKindSource, Bin: "pi", ForkedBy: "pi-matt",
		Source: "git+https://example.com/pi?ref=main", Build: "make install", Produces: []string{".local/bin/pi"}}
	pins := floorForkPins([]hostfloor.Program{{Pack: "fx", Install: tree}, {Pack: "pi", Install: fork}})
	if _, ok := pins["x"]; ok {
		t.Errorf("floorForkPins carries a pack-tree program: %+v", pins["x"])
	}
	if _, ok := pins["pi"]; !ok {
		t.Errorf("floorForkPins dropped the fork: %+v", pins)
	}
}
