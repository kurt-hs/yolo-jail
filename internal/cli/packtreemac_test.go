package cli

// packtreemac_test.go pins a PACK-TREE PROGRAM's build on a Mac (docs/design/pack-buildable-programs.md
// §9, the plan's Task 10): the sealed macos-user arm hands the darwin fork-build act (FP-D24) the
// placement script with the act's own checkout as its source, the launch's build child carries the
// same line, that line runs inside the act's own build script, and which bytes a Mac takes when its
// darwin act can never run (OQ-BG2's leaning).

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// ON macos-user A PACK-TREE PROGRAM'S SEALED BUILD REACHES THE FORK-BUILD ACT with its placement
// script copying from the act's own checkout (src/ in the build's darwin staging tree), never a
// container's /workspace/src, which does not exist on a Mac. The act is handed the host workspace's
// src/, where buildFork staged the pack's tree, and no tree directory. Have forkBuildRunJail hand the
// container line, and the act's build copies from nowhere.
func TestAPackTreeBuildOnMacosUserReachesTheForkBuildActWithItsPlacement(t *testing.T) {
	f, digest := packTreeBuildFixture(t, "make")
	var seen run.Options
	withFakeCaptureJail(t, func(o run.Options) int { seen = o; return 1 })
	got := stubMacForkBuildAct(t, 0)
	b := forkBuild{Fork: f, Commit: digest, Platform: "darwin/" + goruntime.GOARCH}
	ws := t.TempDir()
	var errw bytes.Buffer
	forkBuildRunJail(ws, b, captureStreams{out: &bytes.Buffer{}, errw: &errw}, false, captureAct{runtime: "macos-user"})
	if rc := runSealedMacArm(t, seen, ws); rc != 0 || len(*got) != 1 {
		t.Fatalf("the sealed macos-user arm returned %d and ran the fork-build act %d times, want 0 and once\n%s",
			rc, len(*got), errw.String())
	}
	o := (*got)[0]
	src := macosuser.ForkBuildSourceDir(macosuser.ForkBuildStagingRoot("", b.id()))
	if want := packTreePlacementScriptFrom(src, "x", "x", "make"); o.Build != want {
		t.Errorf("the act was handed build line\n %s\nwant the placement script from the act's checkout\n %s", o.Build, want)
	}
	if strings.Contains(o.Build, "/workspace/src") {
		t.Errorf("the darwin build copies from a container's /workspace/src: %s", o.Build)
	}
	if o.BuildID != b.id() || o.TreeDir != "" || o.Bin != "x" {
		t.Errorf("the act was handed id %q tree %q bin %q, want %q, no tree, x", o.BuildID, o.TreeDir, o.Bin, b.id())
	}
	if want := filepath.Join(ws, forkSourceLeaf); o.Source != want {
		t.Errorf("the act builds %q, want the staged pack tree %q", o.Source, want)
	}
}

// THE LAUNCH'S BUILD CHILD carries the line for the runtime it boots: the darwin act's on macos-user,
// a container's otherwise. A fork's line is the same on both.
func TestTheBuildChildCarriesThePlacementForItsRuntime(t *testing.T) {
	f, digest := packTreeBuildFixture(t, "make")
	b := forkBuild{Fork: f, Commit: digest, Platform: "darwin/" + goruntime.GOARCH}
	last := func(rt string) string { argv := forkBuildChildArgv("/s", b, false, rt); return argv[len(argv)-1] }
	if got, want := last("macos-user"), packTreePlacementScriptFrom(packTreeBuildSource("macos-user", b.id()), "x", "x", "make"); got != want {
		t.Errorf("macos-user child line:\n %s\nwant\n %s", got, want)
	}
	if got := last(""); got != packTreePlacementScript("x", "x", "make") {
		t.Errorf("a container child's line is not the container placement: %s", got)
	}
	fork := forkBuild{Fork: packload.Fork{Pack: "fp", Base: "base", Bin: "tool", Source: forkTestSource, Build: "make"}}
	if fork.buildLineFor("macos-user") != "make" {
		t.Errorf("a fork's line changed on macos-user: %q", fork.buildLineFor("macos-user"))
	}
}

