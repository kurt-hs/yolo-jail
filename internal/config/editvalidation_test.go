package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
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

func explicitEditTreeSnapshot(t *testing.T, roots ...string) string {
	t.Helper()
	var entries []string
	for _, root := range roots {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			entries = append(entries, root+"|missing")
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			state := fmt.Sprintf("%s|%s|%o|%d|%d", path, info.Mode().Type(), info.Mode().Perm(), info.Size(), info.ModTime().UnixNano())
			if info.Mode().IsRegular() {
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(body)
				state += "|" + hex.EncodeToString(sum[:])
			} else if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				state += "|" + target
			}
			entries = append(entries, state)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return strings.Join(entries, "\n")
}

func resetEmbeddedForExplicitEditTest(t *testing.T) {
	t.Helper()
	fsys := packload.EmbeddedFS()
	packload.ReleaseEmbedded()
	t.Cleanup(func() {
		packload.ReleaseEmbedded()
		packload.SetEmbeddedFS(fsys)
	})
}

func TestExplicitContextNilUserMapsDoNotFallBackToOrdinaryConfig(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{"packs":"malformed","loopholes":{"github-broker":{"enabled":false}}}`)
	workspace := explicitEditTempDir(t)
	ctx := &editValidationContext{rawWorkspace: editReadableMap(t, `{}`)}
	if sources := validationUserSources(ctx); sources != nil {
		t.Fatalf("explicit nil source fell back to ordinary user sources: %#v", sources)
	}
	var errs []string
	validatePacks(workspace, &errs, ctx)
	if len(errs) != 0 {
		t.Fatalf("explicit nil raw-user map fell back to ordinary pack entries: %v", errs)
	}
	config := editReadableMap(t, `{"loopholes":{"github-broker":{"enabled":false}}}`)
	var warnings []string
	validateLoopholes(config, workspace, brokeredResolver, &errs, &warnings, ctx)
	for _, problem := range errs {
		if strings.Contains(problem, "github-broker.enabled") {
			t.Fatalf("explicit nil raw-user map fell back to the on-disk loophole switch: %v", errs)
		}
	}
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
	planned := []byte(`{"packs":["github","aws-auth"],"allow":{"github":{"always":["read"]}}}`)
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
	if len(result.ReadableWarnings()) == 0 {
		t.Fatal("selected aws-auth pack's existing missing-settings diagnostic was not retained")
	}
	readableWarnings := result.ReadableWarnings()
	readableWarnings[0] = "caller-mutation"
	if result.ReadableWarnings()[0] == "caller-mutation" {
		t.Fatal("validated readable diagnostics alias caller-owned slices")
	}
	requests := result.ReadableRequests()
	if len(requests) != 1 || requests[0].Path != "requests.github.create_pr" || requests[0].Status != ReadableRequestSkipped {
		t.Fatalf("typed readable request status was not retained: %#v", requests)
	}
	if len(requests[0].Files) == 0 {
		t.Fatalf("typed skipped request has no detached source origins: %#v", requests[0])
	}
	original := requests[0]
	originalFile := original.Files[0]
	requests[0].Files[0] = "caller-mutation"
	requests[0].Path = "caller-mutation"
	requests[0].Status = ReadableRequestGranted
	requests[0].Reason = "caller-mutation"
	observed := result.ReadableRequests()[0]
	if observed.Path != original.Path || observed.Status != original.Status || observed.Reason != original.Reason ||
		observed.Files[0] != originalFile {
		t.Fatalf("validated request exposed caller-mutable nested state: before=%#v after=%#v", original, observed)
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

func TestValidateExplicitEditSelectedCanonicalProviderViaUsesDefaultClosure(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{}`)
	planned := []byte(`{"packs":["pi"],"active_provider":{"default":"local"},"providers":{"local":{"via":"wire-bridge"}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := readExplicitEditContext(workspace, target, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.readRawScopes(); err != nil {
		t.Fatal(err)
	}
	selected, err := ctx.selectedPacks()
	if err != nil {
		t.Fatalf("resolve actual planned active_provider via closure: %v", err)
	}
	if !containsPack(selected, "wire-bridge") {
		t.Fatalf("planned provider via did not join the default selected-pack closure: %v", packNames(selected))
	}
	result, err := ValidateExplicitEdit(workspace, target, input)
	if err != nil {
		t.Fatalf("public planned explicit-edit validation rejected the selected provider via: %v", err)
	}
	if _, ok := result.SnapshotJSON(); !ok {
		t.Fatal("public explicit-edit validation returned no validated snapshot")
	}
}

func TestValidateExplicitEditCanonicalViaSelectionVariantsAliasesAndNative(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selector  string
		providers string
		wantVia   bool
	}{
		{name: "variant", selector: `"local/bridge"`, providers: `{"local":{"variants":{"bridge":{"via":"wire-bridge"}}}}`, wantVia: true},
		{name: "alias", selector: `"friendly"`, providers: `{"local":{"alias":"friendly","via":"wire-bridge"}}`, wantVia: true},
		{name: "native", selector: `"native"`, providers: `{"local":{"via":"wire-bridge"}}`, wantVia: false},
		{name: "provider-set-primary", selector: `["local","other"]`, providers: `{"local":{"via":"wire-bridge"},"other":{}}`, wantVia: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := explicitEditTempDir(t)
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			workspace := explicitEditTempDir(t)
			physical := paths.UserConfigPath()
			if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
				t.Fatal(err)
			}
			write(t, physical, `{}`)
			planned := []byte(`{"packs":["pi"],"active_provider":{"default":` + tc.selector + `},"providers":` + tc.providers + `}`)
			input, err := NewConfigReadInput(physical, planned)
			if err != nil {
				t.Fatal(err)
			}
			target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := readExplicitEditContext(workspace, target, input)
			if err != nil {
				t.Fatal(err)
			}
			if err := ctx.readRawScopes(); err != nil {
				t.Fatal(err)
			}
			selected, err := ctx.selectedPacks()
			if err != nil {
				t.Fatalf("resolve canonical provider via closure: %v", err)
			}
			if got := containsPack(selected, "wire-bridge"); got != tc.wantVia {
				t.Fatalf("wire-bridge selected=%v, want %v; selected=%v", got, tc.wantVia, packNames(selected))
			}
			result, err := ValidateExplicitEdit(workspace, target, input)
			if err != nil {
				t.Fatalf("public explicit-edit validation: %v", err)
			}
			if _, ok := result.SnapshotJSON(); !ok {
				t.Fatal("public explicit-edit validation returned no validated snapshot")
			}
		})
	}
}

func TestValidateExplicitEditRefusesCanonicalProviderViaOutsideEmbeddedClosure(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{}`)
	planned := []byte(`{"packs":["pi"],"active_provider":{"default":"local"},"providers":{"local":{"via":"not-shipped"}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(err.Error(), "not an embedded official pack") {
		t.Fatalf("planned active provider's unshipped via was not refused by actual closure: %v", err)
	}
}

func containsPack(packs []*packload.Pack, name string) bool {
	for _, pack := range packs {
		if pack != nil && pack.Name == name {
			return true
		}
	}
	return false
}

func packNames(packs []*packload.Pack) []string {
	names := make([]string, 0, len(packs))
	for _, pack := range packs {
		if pack != nil {
			names = append(names, pack.Name)
		}
	}
	return names
}

func TestValidateExplicitEditLegacyPackHandleUsesCapturedEmbeddedSource(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	resetEmbeddedForExplicitEditTest(t)
	alpha := fstest.MapFS{"alpha/pack.json": {Data: []byte(`{"name":"alpha","contributes":[{"kind":"program","bin":"alpha-cli","via":"npm","package":"alpha@1"}]}`)}}
	beta := fstest.MapFS{"beta/pack.json": {Data: []byte(`{"name":"beta","contributes":[{"kind":"program","bin":"beta-cli","via":"npm","package":"beta@1"}]}`)}}
	packload.SetEmbeddedFS(alpha)
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{}`)
	input, err := NewConfigReadInput(physical, []byte(`{"packs":["alpha"]}`))
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := readExplicitEditContext(workspace, target, input)
	if err != nil {
		t.Fatal(err)
	}
	packload.SetEmbeddedFS(beta)
	if err := ctx.readRawScopes(); err != nil {
		t.Fatal(err)
	}
	ctx.packs, err = ctx.selectedPacks()
	if err != nil {
		t.Fatalf("planned JSONC bare embedded handle did not resolve from the captured source: %v", err)
	}
	if !containsPack(ctx.packs, "alpha") {
		t.Fatalf("planned legacy selection did not retain captured alpha: %v", packNames(ctx.packs))
	}
	namespace, _, err := explicitEditProfileNamespaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, hasAlpha := namespace["alpha-cli"]
	_, hasBeta := namespace["beta-cli"]
	if !hasAlpha || hasBeta {
		t.Fatalf("planned legacy namespace did not use captured embedded source: %v", namespace)
	}
	if packload.EmbeddedLoaded() {
		t.Fatal("legacy source-coherence check materialized embedded packs")
	}
}

