package config

import (
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// The drift diff names its two sides by what they are now that a live apply folds changes into
// the baseline (docs/design/live-config-apply.md LA-D8): the config this jail HAS, which is the
// config it booted with plus every change applied since, against the file on disk.
func TestTheDriftDiffNamesTheConfigThisJailHas(t *testing.T) {
	ws := t.TempDir()
	writeWSConfig(t, ws, `{"packages":["jq"]}`)
	wsCfg, _ := LoadWorkspaceConfig(ws, false, func(string) {})
	if _, err := WriteWorkspaceBootBaseline(ws, wsCfg); err != nil {
		t.Fatal(err)
	}
	writeWSConfig(t, ws, `{"packages":["jq","ripgrep"]}`)
	diff, _, _, err := WorkspaceConfigDrift(ws)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(diff, "\n")
	for _, want := range []string{"--- config this jail has", "+++ workspace config on disk now"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the drift diff does not label a side %q:\n%s", want, joined)
		}
	}
}

// Folding one key replaces exactly that key in the baseline with the workspace's current value,
// leaves every other key at its boot value, and deletes a key the workspace no longer has.
func TestFoldBootBaselineKeyReplacesOneKey(t *testing.T) {
	ws := t.TempDir()
	writeWSConfig(t, ws, `{"mounts":["/a"],"network":{"ports":["1:1"]},"packages":["jq"]}`)
	boot, _ := LoadWorkspaceConfig(ws, false, func(string) {})
	if _, err := WriteWorkspaceBootBaseline(ws, boot); err != nil {
		t.Fatal(err)
	}
	writeWSConfig(t, ws, `{"mounts":["/a","/b"],"network":{"ports":["2:2"]}}`)
	cur, _ := LoadWorkspaceConfig(ws, false, func(string) {})

	if err := FoldBootBaselineKey(ws, "mounts", cur); err != nil {
		t.Fatal(err)
	}
	if err := FoldBootBaselineKey(ws, "packages", cur); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadWorkspaceBootBaseline(ws)
	if err != nil || !ok {
		t.Fatalf("ReadWorkspaceBootBaseline: ok=%v err=%v", ok, err)
	}
	mounts, _ := got.Get("mounts")
	if s, _ := jsonx.DumpsCompact(mounts); s != `["/a", "/b"]` {
		t.Errorf("mounts was not folded to the current value: %s", s)
	}
	if _, has := got.Get("packages"); has {
		t.Error("a key the workspace no longer has was not deleted from the baseline")
	}
	network, _ := got.Get("network")
	if s, _ := jsonx.DumpsCompact(network); !strings.Contains(s, "1:1") {
		t.Errorf("an unfolded key moved: network = %s", s)
	}
	// The drift that remains is exactly the unfolded key.
	diff, hasDrift, _, _ := WorkspaceConfigDrift(ws)
	joined := strings.Join(diff, "\n")
	if !hasDrift || !strings.Contains(joined, "2:2") || strings.Contains(joined, `"/b"`) {
		t.Errorf("after folding mounts and packages, only network should drift:\n%s", joined)
	}
}

// With no baseline there is nothing to fold into: an error, and no file is created, so a jail
// launched before baselines existed keeps reporting "cannot determine" rather than a baseline
// holding one key.
func TestFoldBootBaselineKeyNeedsABaseline(t *testing.T) {
	ws := t.TempDir()
	writeWSConfig(t, ws, `{"packages":["jq"]}`)
	cur, _ := LoadWorkspaceConfig(ws, false, func(string) {})
	if err := FoldBootBaselineKey(ws, "packages", cur); err == nil {
		t.Fatal("folding into a missing baseline must fail")
	}
	if _, err := os.Stat(WorkspaceConfigBootPath(ws)); !os.IsNotExist(err) {
		t.Error("a fold with no baseline created one")
	}
}

// ConfigPartChanged is the side-effect-free half of the gate's comparison: true when the
// workspace config part differs from the approval record, false once it is recorded, and it
// writes nothing either way.
func TestConfigPartChangedComparesWithoutRecording(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := t.TempDir()
	writeWSConfig(t, ws, `{"packages":["jq"]}`)
	cur, _ := LoadWorkspaceConfig(ws, false, func(string) {})
	changed, err := ConfigPartChanged(ws, cur)
	if err != nil || !changed {
		t.Fatalf("an unrecorded non-empty config must read as changed: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(ApprovalSnapshotPath(ws)); !os.IsNotExist(err) {
		t.Fatal("ConfigPartChanged wrote the approval record")
	}
	if err := RecordApproval(ws, cur, nil); err != nil {
		t.Fatal(err)
	}
	if changed, err := ConfigPartChanged(ws, cur); err != nil || changed {
		t.Errorf("a recorded config must read as unchanged: changed=%v err=%v", changed, err)
	}
	empty := t.TempDir()
	if changed, _ := ConfigPartChanged(empty, jsonx.NewOrderedMap()); changed {
		t.Error("an empty config with no record is recorded silently by the gate, so it is not a change")
	}
}
