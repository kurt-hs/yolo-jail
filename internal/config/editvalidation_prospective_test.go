package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestValidateExplicitEditRejectsProspectiveReceiptAsActualWitness(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	intent := EditMutationIntent{
		Kind:                     EditMutationAdd,
		Key:                      []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}},
		AllowProspectiveCreation: true,
	}
	target, err := ResolveEditTarget(workspace, intent, EditTargetChoice{Kind: EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	if !target.IsProspective() || target.Target.IsProspective() == false {
		t.Fatalf("absent standard Project TOML target lacks a distinct prospective binding: %#v", target)
	}
	if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceConfigTOMLName)); !os.IsNotExist(err) {
		t.Fatalf("target selection created a physical placeholder: %v", err)
	}
	if _, err := ValidateExplicitEdit(workspace, target.Target, ConfigReadInput{}); err == nil || !strings.Contains(err.Error(), "cannot be consumed as an actual explicit-file witness") {
		t.Fatalf("actual validator accepted prospective selection: %v", err)
	}
}
