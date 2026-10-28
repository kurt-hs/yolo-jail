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

func regressionPath(keys ...string) []configdoc.Step {
	path := make([]configdoc.Step, len(keys))
	for i, key := range keys {
		path[i] = configdoc.Step{Key: key}
	}
	return path
}

func TestApplyTargetedResolvesAndAppliesIndexedEditsInBothFormats(t *testing.T) {
	for _, tc := range []struct {
		name, filename, before string
		format                 configdoc.Format
	}{
		{"JSONC", paths.WorkspaceConfigLegacyJSONCName, `{"machine":{"ports":[{"host":8080,"jail":80},{"host":8081,"jail":81}]}}`, configdoc.JSONC},
		{"TOML", paths.WorkspaceConfigTOMLName, "[[machine.ports]]\nhost=8080\njail=80\n[[machine.ports]]\nhost=8081\njail=81\n", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []Action{Set, Remove} {
				t.Run(map[Action]string{Set: "indexed-set", Remove: "indexed-remove"}[action], func(t *testing.T) {
					home, workspace := t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					t.Setenv("YOLO_VERSION", "")
					path := filepath.Join(workspace, tc.filename)
					writeEditFixture(t, path, tc.before)
					neighbor := filepath.Join(workspace, "neighbor.txt")
					writeEditFixture(t, neighbor, "untouched\n")
					beforeNeighbor, _ := os.ReadFile(neighbor)
					modeBefore, _ := os.Stat(path)
					item := append(regressionPath("machine", "ports"), configdoc.Step{Index: 0, IsIndex: true})
					mutation := Mutation{Action: Remove, Path: item}
					if action == Set {
						mutation = Mutation{Action: Set, Path: append(append([]configdoc.Step(nil), item...), configdoc.Step{Key: "host"}), Value: 8180}
					}
					target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
					if err != nil {
						t.Fatalf("resolve supported indexed mutation: %v", err)
					}
					if target.LogicalPath != path {
						t.Fatalf("indexed target=%s, want %s", target.LogicalPath, path)
					}
					if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
						t.Fatalf("apply supported indexed mutation: %v", err)
					}
					after, err := os.ReadFile(path)
					if err != nil || string(after) == tc.before {
						t.Fatalf("indexed edit did not change target: %q err=%v", after, err)
					}
					if action == Set && !strings.Contains(string(after), "8180") {
						t.Fatalf("indexed Set missed selected item: %q", after)
					}
					if action == Remove && strings.Contains(string(after), "8080") {
						t.Fatalf("indexed Remove kept selected item: %q", after)
					}
					afterNeighbor, _ := os.ReadFile(neighbor)
					modeAfter, _ := os.Stat(path)
					if string(afterNeighbor) != string(beforeNeighbor) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
						t.Fatalf("indexed edit touched neighbor or mode: neighbor=%q mode=%v", afterNeighbor, modeAfter.Mode().Perm())
					}
					if matches, _ := filepath.Glob(path + ".yolo-edit-*"); len(matches) != 0 {
						t.Fatalf("indexed edit left backup/temp files: %v", matches)
					}
				})
			}
		})
	}
}

