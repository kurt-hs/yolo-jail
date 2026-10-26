package run

// configlockbriefing_test.go pins that generated mechanics guidance preserves the workspace config
// lock. The production staging call consumes the same applied FactSheet as the renderer.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBriefingSaysALockedWorkspaceConfigIsReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readonly []any
		locked   bool
	}{
		{"workspace_readonly set", []any{".git/hooks"}, true},
		{"workspace_readonly unset", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			ws := t.TempDir()
			emptyLoopholeDirs(t)
			if err := os.WriteFile(filepath.Join(ws, "yolo-jail.jsonc"), []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := appliedTestConfig()
			if tc.readonly != nil {
				cfg = appliedTestConfig("workspace_readonly", tc.readonly)
			}
			recipe := appliedMechanicsRecipe(t, appliedOptions(t, ws, home, false), "podman", cfg, "workspace-config.md")
			const lockedSays = "read-only"
			const editSays = "Edit `/workspace/yolo-jail.jsonc`"
			if got := strings.Contains(recipe, lockedSays); got != tc.locked {
				t.Errorf("generated recipe says the config is read-only = %v, want %v:\n%s", got, tc.locked, recipe)
			}
			if got := strings.Contains(recipe, editSays); got == tc.locked {
				t.Errorf("generated recipe tells the agent to edit the config = %v, want %v:\n%s", got, !tc.locked, recipe)
			}
		})
	}
}
