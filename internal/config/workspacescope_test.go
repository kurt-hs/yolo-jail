package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

func TestProviderScopeValidatorUsesRuleIdentityForArbitraryMapKeys(t *testing.T) {
	providers := map[string]any{}
	for _, name := range []string{"project.provider", "bracket[name]", "punctuation-!"} {
		providers[name] = map[string]any{
			"platform":         nil,
			"api_key_env_name": nil,
			"endpoints":        map[string]any{"custom.proto[!]": map[string]any{"base_url": "https://example.invalid"}},
		}
	}
	providers["null.provider[entry]"] = nil
	providers["legal.provider[entry]"] = map[string]any{"models": []string{"model"}}
	body, err := json.Marshal(map[string]any{"providers": providers})
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, WorkspaceConfigName), body, 0o644); err != nil {
		t.Fatal(err)
	}
	var errs []string
	validateProviderCredentialScope(ws, &errs)
	if len(errs) != 10 {
		t.Fatalf("provider scope errors = %d, want 13 (all endpoint, platform, api-key, and null refusals): %v", len(errs), errs)
	}
	joined := strings.Join(errs, "\n")
	for _, name := range []string{"project.provider", "bracket[name]", "punctuation-!", "null.provider[entry]"} {
		if !strings.Contains(joined, name) {
			t.Errorf("scope refusal omitted provider key %q: %v", name, errs)
		}
	}
	if strings.Contains(joined, "legal.provider[entry]") {
		t.Fatalf("legal provider key was refused: %v", errs)
	}
}

func TestNullProvidersScopeValidatorRefusesAtWorkspaceScope(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, WorkspaceConfigName), []byte(`{"providers":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var errs []string
	validateProviderCredentialScope(ws, &errs)
	if len(errs) != 1 || !strings.Contains(errs[0], "config.providers") {
		t.Fatalf("null providers errors = %v, want one existing credential-scope refusal", errs)
	}
}

func TestLoopholeSettingsScopeValidatorUsesRuleIdentityForDynamicKeys(t *testing.T) {
	name, setting := "dotted.pack[name]!", "setting.with[punctuation]"
	spec := jsonx.NewOrderedMap()
	settings := jsonx.NewOrderedMap()
	settings.Set(setting, true)
	spec.Set("settings", settings)
	info := &LoopholeInfo{Name: name, Settings: []loopholedecl.Setting{{Key: setting, Scope: loopholedecl.SettingScopeUser}}}
	got := loopholeSettingsScopeViolations(name, spec, "workspace config", info)
	if len(got) != 1 || !strings.Contains(got[0], "config.loopholes."+name+".settings."+setting) {
		t.Fatalf("loophole setting violations = %v, want one refusal naming dynamic keys", got)
	}
}

func TestWorkspaceScopeFactsShareValidationPredicates(t *testing.T) {
	for _, tc := range []struct {
		key, body string
	}{
		{"profiles", `{"profiles":{}}`},
		{"packs", `{"packs":[]}`},
		{"adapters", `{"adapters":{}}`},
		{"provisioners", `{"provisioners":{}}`},
		{"on_request", `{"on_request":"echo request"}`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if !WorkspaceUserScopeOnlyKey(tc.key) {
				t.Fatalf("%s is not classified user-scope-only", tc.key)
			}
			ws := t.TempDir()
			if err := os.WriteFile(filepath.Join(ws, WorkspaceConfigName), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			var errs []string
			switch tc.key {
			case "profiles":
				validateProfiles(ws, &errs)
			case "packs":
				validatePacks(ws, &errs)
			case "adapters":
				validateAdapters(ws, &errs)
			case "provisioners":
				cfg := jsonx.NewOrderedMap()
				cfg.Set("provisioners", jsonx.NewOrderedMap())
				validateProvisioners(cfg, ws, &errs, &[]string{})
			case "on_request":
				validateOnRequest(jsonx.NewOrderedMap(), ws, &errs)
			}
			if !strings.Contains(strings.Join(errs, "\n"), "user-scope only") {
				t.Fatalf("validator did not refuse workspace %s: %v", tc.key, errs)
			}
		})
	}
}

func TestWorkspaceScopeRestrictionMetadataMatchesWholeKeyPredicate(t *testing.T) {
	wantWhole := map[string]bool{
		"adapters": true, "agent_updates": true, "cache_relocations": true,
		"host_apply_on_launch": true, "host_floor": true, "host_import_exclusions": true,
		"host_management": true, "host_path": true, "host_wrappers": true,
		"local_clones": true, "macos_user_mode": true, "macos_user_shared_tmp": true,
		"on_request": true, "packs": true, "perf_logging": true, "profile": true,
		"profiles": true, "programs": true, "promotion_target": true,
		"provisioners": true, "share_host_fonts": true, "update_check": true,
		"use_profiles": true,
	}
	gotWhole := make(map[string]bool)
	for _, rule := range WorkspaceScopeRestrictions() {
		if strings.HasPrefix(rule.Path, "config.") && !strings.Contains(rule.Path, "*") && rule.When == "" {
			key := strings.TrimPrefix(rule.Path, "config.")
			gotWhole[key] = true
		}
	}
	if len(gotWhole) != len(wantWhole) {
		t.Fatalf("whole-key metadata = %v, want %v", gotWhole, wantWhole)
	}
	for key := range wantWhole {
		if !gotWhole[key] || !WorkspaceUserScopeOnlyKey(key) {
			t.Errorf("whole-key policy %q is missing from metadata or predicate", key)
		}
	}
	for _, key := range []string{"providers", "mounts", "host_files", "loopholes", "writable_home_dirs"} {
		if gotWhole[key] || WorkspaceUserScopeOnlyKey(key) {
			t.Errorf("mixed or unrestricted key %q became a whole-key restriction", key)
		}
	}
}

func TestWorkspaceScopeRestrictionMetadataDescribesConditionalCases(t *testing.T) {
	for _, tc := range []struct {
		path, when string
	}{
		{"config.providers.*.endpoints", ""},
		{"config.brokered.*.sets", ""},
		{"config.brokered.*.grant_kinds", ""},
		{"config.providers.*.endpoints.*.base_url", ""},
		{"config.providers.*", "provider value is null"},
		{"config.providers", "providers value is null"},
		{"config.mounts[*]", "mount is read-write"},
		{"config.host_files[*]", "entry names a host source (including string shorthand)"},
		{"config.loopholes.*.env", ""},
		{"config.loopholes.*.settings.*", "manifest declares the setting user-scope"},
	} {
		if !workspaceScopeRestriction(tc.path, tc.when) {
			t.Errorf("scope metadata does not classify rule %q when %q", tc.path, tc.when)
		}
	}
	if workspaceScopeRestriction("config.mounts[1]", "mount is read-write") ||
		workspaceScopeRestriction("config.providers.local.models", "") {
		t.Error("concrete paths are not canonical scope rule identities")
	}
	copyOfRules := WorkspaceScopeRestrictions()
	copyOfRules[0].Path = "corrupted"
	if !WorkspaceUserScopeOnlyKey("adapters") {
		t.Fatal("mutating returned metadata changed the policy")
	}
}

func TestWorkspaceScopeFactsLeaveMixedKeysUnclassified(t *testing.T) {
	for _, key := range []string{"providers", "mounts", "host_files", "loopholes"} {
		if WorkspaceUserScopeOnlyKey(key) {
			t.Errorf("mixed/value-specific key %q was classified as wholly user-scope-only", key)
		}
	}
}
