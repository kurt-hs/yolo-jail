package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func explicitEditTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestValidateExplicitEditRunsCommonValidationBeforeSnapshot(t *testing.T) {
	t.Setenv("HOME", explicitEditTempDir(t))
	t.Setenv("YOLO_VERSION", "")
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "valid planned config", body: "[machine.resources]\ncpus = 2\n"},
		{name: "common resource refusal", body: "[machine.resources]\ncpus = -1\n", want: "config.machine.resources.cpus: must be a positive number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := explicitEditTempDir(t)
			physical := filepath.Join(workspace, WorkspaceTOMLConfigName)
			write(t, physical, tc.body)
			input, err := NewConfigReadInput(physical, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.TOML)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ValidateExplicitEdit(workspace, target, input)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("common validation accepted invalid planned input or returned wrong error: %v", err)
				}
				if _, ok := result.SnapshotJSON(); ok {
					t.Fatal("invalid planned input produced a validated snapshot")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if snapshot, ok := result.SnapshotJSON(); !ok || !strings.Contains(snapshot, "\"cpus\": 2") {
				t.Fatalf("valid planned snapshot missing or incomplete: ok=%v snapshot=%s", ok, snapshot)
			}
		})
	}
}

func TestValidateExplicitEditUsesPlannedUserAllowanceSemantics(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{"packs":["git"],"allow":{"git":{"always":["fetch"]}}}`)
	planned := []byte(`{"packs":["git"],"allow":{"git":{"always":["unknown-capability"]}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(strings.ToLower(err.Error()), "capability") {
		t.Fatalf("planned unknown capability was not rejected by typed G validation: %v", err)
	}
}

func TestValidateExplicitEditUsesPlannedWorkspaceScopeForAdapters(t *testing.T) {
	t.Setenv("HOME", explicitEditTempDir(t))
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceConfigName)
	write(t, physical, `{}`)
	planned := []byte(`{"adapters":{"openai->anthropic":{"address":"https://example.invalid"}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(err.Error(), "config.adapters: user-scope only") {
		t.Fatalf("planned workspace-only adapter input was not refused: %v", err)
	}
}

func TestValidateExplicitEditUsesPlannedInactiveProviderSemantics(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{"packs":["pi"]}`)
	planned := []byte(`{"packs":["pi"],"providers":{"broken":{"models":{"child":{"twin_of":"missing"}}}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(err.Error(), "twin_of names a missing model row") {
		t.Fatalf("planned inactive canonical provider facts were not rejected by typed F validation: %v", err)
	}
}

func TestValidateExplicitEditAcceptsCanonicalProviderTableFromPlannedSource(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{}`)
	planned := []byte(`{"packs":["pi","claude","bedrock"],"providers":{"local":{"model":"base","via":"wire-bridge","every_model":{"api":"openai-responses","context":100},"models":{"global.openai.zulu":{"context":200,"price":{"input":0,"output":0,"cache_read":0,"cache_write":0}},"global.openai.alpha":{"twin_of":"global.openai.zulu"}},"variants":{"zeta":{"via":"wire-bridge"},"retired":"off","alpha":{"model":"variant"}}}},"active_provider":{"default":"native","pi":["local/zeta","bedrock"]}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err != nil {
		t.Fatalf("valid planned canonical provider table was rejected: %v", err)
	}
}

func TestValidateExplicitEditReturnsExistingSkippedReadableRequest(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	userPath := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, userPath, `{}`)
	planned := []byte(`{"packs":["github"],"allow":{"github":{"always":["read"]}}}`)
	input, err := NewConfigReadInput(userPath, planned)
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(workspace, WorkspaceConfigName)
	write(t, projectPath, `{"requests":{"github":{"create_pr":true}}}`)
	target, err := NewExplicitEditTarget(workspace, userPath, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ValidateExplicitEdit(workspace, target, input)
	if err != nil {
		t.Fatal(err)
	}
	requests := result.ReadableRequests()
	if len(requests) != 1 || requests[0].Path != "requests.github.create_pr" || requests[0].Status != ReadableRequestSkipped {
		t.Fatalf("typed readable request status was not retained: %#v", requests)
	}
	requests[0].Files = append(requests[0].Files, "caller-mutation")
	if len(result.ReadableRequests()[0].Files) != 1 {
		t.Fatal("validated readable request origins alias caller-owned slices")
	}
	firstSnapshot, _ := result.SnapshotJSON()
	second, err := ValidateExplicitEdit(workspace, target, input)
	if err != nil {
		t.Fatalf("warm planned validation: %v", err)
	}
	secondSnapshot, ok := second.SnapshotJSON()
	if !ok || secondSnapshot != firstSnapshot || len(second.ReadableRequests()) != 1 || second.ReadableRequests()[0].Status != ReadableRequestSkipped {
		t.Fatalf("warm result changed its typed plan: snapshot=%q ok=%v requests=%#v", secondSnapshot, ok, second.ReadableRequests())
	}
	if _, err := os.Stat(paths.HostFloorDir()); !os.IsNotExist(err) {
		t.Fatalf("validation created or found a host-floor catalog path in a fresh HOME: %v", err)
	}
}

func TestValidateExplicitEditUsesPlannedWorkspaceScopeForProfiles(t *testing.T) {
	t.Setenv("HOME", explicitEditTempDir(t))
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceConfigName)
	write(t, physical, `{}`)
	planned := []byte(`{"profiles":{"team":{"provider":"openai"}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(err.Error(), "config.profiles: user-scope only") {
		t.Fatalf("planned workspace-only profile input was not refused: %v", err)
	}
}

func TestExplicitProviderCatalogRetainsReferencedCatalogSourceFacts(t *testing.T) {
	t.Setenv("HOME", explicitEditTempDir(t))
	manifest := fstest.MapFS{
		"fixture/pack.json": {Data: []byte(`{"name":"fixture","contributes":[{"kind":"program","bin":"pi","via":"npm","package":"pi@1","model_catalog":["models.json"]}]}`)},
	}
	pack, problems, err := packload.ReadSelectedPackFS(manifest, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("read selected catalog declaration: problems=%v err=%v", problems, err)
	}
	floorDir := paths.HostFloorDir()
	installDir := filepath.Join(floorDir, "programs", "pi")
	packageDir := filepath.Join(installDir, "npm", "lib", "node_modules", "pi")
	entry := filepath.Join(packageDir, "bin", "pi")
	launcher := filepath.Join(floorDir, "bin", "pi")
	for _, path := range []string{filepath.Dir(entry), filepath.Dir(launcher)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, entry, "#!/bin/sh\n")
	write(t, launcher, "#!/bin/sh\n")
	write(t, filepath.Join(packageDir, "package.json"), `{"name":"pi","version":"1"}`)
	write(t, filepath.Join(packageDir, "models.json"), `{"models":[{"id":"source-model","vendor":"openai","api":"openai-responses"}]}`)
	record := hostfloor.Record{Schema: 1, Bin: "pi", Pack: "fixture", Via: "npm", Declared: "pi@1", Version: "1", Dir: installDir, Entry: entry, Exec: []string{entry}}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(floorDir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(floorDir, "records", "pi.json"), string(recordBytes))

	ctx := &editValidationContext{packs: []*packload.Pack{pack}, rawUser: editReadableMap(t, `{}`)}
	config := editReadableMap(t, `{"providers":{"local":{"models":{"alias-model":{"name":"Alias","vendor":"openai","api":"openai-responses","base":"source-model"}}}}}`)
	catalog, err := explicitProviderCatalog(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := catalog["local"]["source-model"]; !ok || got.Vendor != "openai" {
		t.Fatalf("catalog omitted the referenced source id: %#v", catalog)
	}
}

func TestExplicitProviderCatalogUsesReadOnlyHostFloorLookupColdAndWarm(t *testing.T) {
	t.Setenv("HOME", explicitEditTempDir(t))
	manifest := fstest.MapFS{
		"fixture/pack.json": {Data: []byte(`{"name":"fixture","contributes":[{"kind":"program","bin":"pi","via":"npm","package":"pi@1","model_catalog":["models.json"]}]}`)},
	}
	pack, problems, err := packload.ReadSelectedPackFS(manifest, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("read selected catalog declaration: problems=%v err=%v", problems, err)
	}
	ctx := &editValidationContext{packs: []*packload.Pack{pack}, rawUser: editReadableMap(t, `{}`)}
	config := editReadableMap(t, `{"providers":{"local":{"models":{"global.openai.model":{"name":"Model","vendor":"openai","api":"openai-responses"}}}}}`)
	for _, pass := range []string{"cold", "warm"} {
		catalog, err := explicitProviderCatalog(ctx, config)
		if err != nil {
			t.Fatalf("%s host-floor catalog read: %v", pass, err)
		}
		if catalog == nil || len(catalog) != 0 {
			t.Fatalf("%s absent installed catalog should yield no source facts: %#v", pass, catalog)
		}
		if _, err := os.Stat(paths.HostFloorDir()); !os.IsNotExist(err) {
			t.Fatalf("%s catalog lookup created host-floor state: %v", pass, err)
		}
	}
}

func TestReadExplicitEditConfigBindsRoleAndLeavesSemanticValidationUnclaimed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	for _, tc := range []struct {
		name   string
		file   string
		format configdoc.Format
		body   string
	}{
		{name: "canonical TOML", file: WorkspaceTOMLConfigName, format: configdoc.TOML, body: "[machine.resources]\ncpus = -1\n"},
		{name: "legacy JSONC", file: WorkspaceConfigName, format: configdoc.JSONC, body: `{"resources":{"cpus":-1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := explicitEditTempDir(t)
			physical := filepath.Join(workspace, tc.file)
			write(t, physical, tc.body)
			input, err := NewConfigReadInput(physical, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, tc.format)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ReadExplicitEditConfig(workspace, target, input)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.SnapshotJSON(), "-1") {
				t.Fatalf("planned negative CPU disappeared from the unvalidated read result: %s", result.SnapshotJSON())
			}
		})
	}
}

func TestReadExplicitEditConfigRejectsRoleAndFormatMismatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceTOMLConfigName)
	body := []byte("[machine.resources]\ncpus = 2\n")
	write(t, physical, string(body))
	input, err := NewConfigReadInput(physical, body)
	if err != nil {
		t.Fatal(err)
	}
	wrongRole, err := NewExplicitEditTarget(workspace, physical, ExplicitEditLocal, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, wrongRole, input); err == nil || !strings.Contains(err.Error(), "unauthorized config role project") {
		t.Fatalf("wrong scope accepted or wrong error: %v", err)
	}
	wrongFormat, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, wrongFormat, input); err == nil || !strings.Contains(err.Error(), "different config format") {
		t.Fatalf("wrong format accepted or wrong error: %v", err)
	}
	if _, err := ReadExplicitEditConfig(t.TempDir(), wrongRole, input); err == nil || !strings.Contains(err.Error(), "does not match config graph workspace") {
		t.Fatalf("mismatched graph workspace accepted or wrong error: %v", err)
	}
}

func TestReadExplicitEditConfigRejectsPhysicalTargetEncounteredAcrossProjectAndLocalRoles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceTOMLConfigName)
	body := []byte("[machine.resources]\ncpus = 2\n")
	write(t, physical, string(body))
	if err := os.Symlink(physical, filepath.Join(workspace, WorkspaceLocalTOMLConfigName)); err != nil {
		t.Fatal(err)
	}
	input, err := NewConfigReadInput(physical, body)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, target, input); err == nil || !strings.Contains(err.Error(), "conflicting or unauthorized config role local") {
		t.Fatalf("shared target accepted across project/local roles or wrong error: %v", err)
	}
}

func TestReadExplicitEditConfigRefusesUserScopeInsideJail(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "explicit-edit-test")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("[machine.resources]\ncpus = 2\n")
	write(t, physical, string(body))
	input, err := NewConfigReadInput(physical, body)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, target, input); err == nil || !strings.Contains(err.Error(), "only allowed on the host") || !strings.Contains(err.Error(), physical) {
		t.Fatalf("jailed user edit accepted or error lacks target/next action: %v", err)
	}
}
