package configedit

import (
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestResolvedEditTargetExpectedBytesPreservesNilAndCopiesNonempty(t *testing.T) {
	var unresolved config.ResolvedEditTarget
	if unresolved.ExpectedBytes() != nil {
		t.Fatal("an unresolved zero-value target must keep a nil absent expectation")
	}

	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	before := "[machine.resources]\ncpus=2\n"
	writeEditFixture(t, path, before)
	target, err := ResolveTarget(workspace, Mutation{Action: Set, Path: regressionPath("machine", "resources", "cpus"), Value: 4}, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	expectation := target.ExpectedBytes()
	if expectation == nil || string(expectation) != before {
		t.Fatalf("resolved nonempty expectation = %q", expectation)
	}
	expectation[0] = 'x'
	if again := target.ExpectedBytes(); again == nil || string(again) != before {
		t.Fatalf("ExpectedBytes accessor leaked mutable storage: %q", again)
	}
}
