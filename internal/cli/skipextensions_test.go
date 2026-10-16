package cli

// skipextensions_test.go pins YOLO_SKIP_EXTENSIONS at the host (docs/reference/pi-extensions.md
// §9.1) and cached-good for an extension through the build slot (patched-forks.md §8.5), at their call sites:
// `yolo host apply --assert` leaves a skipped extension's entry out of the agent's list, writes no
// link for it and says so, and the next apply without the variable restores it; `yolo host -- tool`
// neither builds nor stops for it; and YOLO_USE_CACHED_GOOD naming an extension hands the record's
// older good build as the launch's per-launch copy, with no build.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// toolPackages is the `packages` list the host render wrote to the agent's settings.
func toolPackages(t *testing.T, home string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".tool", "settings.json"))
	if err != nil {
		return nil
	}
	var s struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("settings.json: %v\n%s", err, raw)
	}
	return s.Packages
}

func TestAHostApplyLeavesASkippedExtensionOutAndTheNextRestoresIt(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn)
	stubBins(t, "tool")
	const entry = "~/.tool/ext/tool-ext"

	t.Setenv(patchrecovery.SkipExtensionsEnv, "tool-ext")
	var out, errw bytes.Buffer
	if rc := hostApplyRefreshAndRender(&out, &errw, false, true, strings.NewReader(""), "", ""); rc != 0 {
		t.Fatalf("rc=%d\n%s%s", rc, out.String(), errw.String())
	}
	if len(fx.builds) != 0 {
		t.Errorf("the apply built a skipped extension")
	}
	if slices.Contains(toolPackages(t, fx.home), entry) {
		t.Errorf("the agent's list still names the skipped extension: %q", toolPackages(t, fx.home))
	}
	if _, err := os.Lstat(fx.link()); err == nil {
		t.Error("the apply linked a skipped extension")
	}
	if !strings.Contains(out.String()+errw.String(), "extension "+treeKeyCLI+": skipped (YOLO_SKIP_EXTENSIONS); agentpack starts without it") {
		t.Errorf("the apply does not say it skipped the extension:\n%s%s", out.String(), errw.String())
	}

	t.Setenv(patchrecovery.SkipExtensionsEnv, "")
	out.Reset()
	errw.Reset()
	if rc := hostApplyRefreshAndRender(&out, &errw, false, true, strings.NewReader(""), "", ""); rc != 0 {
		t.Fatalf("rc=%d\n%s%s", rc, out.String(), errw.String())
	}
	if !slices.Contains(toolPackages(t, fx.home), entry) || !isDir(fx.link()) {
		t.Errorf("the next apply without the variable did not restore it: list %q, link %v\n%s%s",
			toolPackages(t, fx.home), isDir(fx.link()), out.String(), errw.String())
	}
}

// A NAME COVERING NOTHING at the apply warns, and skips nothing.
func TestAHostApplyWarnsForASkipNameCoveringNothing(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn)
	t.Setenv(patchrecovery.SkipExtensionsEnv, "nosuch")
	var out, errw bytes.Buffer
	applyHost(&out, &errw, false, false, nil)
	if !strings.Contains(out.String()+errw.String(), "Warning: YOLO_SKIP_EXTENSIONS names nosuch, which matches no patched build here") {
		t.Errorf("no unmatched-name warning:\n%s%s", out.String(), errw.String())
	}
}

// `yolo host -- tool` WITH THE OWNER'S EXTENSION SKIPPED builds nothing, is not stopped for the
// missing tree, and says so.
func TestAHostLaunchOfTheOwnerWithItsExtensionSkippedStarts(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":false`)
	stubBins(t, "tool")
	got := captureHostExec(t)
	t.Setenv(patchrecovery.SkipExtensionsEnv, "treepack")
	var errw bytes.Buffer
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc != 0 || !got.execed {
		t.Fatalf("rc=%d, execed %v: the owner did not start with its extension skipped\n%s", rc, got.execed, errw.String())
	}
	if len(fx.builds) != 0 {
		t.Error("the launch built a skipped extension")
	}
	if !strings.Contains(errw.String(), "yolo host: extension "+treeKeyCLI+": skipped (YOLO_SKIP_EXTENSIONS)") {
		t.Errorf("the launch does not say it skipped the extension:\n%s", errw.String())
	}
}

// CACHED GOOD FOR AN EXTENSION, through the launch's build slot: once the build recipe changed and
// its build failed, YOLO_USE_CACHED_GOOD's key hands the recorded older good build as the per-launch
// copy, with no build and no record write.
func TestTheBuildSlotHandsAnExtensionsCachedGoodAsItsCopy(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	good, out := fx.deliver(t, true)
	if good.Dir == "" {
		t.Fatalf("the fixture admitted no good build:\n%s", out)
	}
	manifest := filepath.Join(fx.treeDir, "pack.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"build":"true"`, `"build":"true && true"`, 1)
	if edited == string(data) {
		t.Fatal("the fixture's build recipe was not found")
	}
	writeFile(t, manifest, edited)
	f := fx.tree(t)
	record, err := os.ReadFile(patchedAdvanceStore(true).CheckRecordPath(treeKeyCLI))
	if err != nil {
		t.Fatal(err)
	}
	builds := len(fx.builds)

	var stream bytes.Buffer
	req := run.TreeBuildRequest{Trees: []packload.Fork{f}, Platform: patchedTestPlatform, Runtime: "podman",
		Workspace: "/ws", Build: true, CopyRoot: filepath.Join(t.TempDir(), "tree.patched"),
		Interrupt: &run.ActInterrupt{}, CachedGood: []string{treeKeyCLI}}
	_, trees := runBuildSlot(run.BuildSlotRequest{Trees: &req, MaxBuilds: 1}, &stream, false)
	d := trees[treeKeyCLI]
	if d.Dir == "" || d.Entry != good.Entry {
		t.Fatalf("cached good handed %+v, want a copy of %s\n%s", d, good.Entry, stream.String())
	}
	if got, err := os.ReadFile(filepath.Join(d.Dir, "f.txt")); err != nil || len(got) == 0 {
		t.Errorf("the copy holds no tree: %q (%v)", got, err)
	}
	if len(fx.builds) != builds {
		t.Errorf("cached good ran a build: %d → %d", builds, len(fx.builds))
	}
	if after, _ := os.ReadFile(patchedAdvanceStore(true).CheckRecordPath(treeKeyCLI)); !bytes.Equal(after, record) {
		t.Error("cached good wrote the check record")
	}
	if !strings.Contains(stream.String(), "extension "+treeKeyCLI+": running the older build") {
		t.Errorf("cached good did not say what it runs:\n%s", stream.String())
	}

	// A PRUNED ENTRY is refused, with nothing copied.
	e, err := (&capture.Store{Dir: paths.CapturesDir()}).Resolve(good.Entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(e.Root); err != nil {
		t.Fatal(err)
	}
	_, trees = runBuildSlot(run.BuildSlotRequest{Trees: &req, MaxBuilds: 1}, &stream, false)
	if d := trees[treeKeyCLI]; d.Dir != "" || !strings.Contains(d.Reason, "cached good for "+treeKeyCLI+" is unavailable") {
		t.Errorf("a pruned cached good was handed: %+v", d)
	}
}