func TestValidateExplicitEditProfileNamespaceUsesCapturedEmbeddedSource(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	resetEmbeddedForExplicitEditTest(t)
	alpha := fstest.MapFS{"alpha/pack.json": {Data: []byte(`{"name":"alpha","contributes":[{"kind":"program","bin":"alpha-cli","via":"npm","package":"alpha@1"}]}`)}}
	beta := fstest.MapFS{"beta/pack.json": {Data: []byte(`{"name":"beta","contributes":[{"kind":"program","bin":"beta-cli","via":"npm","package":"beta@1"}]}`)}}
	packload.SetEmbeddedFS(alpha)
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, "format = 2\npacks = []\n")
	planned := []byte("format = 2\npacks = [\"alpha\"]\n")
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := readExplicitEditContext(workspace, target, input)
	if err != nil {
		t.Fatal(err)
	}
	packload.SetEmbeddedFS(beta)
	if err := ctx.readRawScopes(); err != nil {
		t.Fatal(err)
	}
	ctx.packs, err = ctx.selectedPacks()
	if err != nil {
		t.Fatalf("planned TOML bare embedded handle did not resolve from the captured source: %v", err)
	}
	if !containsPack(ctx.packs, "alpha") {
		t.Fatalf("planned selection did not retain the captured alpha pack: %v", packNames(ctx.packs))
	}
	namespace, _, err := explicitEditProfileNamespaces(ctx)
	if err != nil {
		t.Fatalf("read captured invocation namespace: %v", err)
	}
	if _, hasAlpha := namespace["alpha-cli"]; !hasAlpha || namespace["beta-cli"] {
		t.Fatalf("namespace crossed the invocation's captured source: %#v", namespace)
	}
	if packload.EmbeddedLoaded() {
		t.Fatal("source-coherence check materialized embedded packs")
	}
}

