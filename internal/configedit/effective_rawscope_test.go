package configedit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestApplyEffectiveLegacyJSONCRejectsCanonicalRootsInWrongRawScope(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	for _, tc := range []struct {
		name, mutationRoot string
		scope              config.ExplicitEditScope
	}{
		{name: "project active provider", mutationRoot: "active_provider", scope: config.ExplicitEditProject},
		{name: "local active provider", mutationRoot: "active_provider", scope: config.ExplicitEditLocal},
		{name: "project allowance", mutationRoot: "allow", scope: config.ExplicitEditProject},
		{name: "project canonical provider field", mutationRoot: "providers", scope: config.ExplicitEditProject},
		{name: "user requests", mutationRoot: "requests", scope: config.ExplicitEditUser},
	} {
		for _, mode := range []string{"edit", "dry-run", "noop"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				home, workspace := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				path := filepath.Join(workspace, config.WorkspaceConfigName)
				if tc.scope == config.ExplicitEditLocal {
					writeEditFixture(t, path, `{}`)
					path = filepath.Join(workspace, config.WorkspaceLocalConfigName)
				}
				if tc.scope == config.ExplicitEditUser {
					path = filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
				}
				value := rawScopeValue(tc.mutationRoot)
				before := `{}`
				if tc.scope == config.ExplicitEditUser {
					before = `{"packs":["pi"]}`
				}
				if mode == "noop" {
					valueJSON := marshalRawScopeValue(t, value)
					before = `{"` + tc.mutationRoot + `":` + valueJSON + `}`
					if tc.scope == config.ExplicitEditUser {
						before = `{"packs":["pi"],"` + tc.mutationRoot + `":` + valueJSON + `}`
					}
				}
				writeEditFixture(t, path, before)
				beforeInfo, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				var neighborPath string
				var neighborBefore []byte
				if tc.scope == config.ExplicitEditLocal {
					neighborPath = filepath.Join(workspace, config.WorkspaceConfigName)
					neighborBefore, err = os.ReadFile(neighborPath)
					if err != nil {
						t.Fatal(err)
					}
				}
				writes := spySystemWrites(t)
				action := Add
				if mode == "noop" {
					action = Set
				}
				options := effectiveTestOptions(t, workspace, path, configdoc.JSONC, tc.scope, ApplyOptions{DryRun: mode == "dry-run"})
				_, err = ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: action, Path: []configdoc.Step{{Key: tc.mutationRoot}}, Value: value}, options)
				if err == nil || (!strings.Contains(err.Error(), "user-scope only") && !strings.Contains(err.Error(), "workspace-only key")) {
					t.Fatalf("canonical scope violation was not refused: err=%v writes=%d", err, *writes)
				}
				if tc.name == "user requests" && !strings.Contains(err.Error(), "move requests to the workspace config") {
					t.Fatalf("User requests refusal omits the workspace-config next step: %v", err)
				}
				if *writes != 0 {
					t.Fatalf("scope refusal performed %d config writes", *writes)
				}
				got, err := os.ReadFile(path)
				afterInfo, statErr := os.Stat(path)
				if err != nil || statErr != nil {
					t.Fatalf("scope refusal target could not be read/statted: err=%v statErr=%v", err, statErr)
				}
				if string(got) != before || afterInfo.Mode().Perm() != beforeInfo.Mode().Perm() {
					t.Fatalf("scope refusal changed target bytes or mode: got=%q mode=%v wantMode=%v", got, afterInfo.Mode().Perm(), beforeInfo.Mode().Perm())
				}
				if neighborPath != "" {
					neighborAfter, neighborErr := os.ReadFile(neighborPath)
					if neighborErr != nil || string(neighborAfter) != string(neighborBefore) {
						t.Fatalf("scope refusal changed neighbor %s: got=%q err=%v", neighborPath, neighborAfter, neighborErr)
					}
				}
				entries, err := os.ReadDir(filepath.Dir(path))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.Contains(entry.Name(), ".bak") || strings.Contains(entry.Name(), ".pre-migrate") {
						t.Fatalf("scope refusal created backup %s", entry.Name())
					}
				}
			})
		}
	}
}

