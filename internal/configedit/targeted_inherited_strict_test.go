package configedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestApplyTargetedRejectsInvalidOrMixedInheritedLaunchBeforeProjectMutation(t *testing.T) {
	for _, tc := range []struct {
		name, nested, sibling string
	}{
		{name: "malformed nested TOML", nested: "[machine.resources]\nunknown = true\n"},
		{name: "mixed JSONC sibling", nested: "[machine.resources]\nmemory = '8g'\n", sibling: "inherited-launch.jsonc"},
		{name: "mixed JSON sibling", nested: "[machine.resources]\nmemory = '8g'\n", sibling: "inherited-launch.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "9.9.9-test")
			t.Setenv(config.UserLayerEnv, "")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			if err := os.MkdirAll(userDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeEditFixture(t, filepath.Join(userDir, paths.UserConfigLegacyJSONCName), `{}`)
			project := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			const original = "[machine.resources]\ncpus = 2\n"
			writeEditFixture(t, project, original)
			mutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			writeEditFixture(t, filepath.Join(userDir, "inherited-launch.toml"), tc.nested)
			if tc.sibling != "" {
				writeEditFixture(t, filepath.Join(userDir, tc.sibling), `{"resources":{"memory":"16g"}}`)
			}
			if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "inherited-launch") {
				t.Fatalf("ApplyTargeted accepted invalid inherited input: %v", err)
			}
			got, err := os.ReadFile(project)
			if err != nil || string(got) != original {
				t.Fatalf("failed edit changed Project bytes (err=%v): %q", err, got)
			}
			if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)); !os.IsNotExist(err) {
				t.Fatalf("failed edit left a Local creation artifact: %v", err)
			}
		})
	}
}