func TestApplyTargetedResolvesOpaqueProviderDescendantsWithDeclaredOptions(t *testing.T) {
	for _, tc := range []struct {
		name, configName, fragmentName string
		format                         configdoc.Format
	}{
		{"JSONC", paths.UserConfigLegacyJSONCName, "", configdoc.JSONC},
		{"TOML", paths.UserConfigTOMLName, "10-provider.toml", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			userPath := filepath.Join(userDir, tc.configName)
			if tc.format == configdoc.TOML {
				writeEditFixture(t, userPath, "packs = ['zai']\n")
			} else {
				writeEditFixture(t, userPath, `{"packs":["zai"],"providers":{"zai":{"capabilities":["web_search","messages"],"variants":{"fast":{"model":"glm-5.3","options":{"model":"glm-5.3"}}}}}}`)
			}
			fragment := userPath
			if tc.fragmentName != "" {
				fragment = filepath.Join(userDir, "config.d", tc.fragmentName)
				writeEditFixture(t, fragment, "[providers.zai.variants.fast]\nmodel='glm-5.3'\noptions={ model='glm-5.3' }\ncapabilities=['web_search','messages']\n")
			}
			neighbor := filepath.Join(userDir, "config.d", "neighbor.txt")
			writeEditFixture(t, neighbor, "keep\n")
			beforeNeighbor, _ := os.ReadFile(neighbor)
			modeBefore, _ := os.Stat(fragment)
			mutation := Mutation{Action: Set, Path: regressionPath("providers", "zai", "variants", "fast", "options", "model"), Value: "glm-5.3-flash"}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve declared opaque descendant: %v", err)
			}
			if target.LogicalPath != fragment || target.Role != config.ExplicitEditUser {
				t.Fatalf("opaque User target=%s role=%v, want %s/User", target.LogicalPath, target.Role, fragment)
			}
			result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{})
			if err != nil || result.Plan == nil {
				t.Fatalf("apply declared opaque descendant: result=%#v err=%v", result, err)
			}
			if updated, readErr := os.ReadFile(fragment); readErr != nil || !strings.Contains(string(updated), "glm-5.3-flash") {
				t.Fatalf("opaque Set did not update its resolved source: %q err=%v", updated, readErr)
			}
			neighborAfter, _ := os.ReadFile(neighbor)
			modeAfter, _ := os.Stat(fragment)
			if string(neighborAfter) != string(beforeNeighbor) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
				t.Fatal("opaque edit changed neighbor or mode")
			}
			if tc.format == configdoc.TOML {
				indexed := Mutation{Action: Remove, Path: append(regressionPath("providers", "zai", "variants", "fast", "capabilities"), configdoc.Step{Index: 0, IsIndex: true})}
				target, err = ResolveTarget(workspace, indexed, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
				if err != nil {
					t.Fatalf("resolve indexed User descendant: %v", err)
				}
				if target.LogicalPath != fragment || target.Role != config.ExplicitEditUser {
					t.Fatalf("indexed User target=%s role=%v, want config.d fragment %s/User", target.LogicalPath, target.Role, fragment)
				}
				if _, err := ApplyTargeted(workspace, target, indexed, ApplyOptions{}); err != nil {
					t.Fatalf("apply indexed User descendant: %v", err)
				}
			}
			if _, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetProject}); err == nil || !strings.Contains(err.Error(), "user-scope only") {
				t.Fatalf("User-only opaque descendant accepted Project role: %v", err)
			}
		})
	}
}

func TestApplyTargetedKeepsProviderOpaqueDescendantsSemanticallyTyped(t *testing.T) {
	for _, tc := range []struct {
		name, configName, fragmentName string
		format                         configdoc.Format
	}{
		{"JSONC", paths.UserConfigLegacyJSONCName, "", configdoc.JSONC},
		{"TOML", paths.UserConfigTOMLName, "10-provider.toml", configdoc.TOML},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			userPath := filepath.Join(userDir, tc.configName)
			if tc.format == configdoc.TOML {
				writeEditFixture(t, userPath, "packs = ['zai']\n")
			} else {
				writeEditFixture(t, userPath, `{"packs":["zai"],"providers":{"zai":{"variants":{"fast":{"model":"glm-5.3","options":{"model":"glm-5.3"}}}}}}`)
			}
			fragment := userPath
			if tc.fragmentName != "" {
				fragment = filepath.Join(userDir, "config.d", tc.fragmentName)
				writeEditFixture(t, fragment, "[providers.zai.variants.fast]\nmodel='glm-5.3'\noptions={ model='glm-5.3' }\ncapabilities=['web_search','messages']\n")
			}
			neighbor := filepath.Join(workspace, "neighbor.txt")
			writeEditFixture(t, neighbor, "unchanged\n")
			before, _ := os.ReadFile(fragment)
			modeBefore, _ := os.Stat(fragment)
			path := append(regressionPath("providers", "zai", "variants", "fast", "options"), configdoc.Step{Key: "invented"})
			mutation := Mutation{Action: Add, Path: path, Value: "must-not-apply"}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve opaque semantic-negative target: %v", err)
			}
			_, err = ApplyTargeted(workspace, target, mutation, ApplyOptions{})
			if err == nil || !strings.Contains(err.Error(), "invented") {
				t.Fatalf("undeclared provider option was not semantically refused: %v", err)
			}
			after, _ := os.ReadFile(fragment)
			neighborAfter, _ := os.ReadFile(neighbor)
			modeAfter, _ := os.Stat(fragment)
			if string(after) != string(before) || string(neighborAfter) != "unchanged\n" || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
				t.Fatalf("semantic refusal changed files/mode: target=%q neighbor=%q", after, neighborAfter)
			}
			if matches, _ := filepath.Glob(fragment + ".yolo-edit-*"); len(matches) != 0 {
				t.Fatalf("semantic refusal left backup/temp files: %v", matches)
			}
		})
	}
}

