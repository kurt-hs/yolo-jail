package cli

import (
	"bytes"
	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the host exec/apply callers, not just an offline store helper.
func TestPreparationHostConsumesGoodWithoutForegroundAdvance(t *testing.T) {
	for _, verb := range []string{"exec", "apply"} {
		t.Run(verb, func(t *testing.T) {
			fx := newTreeFixture(t, `"f.txt"`)
			fx.listTreeForAgent(t)
			fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true`)
			if !advanceHostTrees(io.Discard, false, "tool", nil) {
				t.Fatal("initial readiness failed")
			}
			fx.renderTrees(t, true)
			first, _ := os.Readlink(fx.link())
			fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
			fx.now = fx.now.Add(2 * time.Hour)
			fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true,"agent_updates":{"agentpack":"next-launch"}`)
			stubSpawn(t)
			stubBins(t, "tool", "other")
			got := captureHostExec(t)
			var out bytes.Buffer
			if verb == "exec" {
				if rc := hostExec(nil, []string{"tool"}, io.Discard, &out, nil); rc != 0 || !got.execed {
					t.Fatalf("exec rc=%d: %s", rc, out.String())
				}
			} else if rc := hostApply([]string{"--assert"}, io.Discard, &out, false, nil); rc != 0 {
				t.Fatalf("apply rc=%d: %s", rc, out.String())
			}
			if len(fx.builds) != 1 {
				t.Fatalf("warm next-launch ran a foreground build: %d\n%s", len(fx.builds), out.String())
			}
			if link, _ := os.Readlink(fx.link()); link != first {
				t.Fatalf("launch consumed unadmitted candidate: %s != %s", link, first)
			}
		})
	}
}

func TestPreparationHostFloorReadsAdmittedGood(t *testing.T) {
	fx := patchedFloorFixture(t)
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("cold rc=%d: %s", rc, out)
	}
	first := floorToolBody(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	fx.later(2 * time.Hour)
	cfg := filepath.Join(fx.home, ".config", "yolo-jail", "config.jsonc")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg, strings.TrimSuffix(strings.TrimSpace(string(data)), "}")+`,"agent_updates":{"forkpack":"next-launch"}}`)
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("warm rc=%d: %s", rc, out)
	}
	if len(fx.builds) != 1 || floorToolBody(t) != first {
		t.Fatal("next-launch floor waited for advance instead of consuming Good")
	}
}

func TestPreparationHostRejectsDiagnosticReadyWithoutGood(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true,"agent_updates":{"agentpack":"next-launch"}`)
	stubSpawn(t)
	selected := preparationSubscription{Key: fx.tree(t).Key(), Runtime: captureRuntime(), Platform: capture.Platform()}
	snapshot := preparationSnapshot{Key: selected.Key, Runtime: selected.Runtime, Platform: selected.Platform,
		InputIdentity: "stale-input", Generation: 19, Phase: "ready", ServingEntry: "not-an-admitted-entry"}
	path := filepath.Join(paths.BackgroundAdvanceDir(), "scheduler", "snapshots", preparationSubscriptionID(selected)+".json")
	if err := writePreparationJSON(path, snapshot); err != nil {
		t.Fatal(err)
	}
	stubBins(t, "tool")
	got := captureHostExec(t)
	var out bytes.Buffer
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &out, nil); rc != 0 || !got.execed {
		t.Fatalf("cold rc=%d: %s", rc, out.String())
	}
	if len(fx.builds) != 1 {
		t.Fatal("diagnostic snapshot bypassed cold readiness")
	}
}

func TestPreparationHostRejectsStaleRecipe(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn)
	advanceHostTrees(io.Discard, false, "tool", nil)
	manifest := filepath.Join(fx.treeDir, "pack.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, strings.Replace(string(data), `"build":"true"`, `"build":"echo changed"`, 1))
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true,"agent_updates":{"agentpack":"next-launch"}`)
	stubSpawn(t)
	stubBins(t, "tool")
	captureHostExec(t)
	var out bytes.Buffer
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &out, nil); rc != 0 {
		t.Fatalf("edited readiness rc=%d: %s", rc, out.String())
	}
	if len(fx.builds) != 2 {
		t.Fatalf("stale recipe bypassed readiness: %d builds", len(fx.builds))
	}
}

func TestPreparationHostUnownedLaunchRegistersNothing(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true,"agent_updates":{"agentpack":"next-launch"}`)
	stubSpawn(t)
	stubBins(t, "other")
	got := captureHostExec(t)
	var out bytes.Buffer
	if rc := hostExec(nil, []string{"other"}, io.Discard, &out, nil); rc != 0 || !got.execed {
		t.Fatalf("unowned rc=%d: %s", rc, out.String())
	}
	values, err := readPreparationSubscriptions(filepath.Join(paths.BackgroundAdvanceDir(), "scheduler"))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 || len(fx.builds) != 0 {
		t.Fatal("unowned launch registered or built another owner's key")
	}
}

func TestPreparationHostStatusIsDiagnosticAndPlatformQualified(t *testing.T) {
	newPatchedFixture(t, "")
	selected := preparationSubscription{Key: "forkpack/tool", Runtime: captureRuntime(), Platform: capture.Platform()}
	root := filepath.Join(paths.BackgroundAdvanceDir(), "scheduler")
	snapshot := preparationSnapshot{Key: selected.Key, Runtime: selected.Runtime, Platform: selected.Platform,
		InputIdentity: "old-input", Generation: 3, Phase: "inactive", Reason: "owner removed; select it before restarting"}
	path := filepath.Join(root, "snapshots", preparationSubscriptionID(selected)+".json")
	if err := writePreparationJSON(path, snapshot); err != nil {
		t.Fatal(err)
	}
	other := snapshot
	other.Platform = strings.Replace(selected.Platform, "linux", "darwin", 1)
	if other.Platform == selected.Platform {
		other.Platform = strings.Replace(selected.Platform, "darwin", "linux", 1)
	}
	other.Phase = "ready"
	other.Reason = "container-only-evidence"
	if err := writePreparationJSON(filepath.Join(root, "snapshots", preparationSubscriptionID(preparationSubscription{Key: other.Key, Runtime: other.Runtime, Platform: other.Platform})+".json"), other); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	var out, errw bytes.Buffer
	if rc := packMain([]string{"status", selected.Key}, &out, &errw, false); rc != 0 {
		t.Fatalf("status rc=%d: %s", rc, errw.String())
	}
	if !strings.Contains(out.String(), "diagnostic only") || !strings.Contains(out.String(), snapshot.Reason) || strings.Contains(out.String(), other.Reason) {
		t.Fatalf("unqualified or missing diagnostics: %s", out.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("status mutated scheduler evidence")
	}
}

func TestPreparationHostRetainsThreeUnknownVersions(t *testing.T) {
	dir := t.TempDir()
	first, second, third := filepath.Join(dir, "first"), filepath.Join(dir, "second"), filepath.Join(dir, "third")
	for _, path := range []string{first, second, third} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pruneHostTreeVersions(dir, third, second)
	for _, path := range []string{first, second, third} {
		if !isDir(path) {
			t.Fatalf("unproven version retired: %s", path)
		}
	}
}
