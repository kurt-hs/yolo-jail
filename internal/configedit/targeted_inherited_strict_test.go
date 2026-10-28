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

func TestApplyTargetedRejectsInvalidInheritedStateBeforeProspectiveCreation(t *testing.T) {
	for _, tc := range []struct {
		name, nested, sibling, siblingBody string
	}{
		{name: "malformed TOML syntax", nested: "[machine.resources\ncpus = 2\n"},
		{name: "unknown-key TOML", nested: "[machine.resources]\nunknown = true\n"},
		{name: "mixed JSONC", nested: "[machine.resources]\nmemory = '8g'\n", sibling: "inherited-launch.jsonc", siblingBody: `{"resources":{"memory":"16g"}}`},
		{name: "mixed JSON", nested: "[machine.resources]\nmemory = '8g'\n", sibling: "inherited-launch.json", siblingBody: `{"resources":{"memory":"16g"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := matrixCanonicalTempDir(t), matrixCanonicalTempDir(t)
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "9.9.9-test")
			t.Setenv(config.UserLayerEnv, "")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			if err := os.MkdirAll(userDir, 0o755); err != nil {
				t.Fatal(err)
			}
			ownerConfig := filepath.Join(userDir, paths.UserConfigLegacyJSONCName)
			ownerConfigBytes := []byte("{}\n")
			if err := os.WriteFile(ownerConfig, ownerConfigBytes, 0o600); err != nil {
				t.Fatal(err)
			}

			project := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			mutation := Mutation{Action: Add, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			if !target.IsProspective() || target.PhysicalPath != project || target.Role != config.ExplicitEditProject {
				t.Fatalf("ResolveTarget = %#v; want prospective Project %s", target, project)
			}
			parentInfo, err := os.Stat(workspace)
			if err != nil {
				t.Fatal(err)
			}
			entriesBefore, err := os.ReadDir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			entryNames := make([]string, len(entriesBefore))
			for i, entry := range entriesBefore {
				entryNames[i] = entry.Name()
			}

			nestedPath := filepath.Join(userDir, "inherited-launch.toml")
			nestedBytes := []byte(tc.nested)
			if err := os.WriteFile(nestedPath, nestedBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			ownerFiles := map[string][]byte{ownerConfig: ownerConfigBytes, nestedPath: nestedBytes}
			if tc.sibling != "" {
				siblingPath := filepath.Join(userDir, tc.sibling)
				siblingBytes := []byte(tc.siblingBody + "\n")
				if err := os.WriteFile(siblingPath, siblingBytes, 0o600); err != nil {
					t.Fatal(err)
				}
				ownerFiles[siblingPath] = siblingBytes
			}
			ownerModes := map[string]os.FileMode{}
			for path := range ownerFiles {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				ownerModes[path] = info.Mode().Perm()
			}
			userDirInfo, err := os.Stat(userDir)
			if err != nil {
				t.Fatal(err)
			}
			userEntriesBefore, err := os.ReadDir(userDir)
			if err != nil {
				t.Fatal(err)
			}
			userEntryNames := make([]string, len(userEntriesBefore))
			for i, entry := range userEntriesBefore {
				userEntryNames[i] = entry.Name()
			}

			if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "inherited-launch") {
				t.Fatalf("ApplyTargeted accepted invalid inherited state for a prospective target: %v", err)
			}
			if _, err := os.Lstat(project); !os.IsNotExist(err) {
				t.Fatalf("rejected prospective Project target was published: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)); !os.IsNotExist(err) {
				t.Fatalf("rejected prospective edit left a Local artifact: %v", err)
			}
			if _, err := os.Lstat(project + ".yolo-edit.lock"); !os.IsNotExist(err) {
				t.Fatalf("rejected prospective edit left its lock sidecar: %v", err)
			}
			entriesAfter, err := os.ReadDir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			gotNames := make([]string, len(entriesAfter))
			for i, entry := range entriesAfter {
				gotNames[i] = entry.Name()
			}
			if strings.Join(gotNames, "\x00") != strings.Join(entryNames, "\x00") {
				t.Fatalf("prospective rejection changed workspace parent entries: before=%v after=%v", entryNames, gotNames)
			}
			if afterInfo, err := os.Stat(workspace); err != nil || afterInfo.Mode().Perm() != parentInfo.Mode().Perm() {
				t.Fatalf("workspace parent mode changed: before=%v after=%v err=%v", parentInfo.Mode().Perm(), afterInfo, err)
			}
			userEntriesAfter, err := os.ReadDir(userDir)
			if err != nil {
				t.Fatal(err)
			}
			userEntryNamesAfter := make([]string, len(userEntriesAfter))
			for i, entry := range userEntriesAfter {
				userEntryNamesAfter[i] = entry.Name()
			}
			if strings.Join(userEntryNamesAfter, "\x00") != strings.Join(userEntryNames, "\x00") {
				t.Fatalf("rejected prospective edit changed User parent entries: before=%v after=%v", userEntryNames, userEntryNamesAfter)
			}
			if afterInfo, err := os.Stat(userDir); err != nil || afterInfo.Mode().Perm() != userDirInfo.Mode().Perm() {
				t.Fatalf("User parent mode changed: before=%v after=%v err=%v", userDirInfo.Mode().Perm(), afterInfo, err)
			}
			for path, before := range ownerFiles {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != string(before) {
					t.Fatalf("User owner file %s bytes changed: got=%q err=%v want=%q", path, got, err, before)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != ownerModes[path] {
					t.Fatalf("User owner file %s mode changed: info=%v err=%v want=%v", path, info, err, ownerModes[path])
				}
			}
		})
	}
}

func TestApplyTargetedRejectsInvalidOrMixedInheritedLaunchBeforeProjectMutation(t *testing.T) {
	for _, tc := range []struct {
		name, nested, sibling string
	}{
		{name: "unknown-key nested TOML", nested: "[machine.resources]\nunknown = true\n"},
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