func TestApplyEffectiveLegacyJSONCScopesPlannedProjectFragment(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	project := filepath.Join(workspace, config.WorkspaceConfigName)
	fragment := filepath.Join(workspace, "parts", "scope.jsonc")
	writeEditFixture(t, project, `{"include_if_found":["parts/scope.jsonc"]}`)
	writeEditFixture(t, fragment, `{}`)
	projectBefore, err := os.ReadFile(project)
	if err != nil {
		t.Fatal(err)
	}
	fragmentBefore, err := os.ReadFile(fragment)
	if err != nil {
		t.Fatal(err)
	}
	writes := spySystemWrites(t)
	options := effectiveTestOptions(t, workspace, fragment, configdoc.JSONC, config.ExplicitEditProject, ApplyOptions{})
	_, err = ApplyEffective(workspace, fragment, configdoc.JSONC, Mutation{Action: Add, Path: []configdoc.Step{{Key: "active_provider"}}, Value: rawScopeValue("active_provider")}, options)
	if err == nil || !strings.Contains(err.Error(), "user-scope only") || *writes != 0 {
		t.Fatalf("planned canonical scope in consumed fragment was not refused before writes: err=%v writes=%d", err, *writes)
	}
	for path, before := range map[string][]byte{project: projectBefore, fragment: fragmentBefore} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != string(before) {
			t.Errorf("refusal changed %s: got=%q err=%v", path, got, readErr)
		}
	}
}

func TestApplyEffectiveAcceptsCanonicalHostUserProviderAndAllowance(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	before := `{"packs":["git"],"active_provider":{"default":"native"},"allow":{"git":{"always":["fetch"]}}}`
	writeEditFixture(t, path, before)
	writes := spySystemWrites(t)
	options := effectiveTestOptions(t, workspace, path, configdoc.JSONC, config.ExplicitEditUser, ApplyOptions{})
	_, err := ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: Set,
		Path: []configdoc.Step{{Key: "active_provider"}}, Value: rawScopeValue("active_provider")}, options)
	if err != nil || *writes != 0 {
		t.Fatalf("valid host user canonical config was rejected or written: err=%v writes=%d", err, *writes)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != before {
		t.Fatalf("host user noop changed bytes: got=%q err=%v", got, err)
	}
}

func TestApplyEffectiveAcceptsCanonicalWorkspaceRequestOutsideCeiling(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	projectPath := filepath.Join(workspace, config.WorkspaceConfigName)
	writeEditFixture(t, userPath, `{"packs":["git"]}`)
	writeEditFixture(t, projectPath, `{"packages":["just"],"requests":{"git":{"push":["main"]}}}`)
	var sawSkipped bool
	options := effectiveTestOptions(t, workspace, projectPath, configdoc.JSONC, config.ExplicitEditProject, ApplyOptions{
		OnValidated: func(result config.ValidatedExplicitEditResult) {
			for _, request := range result.ReadableRequests() {
				if request.Capability == "push" && request.Status == config.ReadableRequestSkipped {
					sawSkipped = true
				}
			}
		},
	})
	writes := spySystemWrites(t)
	_, err := ApplyEffective(workspace, projectPath, configdoc.JSONC, Mutation{Action: Set,
		Path: []configdoc.Step{{Key: "packages"}}, Value: []any{"just"}}, options)
	if err != nil || !sawSkipped || *writes != 0 {
		t.Fatalf("accepted-but-skipped request changed semantics: err=%v sawSkipped=%t writes=%d", err, sawSkipped, *writes)
	}
}

func rawScopeValue(root string) map[string]any {
	switch root {
	case "active_provider":
		return map[string]any{"default": "native"}
	case "allow":
		return map[string]any{"git": map[string]any{"always": []any{"fetch"}}}
	case "providers":
		return map[string]any{"custom": map[string]any{"model": "gpt-test"}}
	case "requests":
		return map[string]any{"git": map[string]any{"push": []any{"main"}}}
	default:
		panic("unknown raw scope root " + root)
	}
}

func marshalRawScopeValue(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