// THE DARWIN ACT'S OWN SCRIPT RUNS THE PLACEMENT: macosuser.ForkBuildScript writes its toolchain
// record, cds into the checkout and runs the line it is handed, and the placement script then builds
// in ~/.local/lib/yolo-programs/<bin> and links ~/.local/bin/<bin> — run here through bash, as the act
// runs it (macOS's /bin/bash, whose cp, ln, test and chmod take the same flags).
func TestTheDarwinBuildScriptRunsThePlacement(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	src := filepath.Join(root, "src")
	writeFile(t, filepath.Join(src, "VERSION"), "4\n")
	build := `printf '#!/bin/sh\necho x-%s\n' "$(cat VERSION)" > x`
	script := macosuser.ForkBuildScript(src, filepath.Join(root, "toolchain"), "yolo test",
		packTreePlacementScriptFrom(src, "x", "x", build))
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the darwin build script failed: %v\n%s", err, out)
	}
	link := filepath.Join(home, packdecl.PackTreeProgramLink("x"))
	out, err := exec.Command(link).Output()
	if err != nil || strings.TrimSpace(string(out)) != "x-4" {
		t.Fatalf("running %s: %q %v, want x-4 from the program's directory", link, out, err)
	}
	if _, err := os.Stat(filepath.Join(root, "toolchain")); err != nil {
		t.Errorf("the act's toolchain record was not written first: %v", err)
	}
}

// OQ-BG2 AS RULED, ON A MAC: the darwin act can build a pack's tree in the run-as-you mode, and in
// the account mode once the sandbox account exists. With no account it never can — and still a
// declared darwin prebuilt does NOT stand in: `yolo pack install`'s prefetch fetches nothing for a
// present source on any Mac, and fetches the artifact only once the user opts in (BG-D18). Delete
// the prefetch's read of config.ProgramPrebuiltOptIns and the last assertion goes red.
func TestADarwinPrebuiltIsFetchedForAPresentSourceOnlyWithTheOptIn(t *testing.T) {
	if darwinPackTreeBuildable(macSetup{}.probes()) {
		t.Error("an account-mode Mac with no sandbox account reads as able to build")
	}
	if !darwinPackTreeBuildable(macSetup{account: true}.probes()) {
		t.Error("an account-mode Mac with the sandbox account reads as unable to build")
	}
	self := macSetup{}.probes()
	self.Mode = macosuser.ModeSelf
	if !darwinPackTreeBuildable(self) {
		t.Error("the run-as-you mode reads as unable to build, though it needs no account")
	}

	body := []byte("#!/bin/sh\necho prebuilt-x\n")
	url, hits, _ := servePrebuilt(t, body)
	f := prebuiltFixture(t, url, sha256Hex(body), true)
	pack := &packload.Pack{Name: "fx", Root: f.Root, Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "x", Via: packdecl.ViaPack, From: "tools/x", Run: "x",
		Prebuilt: map[string]packdecl.PrebuiltBuild{"darwin/" + goruntime.GOARCH: {URL: url, SHA256: sha256Hex(body)}},
	}}}}
	prefetch := func() {
		t.Helper()
		if rc := fetchPackTreePrebuilts([]*packload.Pack{pack}, packBinaryFetcher(), "darwin", goruntime.GOARCH,
			richtext.Printer{W: io.Discard}, io.Discard); rc != 0 {
			t.Fatalf("the prefetch failed: %d", rc)
		}
	}
	withMac(t, macSetup{account: true})
	prefetch()
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests for the darwin prebuilt on a Mac whose darwin act can build the tree, want none", n)
	}
	withMac(t, macSetup{})
	prefetch()
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests for the darwin prebuilt on a Mac with no sandbox account, want none: a "+
			"prebuilt is never substituted for a present source", n)
	}
	writeUserConfig(t, `{"programs": {"overrides": {"x": {"prebuilt": true}}}}`)
	prefetch()
	if n := hits.Load(); n != 1 {
		t.Errorf("%d requests for the darwin prebuilt once the user opted x into it, want one", n)
	}
}