func TestValidateExplicitEditLegacyProfileNamespaceUsesPlannedCustomPackWithoutMaterializing(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	resetEmbeddedForExplicitEditTest(t)
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{"packs":["pi"]}`)
	customPack := filepath.Join(explicitEditTempDir(t), "custom-pack")
	if err := os.MkdirAll(customPack, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(customPack, "pack.json"), `{"name":"custom-pack","contributes":[{"kind":"program","bin":"planned-cli","via":"npm","package":"planned-cli@1"}]}`)
	planned := []byte(`{"packs":[{"source":"` + customPack + `","name":"custom-pack"}],"profile":{"planned-cli":"unvalidated-profile-name"}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if packload.EmbeddedLoaded() {
		t.Fatal("fixture began after embedded-pack materialization")
	}
	roots := []string{home, workspace, customPack, paths.PacksDir(), paths.HostFloorDir()}
	before := explicitEditTreeSnapshot(t, roots...)
	for _, pass := range []string{"cold", "warm"} {
		result, err := ValidateExplicitEdit(workspace, target, input)
		if err != nil {
			t.Fatalf("%s validation did not use the planned custom pack CLI namespace: %v", pass, err)
		}
		if _, ok := result.SnapshotJSON(); !ok {
			t.Fatalf("%s explicit edit returned no validated snapshot", pass)
		}
		if packload.EmbeddedLoaded() {
			t.Fatalf("%s public validation materialized embedded packs", pass)
		}
		if after := explicitEditTreeSnapshot(t, roots...); after != before {
			t.Fatalf("%s public validation changed the observed file trees", pass)
		}
	}
}

func TestValidateExplicitEditUsesPlannedUserBrokeredScopeForWorkspaceGrants(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	userPath := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, userPath, `{"brokered":{"git":{"grant_kinds":[]}}}`)
	workspacePath := filepath.Join(workspace, WorkspaceConfigName)
	write(t, workspacePath, `{"brokered":{"git":{"grants":[{"kind":"branch","repo":"github.com/example/repo","branch":"main"}]}}}`)
	planned := []byte(`{"brokered":{"git":{"grant_kinds":["branch"]}}}`)
	input, err := NewConfigReadInput(userPath, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, userPath, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err != nil {
		t.Fatalf("common brokered validation did not use planned user bytes for workspace grants: %v", err)
	}
}

func TestValidateExplicitEditRefusesPlannedProjectLegacyWritableMount(t *testing.T) {
	t.Setenv("HOME", explicitEditTempDir(t))
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceConfigName)
	write(t, physical, `{}`)
	source := explicitEditTempDir(t)
	planned := []byte(`{"mounts":[{"host":"` + source + `","at":"/ctx/writable","mode":"rw"}]}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(err.Error(), "config.mounts") {
		t.Fatalf("public validation did not refuse a planned project-scope writable legacy mount: %v", err)
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

func TestValidateExplicitEditRefusesStagedFetchedPackWithoutProvableSource(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{}`)
	stagedRoot := filepath.Join(explicitEditTempDir(t), "delivered")
	entry := PackEntry{Source: "git+https://example.invalid/org/repo//sub?ref=main", Name: "staged-pack"}
	stagedPack := filepath.Join(stagedRoot, entry.Slug())
	if err := os.MkdirAll(stagedPack, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stagedPack, "pack.json"), `{"name":"staged-pack"}`)
	t.Setenv("YOLO_PACK_ROOT", stagedRoot)
	planned := []byte(`{"packs":[{"source":"` + entry.Source + `","name":"staged-pack"}]}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{home, workspace, stagedRoot, paths.PacksDir()}
	before := explicitEditTreeSnapshot(t, roots...)
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil ||
		!strings.Contains(err.Error(), "only resolves through staged tree") || !strings.Contains(err.Error(), "rerun the host launch") {
		t.Fatalf("public validation did not fail closed with an actionable staged-source refusal: %v", err)
	}
	if after := explicitEditTreeSnapshot(t, roots...); after != before {
		t.Fatal("refusing an unprovable staged source changed the observed file trees")
	}
	if packload.EmbeddedLoaded() {
		t.Fatal("staged-source refusal materialized embedded packs")
	}
}

func TestValidateExplicitEditLoadsPlannedCatalogFactsFromHostFloorWithoutWrites(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	resetEmbeddedForExplicitEditTest(t)
	workspace := explicitEditTempDir(t)
	physical := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, physical, `{}`)
	packOne := filepath.Join(explicitEditTempDir(t), "catalog-pack-one")
	packTwo := filepath.Join(explicitEditTempDir(t), "catalog-pack-two")
	for _, packDir := range []string{packOne, packTwo} {
		if err := os.MkdirAll(packDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(packOne, "pack.json"), `{"name":"catalog-pack-one","contributes":[{"kind":"program","bin":"catalog-cli","via":"npm","package":"catalog-pkg@1","model_catalog":["first.json"],"protocols":["openai-responses"],"provider_sets":true}]}`)
	write(t, filepath.Join(packTwo, "pack.json"), `{"name":"catalog-pack-two","contributes":[{"kind":"program","bin":"catalog-cli","via":"npm","package":"shadow-pkg@1","model_catalog":["second.json"]}]}`)
	floorDir := paths.HostFloorDir()
	installDir := filepath.Join(floorDir, "programs", "catalog-cli", "installed")
	packageDir := filepath.Join(installDir, "npm", "lib", "node_modules", "catalog-pkg")
	entry := filepath.Join(packageDir, "bin", "catalog-cli")
	launcher := filepath.Join(floorDir, "bin", "catalog-cli")
	for _, path := range []string{filepath.Dir(entry), filepath.Dir(launcher), filepath.Join(floorDir, "records")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(t, entry, "catalog cli fixture\n")
	write(t, launcher, "catalog launcher fixture\n")
	write(t, filepath.Join(packageDir, "package.json"), `{"name":"catalog-pkg","version":"1"}`)
	firstCatalog := `{"models":[{"id":"source-model-one","vendor":"openai","api":"openai-responses","context":100000,"price":{"input":0,"output":0,"cache_read":0,"cache_write":0}}]}`
	write(t, filepath.Join(packageDir, "first.json"), firstCatalog)
	write(t, filepath.Join(packageDir, "second.json"), `{"models":[{"id":"source-model-two","vendor":"anthropic","api":"anthropic","context":200000,"price":{"input":0,"output":0,"cache_read":0,"cache_write":0}}]}`)
	record := hostfloor.Record{Schema: 1, Bin: "catalog-cli", Pack: "catalog-pack-one", Via: "npm", Declared: "catalog-pkg@1", Version: "1", Dir: installDir, Entry: entry, Exec: []string{entry}}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(floorDir, "records", "catalog-cli.json"), string(recordBytes))

	planned := []byte(`{"packs":[{"source":"` + packOne + `","name":"catalog-pack-one"},{"source":"` + packTwo + `","name":"catalog-pack-two"}],"providers":{"local":{"models":{"source-model-one":{"name":"One"},"alias-one":{"name":"Alias","base":"source-model-one","twin_of":"source-model-one"}}},"other":{"models":{"source-model-two":{"name":"Two"}}}}}`)
	input, err := NewConfigReadInput(physical, planned)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if packload.EmbeddedLoaded() {
		t.Fatal("fixture began after embedded-pack materialization")
	}
	write(t, filepath.Join(packageDir, "first.json"), `{"models":[{"id":"broken","price":{"input":"free"}}]}`)
	if _, err := ValidateExplicitEdit(workspace, target, input); err == nil || !strings.Contains(err.Error(), "offline provider model catalog for catalog-cli") {
		t.Fatalf("public validation did not read the declared host-floor catalog through its default producer: %v", err)
	}
	write(t, filepath.Join(packageDir, "first.json"), firstCatalog)
	if packload.EmbeddedLoaded() {
		t.Fatal("catalog-read control materialized embedded packs")
	}
	roots := []string{home, workspace, packOne, packTwo, paths.PacksDir(), floorDir}
	before := explicitEditTreeSnapshot(t, roots...)
	ctx, err := readExplicitEditContext(workspace, target, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.readRawScopes(); err != nil {
		t.Fatal(err)
	}
	ctx.packs, err = ctx.selectedPacks()
	if err != nil {
		t.Fatal(err)
	}
	ctx.catalog, err = explicitProviderCatalog(ctx, ctx.effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct {
		provider string
		id       string
		vendor   string
		api      string
		context  int64
	}{
		{provider: "local", id: "source-model-one", vendor: "openai", api: "openai-responses", context: 100000},
		{provider: "local", id: "source-model-two", vendor: "anthropic", api: "anthropic", context: 200000},
		{provider: "other", id: "source-model-one", vendor: "openai", api: "openai-responses", context: 100000},
		{provider: "other", id: "source-model-two", vendor: "anthropic", api: "anthropic", context: 200000},
	} {
		got, ok := ctx.catalog[expected.provider][expected.id]
		if !ok || got.Vendor != expected.vendor || got.API != expected.api || got.Context != expected.context {
			t.Fatalf("default planned catalog = %#v; provider %s/%s = %#v, present=%v; want vendor=%s api=%s context=%d", ctx.catalog, expected.provider, expected.id, got, ok, expected.vendor, expected.api, expected.context)
		}
	}
	errs, _ := validateConfigWithEditContext(ctx.effective, ctx.workspace, nil, ctx)
	if len(errs) != 0 {
		t.Fatalf("same default selected packs/catalog did not reach typed provider validation: %v", errs)
	}
	if ctx.semantic == nil {
		t.Fatal("default typed F/G validation did not retain semantic inputs")
	}
	var localDeclaration, otherDeclaration packdecl.ProviderContribution
	for _, declaration := range ctx.semantic.ProviderInput.Declarations {
		switch declaration.Name {
		case "local":
			localDeclaration = declaration
		case "other":
			otherDeclaration = declaration
		}
	}
	localModels, err := ComposeProviderModels(localDeclaration, ctx.semantic.ProviderInput.Catalog["local"])
	if err != nil {
		t.Fatal(err)
	}
	source := localModels.ByID["source-model-one"]
	alias := localModels.ByID["alias-one"]
	if source.Facts.Vendor != "openai" || source.Facts.API != "openai-responses" || source.Facts.Context != 100000 {
		t.Fatalf("typed source model did not retain actual winning catalog facts: %#v", source)
	}
	if alias.Facts.Vendor != source.Facts.Vendor || alias.Facts.API != source.Facts.API || alias.Facts.Context != source.Facts.Context || alias.Facts.Base != "source-model-one" {
		t.Fatalf("typed alias did not inherit the winning source model's facts and base: source=%#v alias=%#v", source, alias)
	}
	otherModels, err := ComposeProviderModels(otherDeclaration, ctx.semantic.ProviderInput.Catalog["other"])
	if err != nil {
		t.Fatal(err)
	}
	otherModel := otherModels.ByID["source-model-two"]
	if otherModel.Facts.Vendor != "anthropic" || otherModel.Facts.API != "anthropic" || otherModel.Facts.Context != 200000 {
		t.Fatalf("second declaration's own-glob catalog facts were not typed: %#v", otherModel)
	}
	if packload.EmbeddedLoaded() {
		t.Fatal("private same-default producer inspection materialized embedded packs")
	}
	if after := explicitEditTreeSnapshot(t, roots...); after != before {
		t.Fatal("private same-default producer inspection changed observed file trees")
	}
	for _, pass := range []string{"cold", "warm"} {
		result, err := ValidateExplicitEdit(workspace, target, input)
		if err != nil {
			t.Fatalf("%s public validation did not consume the planned local pack and installed canonical catalog: %v", pass, err)
		}
		if _, ok := result.SnapshotJSON(); !ok {
			t.Fatalf("%s explicit edit returned no validated snapshot", pass)
		}
		if packload.EmbeddedLoaded() {
			t.Fatalf("%s public validation materialized embedded packs", pass)
		}
		if after := explicitEditTreeSnapshot(t, roots...); after != before {
			t.Fatalf("%s public validation changed the observed file trees", pass)
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