func TestApplyTargetedPreservesEmptyTOMLPreimageForStaleChecks(t *testing.T) {
	for _, mode := range []string{"edit", "dry-run", "noop"} {
		t.Run(mode, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			writeEditFixture(t, path, "")
			mutation := Mutation{Action: Add, Path: regressionPath("machine", "resources", "cpus"), Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			expected := target.ExpectedBytes()
			if expected == nil || len(expected) != 0 {
				t.Fatalf("resolved empty preimage must be detached non-nil empty: %#v", expected)
			}
			expected = append(expected, 'x')
			if again := target.ExpectedBytes(); again == nil || len(again) != 0 {
				t.Fatalf("expected-byte accessor leaked mutable storage: %#v", again)
			}
			if mode == "noop" {
				writeEditFixture(t, path, "[machine.resources]\ncpus=4\n")
			} else {
				writeEditFixture(t, path, "[machine.resources]\ncpus=2\n")
			}
			modeBefore, _ := os.Stat(path)
			neighbor := filepath.Join(workspace, "neighbor.txt")
			writeEditFixture(t, neighbor, "stay\n")
			before, _ := os.ReadFile(path)
			neighborBefore, _ := os.ReadFile(neighbor)
			_, err = ApplyTargeted(workspace, target, mutation, ApplyOptions{DryRun: mode == "dry-run"})
			if err == nil || !strings.Contains(err.Error(), "changed since it was planned") {
				t.Fatalf("stale empty expectation was not refused in %s mode: %v", mode, err)
			}
			after, _ := os.ReadFile(path)
			neighborAfter, _ := os.ReadFile(neighbor)
			modeAfter, _ := os.Stat(path)
			if string(after) != string(before) || string(neighborAfter) != string(neighborBefore) || modeAfter.Mode().Perm() != modeBefore.Mode().Perm() {
				t.Fatalf("stale empty preimage changed target/neighbor/mode")
			}
			if matches, _ := filepath.Glob(path + ".yolo-edit-*"); len(matches) != 0 {
				t.Fatalf("stale refusal left backup/temp files: %v", matches)
			}
		})
	}
}

func TestApplyTargetedAllowsAnUnchangedEmptyTOMLPreimage(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "edit", true: "dry-run"}[dryRun], func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			writeEditFixture(t, path, "")
			mutation := Mutation{Action: Add, Path: regressionPath("machine", "resources", "cpus"), Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{DryRun: dryRun})
			if err != nil || result.Plan == nil {
				t.Fatalf("unchanged empty target result=%#v err=%v", result, err)
			}
			got, _ := os.ReadFile(path)
			if dryRun && len(got) != 0 {
				t.Fatalf("dry-run wrote empty target: %q", got)
			}
			if !dryRun && len(got) == 0 {
				t.Fatal("edit did not write empty target")
			}
		})
	}
}

func TestApplyTargetedNoopsOnUnchangedExistingAdd(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	before := "[machine.resources]\ncpus=4\n"
	writeEditFixture(t, path, before)
	mutation := Mutation{Action: Add, Path: regressionPath("machine", "resources", "cpus"), Value: 4}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{})
	got, readErr := os.ReadFile(path)
	if err != nil || result.Plan == nil || !result.Plan.Noop || readErr != nil || string(got) != before {
		t.Fatalf("unchanged Add no-op result=%#v err=%v bytes=%q readErr=%v", result, err, got, readErr)
	}
}
