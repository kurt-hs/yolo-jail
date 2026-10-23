package run

// prebuiltline_test.go pins a PACK-TREE PROGRAM's prebuilt artifact at the launch
// (docs/design/pack-buildable-programs.md §4, §7, BG-D10): a launch whose pack ships no source
// directory hands the build act the platform's prebuilt keyed on its sha256, says one line naming
// it and the sha256 it verified, and an attach to that jail says the same line.

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

const prebuiltTestURL = "https://example.invalid/x-linux"

var prebuiltTestSHA = strings.Repeat("c0ffee", 10) + "abcd"

// THE CALL SITE, Run's own entry point: the pin is the prebuilt (never a build of a tree that is
// not there), the jail is handed the key the act answers, and the launch prints the disclosure with
// the sha256's first 12 hex digits. Red if Run stops resolving through packTreePins, or the line is
// dropped.
func TestALaunchWithNoSourceTakesThePrebuiltAndSaysItsSHA(t *testing.T) {
	home := packHome(t)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_PACK_ROOT", "")
	pack := t.TempDir()
	manifest := `{"contributes":[{"kind":"program","bin":"x","via":"pack","from":"tools/x","run":"x",` +
		`"prebuilt":{"linux/` + goruntime.GOARCH + `":{"url":"` + prebuiltTestURL + `","sha256":"` + prebuiltTestSHA + `"}}}]}`
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeUserPacks(t, home, `[{"source":"file://`+pack+`","name":"fx"}]`)

	var got []packload.ForkPin
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		o.BuildForks = func(req ForkBuildRequest) map[string]entrypoint.ForkDelivery {
			got = append(got, req.Pins...)
			return map[string]entrypoint.ForkDelivery{"x": {Key: "k-prebuilt"}}
		}
	})
	if len(got) != 1 || !got[0].Fork.IsPrebuilt() || got[0].Commit != prebuiltTestSHA ||
		got[0].Fork.Prebuilt.URL != prebuiltTestURL {
		t.Fatalf("the build act was handed %+v, want x's prebuilt keyed on its sha256\n%s", got, printed)
	}
	if d := forkBuildsInArgv(t, argv); d["x"].Key != "k-prebuilt" {
		t.Errorf("the jail is handed %+v, want x's key k-prebuilt\n%s", d, printed)
	}
	want := "x: prebuilt artifact from pack fx (no tools/x in the pack) — sha256 " + prebuiltTestSHA[:12] +
		"… verified, " + prebuiltTestURL
	if strings.Count(printed, want) != 1 {
		t.Errorf("the launch does not say the prebuilt line once (want %q):\n%s", want, printed)
	}
}

// A prebuilt the slot could not deliver is never said to be verified: its warning and the
// readiness act's refusal say it.
func TestAnUndeliveredPrebuiltIsNotSaidVerified(t *testing.T) {
	var stderr strings.Builder
	o := &Options{Stderr: &stderr}
	f := packload.Fork{Pack: "fx", Bin: "x", From: "tools/x", Run: "x", PrebuiltWhy: "no tools/x in the pack",
		Prebuilt: &packdecl.PrebuiltBuild{URL: prebuiltTestURL, SHA256: prebuiltTestSHA}}
	o.forkPinned = []packload.ForkPin{{Fork: f, Commit: prebuiltTestSHA}}
	o.forkDelivered = map[string]entrypoint.ForkDelivery{"x": {Reason: "digest mismatch"}}
	o.notePrebuiltDeliveries()
	if strings.Contains(stderr.String(), "verified") {
		t.Errorf("an undelivered prebuilt was said verified: %q", stderr.String())
	}
	o.forkDelivered = map[string]entrypoint.ForkDelivery{"x": {Key: "k"}}
	o.notePrebuiltDeliveries()
	if !strings.Contains(stderr.String(), "sha256 "+prebuiltTestSHA[:12]) {
		t.Errorf("a delivered prebuilt was not said: %q", stderr.String())
	}
}

// AN ATTACH says the line again, from the record the launch left (recordHandedForks).
func TestAnAttachSaysThePrebuiltItsJailRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tree, err := newPackTree("yolo-prebuilt-attach")
	if err != nil {
		t.Fatal(err)
	}
	f := packload.Fork{Pack: "fx", Bin: "x", From: "tools/x", Run: "x", PrebuiltWhy: "no tools/x in the pack",
		Prebuilt: &packdecl.PrebuiltBuild{URL: prebuiltTestURL, SHA256: prebuiltTestSHA}}
	launch := &Options{Stderr: &strings.Builder{}, packTree: tree}
	launch.forkPinned = []packload.ForkPin{{Fork: f, Commit: prebuiltTestSHA}}
	launch.recordHandedForks(map[string]entrypoint.ForkDelivery{"x": {Key: "k"}})

	var stderr strings.Builder
	attach := &Options{Stderr: &stderr}
	attach.noteAttachHandedBuilds(attachPackView{staged: stagedPacks{root: tree}})
	want := PrebuiltLine("x", "fx", "no tools/x in the pack", prebuiltTestSHA, prebuiltTestURL)
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("the attach line = %q, want %q", stderr.String(), want)
	}
}
