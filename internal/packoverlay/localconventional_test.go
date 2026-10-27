package packoverlay

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packstage"
)

func TestLocalUseReaderCollectsLinkedConventionalOverlayForSelectedOwner(t *testing.T) {
	root := t.TempDir()
	dotfiles := t.TempDir()
	linkedDir := filepath.Join(dotfiles, "overlays")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := filepath.Join(dotfiles, "overlay.jsonc")
	if err := os.WriteFile(body, []byte(`{"linked":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(body, filepath.Join(linkedDir, "claude-settings@bedrock.jsonc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedDir, filepath.Join(root, "overlays")); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDirForLocalUse(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDirForLocalUse: %v", problems)
	}
	// The resolver sets Local from the resolved entry after loading; this also lets Collect
	// carry the already-decided origin policy to the later file read.
	contributor.Local = true
	owner := arraySurfaceOwner(t)
	profiles := map[string][]string{"claude": {"bedrock"}}
	set := Collect([]*packload.Pack{owner, contributor}, true, profiles)
	if len(set.Problems) != 0 || len(set.Orphans) != 0 {
		t.Fatalf("Collect linked overlay problems=%v orphans=%+v", set.Problems, set.Orphans)
	}
	if overlays := set.For("claude", "settings"); len(overlays) != 1 || overlays[0].Data.(map[string]any)["linked"] != true {
		t.Fatalf("selected owner did not receive the linked overlay: %+v", overlays)
	}
	dest := filepath.Join(t.TempDir(), "staged")
	if _, err := packstage.Stage(packstage.Spec{Root: root, Dest: dest, FollowSymlinks: true}); err != nil {
		t.Fatalf("stage local fixture: %v", err)
	}
	staged, stagedProblems := packload.LoadDirForUse(dest, "addon")
	if len(stagedProblems) != 0 {
		t.Fatalf("LoadDirForUse(staged local fixture): %v", stagedProblems)
	}
	staged.Local = true // the resolver preserves resolved origin on the materialized copy
	stagedSet := Collect([]*packload.Pack{owner, staged}, true, profiles)
	if len(stagedSet.Problems) != 0 || !reflect.DeepEqual(stagedSet.For("claude", "settings"), set.For("claude", "settings")) {
		t.Fatalf("in-place and materialized overlay results differ: in-place=%+v staged=%+v problems=%v", set.For("claude", "settings"), stagedSet.For("claude", "settings"), stagedSet.Problems)
	}
	inactive := Collect([]*packload.Pack{owner, contributor}, true, nil)
	if len(inactive.Problems) != 0 || len(inactive.For("claude", "settings")) != 0 {
		t.Fatalf("profile gate changed for linked overlay: problems=%v overlays=%+v", inactive.Problems, inactive.For("claude", "settings"))
	}
}

func TestCollectDoesNotFollowLinkedOverlayForOrdinaryUseReader(t *testing.T) {
	root := t.TempDir()
	dotfiles := t.TempDir()
	outside := filepath.Join(dotfiles, "outside.jsonc")
	if err := os.WriteFile(outside, []byte(`{"escaped":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "overlays", "claude-settings.jsonc")
	if err := os.WriteFile(inside, []byte(`{"inside":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDirForUse(root, "fetched")
	if len(problems) != 0 || len(contributor.OverlayFiles) != 1 {
		t.Fatalf("ordinary read fixture: pack=%v overlays=%v problems=%v", contributor, contributor.OverlayFiles, problems)
	}
	if err := os.Remove(inside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Fatal(err)
	}
	set := Collect([]*packload.Pack{arraySurfaceOwner(t), contributor}, true, nil)
	if len(set.Problems) == 0 || !strings.Contains(strings.Join(set.Problems, "\n"), "regular file") {
		t.Fatalf("later Collect read escaped symlink without a refusal: %v", set.Problems)
	}
	if len(set.For("claude", "settings")) != 0 {
		t.Fatalf("escaped file was applied: %+v", set.For("claude", "settings"))
	}
}
