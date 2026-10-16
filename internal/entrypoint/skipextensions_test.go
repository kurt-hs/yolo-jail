package entrypoint

// skipextensions_test.go is the jail's half of YOLO_SKIP_EXTENSIONS
// (docs/design/patched-build-recovery.md §6.6): a key the host's wire marks skipped has every entry
// that loads it left out of the list the agent reads — no tree entry and no fallback — through the
// boot's own loop (ConfigurePackSurfaces), and the owner's launcher is not stopped for it.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestTheBootLeavesASkippedExtensionOutOfTheAgentsList(t *testing.T) {
	e, _ := overlayRenderEnv(t)
	e.Vars[PatchedTreesEnv] = PatchedTreesWire(map[string]TreeDelivery{
		fallbackKey: {Into: ".pi/agent/yolo-ext/pi-web-access", Reason: "skipped (YOLO_SKIP_EXTENSIONS)", Owner: "owner",
			Skipped: true},
	})
	owner, ext := listOwnerPack(t, "", nil), fallbackPack(t)
	bootJail(t, e, owner, ext)
	// Neither the tree's entry nor its fallback: the agent starts without the extension.
	wantPackages(t, e.Home, "npm:owner-a", "npm:owner-b", listKilo)
	var add []any
	_ = json.Unmarshal(ext.Decl.Contributes[1].Add, &add)
	if len(add) != 2 || add[1] != fallbackTree {
		t.Errorf("the boot rewrote the loaded pack's own list: %v", add)
	}
}

// A path INSIDE the skipped tree is left out too, and an unskipped tree beside it stays.
func TestTheBootLeavesEveryEntryLoadingASkippedTreeOut(t *testing.T) {
	e, _ := overlayRenderEnv(t)
	const inside = "~/.pi/agent/yolo-patched/subagents/index.ts"
	const kept = "~/.pi/agent/yolo-patched/goal"
	add, err := json.Marshal([]any{listKilo, inside, kept})
	if err != nil {
		t.Fatal(err)
	}
	ext := &packload.Pack{Name: "matt", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{
		{Kind: packdecl.KindFiles, Into: ".pi/agent/yolo-patched/subagents", Source: "git+https://h/o/sub?ref=main",
			Patches: "patches"},
		{Kind: packdecl.KindFiles, Into: ".pi/agent/yolo-patched/goal", Source: "git+https://h/o/goal?ref=main"},
		{Kind: packdecl.KindConfigList, Surface: "pi/settings", Path: "/packages", Add: add},
	}}}
	e.Vars[PatchedTreesEnv] = PatchedTreesWire(map[string]TreeDelivery{
		"matt/subagents": {Into: ".pi/agent/yolo-patched/subagents", Skipped: true},
		"matt/goal":      {Into: ".pi/agent/yolo-patched/goal", Build: "0123abcd"},
	})
	bootJail(t, e, listOwnerPack(t, "", nil), ext)
	wantPackages(t, e.Home, "npm:owner-a", "npm:owner-b", listKilo, kept)
}

// THE OWNER'S LAUNCHER IS NOT STOPPED for a skipped extension, even by a wire that also says stop.
func TestASkippedExtensionStopsNoLauncher(t *testing.T) {
	skipped := strings.Replace(stoppedTree, `"stop":true`, `"stop":true,"skipped":true`, 1)
	home, launcher, fakeBin, _ := forkLauncherWithTrees(t, `{"pi":{"key":"k1"}}`, skipped)
	if out, rc := runForkLauncher(t, home, launcher, fakeBin); rc != 0 || !strings.Contains(out, "FORK_BUILD_RAN k1") {
		t.Errorf("rc=%d, a skipped extension stopped pi:\n%s", rc, out)
	}
}
