package config

import (
	"strings"
	"testing"
)

func TestSchemaKeysCensusEveryLegacyLiveAndNestedField(t *testing.T) {
	keys := SchemaKeys()
	got := map[string]SchemaKey{}
	for _, key := range keys {
		if key.Path == "" {
			t.Fatal("schema key has empty path")
		}
		if _, exists := got[key.Path]; exists {
			t.Errorf("schema path %q appears more than once", key.Path)
		}
		got[key.Path] = key
	}
	for _, path := range []string{
		"runtime", "packages", "mounts", "network", "network.mode", "network.ports",
		"resources.memory", "providers.*.endpoints.*.base_url", "providers.*.models.*.id",
		"providers.*.models.*.cost.cache_write", "model_facts.*.openrouter_routing",
		"profiles.*.options.*", "profile", "loopholes.*.settings.*", "brokered.*.grants.*",
		"host_files.*", "env_sources.*", "programs.overrides.*",
	} {
		if _, ok := got[path]; !ok {
			t.Errorf("legacy schema census omits %q", path)
		}
	}
	for path := range knownTopLevelConfigKeys {
		if _, ok := got[path]; !ok {
			t.Errorf("schema census omits top-level field %q", path)
		}
	}
	for prefix, fields := range map[string]map[string]struct{}{
		"network": knownNetworkKeys, "security": knownSecurityKeys,
		"security.blocked_tools.*": knownBlockedToolKeys, "packages.*": knownPackageKeys,
		"lsp_servers.*": knownLSPServerKeys, "mcp_servers.*": knownMCPServerKeys,
		"providers.*": knownProviderKeys, "providers.*.endpoints.*": knownEndpointKeys,
		"providers.*.models.*": knownModelKeys, "providers.*.models.*.cost": knownModelCostKeys,
		"model_facts.*": knownSharedModelKeys, "devices.*": knownDeviceKeys,
		"resources": knownResourcesKeys, "loopholes.*": knownHostServiceKeys,
		"loopholes.*.settings": knownLoopholeOverrideKeys, "gpu": knownGPUKeys,
		"prune": knownPruneKeys,
	} {
		for field := range fields {
			path := prefix + "." + field
			if _, ok := got[path]; !ok {
				t.Errorf("schema census omits nested field %q", path)
			}
		}
	}
	for _, path := range []string{"providers", "mounts", "loopholes", "brokered", "host_files"} {
		key := got[path]
		if key.Scope != SchemaScopeFieldSensitive {
			t.Errorf("%s scope = %v, want field-sensitive", path, key.Scope)
		}
	}
	if got["packs"].Scope != SchemaScopeUserOnly || got["runtime"].Scope != SchemaScopeBoth {
		t.Errorf("scope census lost whole-key distinctions: packs=%v runtime=%v", got["packs"].Scope, got["runtime"].Scope)
	}
}

func TestRenameDescriptorsHaveScopeAndReplacement(t *testing.T) {
	descriptors := RenameDescriptors()
	seen := map[string]bool{}
	for _, d := range descriptors {
		if d.OldPath == "" || seen[d.OldPath] {
			t.Errorf("empty or duplicate old path in rename table: %q", d.OldPath)
		}
		seen[d.OldPath] = true
		if d.Transform == RenameUnspecified {
			t.Errorf("%s has no transformation", d.OldPath)
		}
		if d.Transform != RenameDrop && len(d.Replacements) == 0 {
			t.Errorf("%s has no replacement", d.OldPath)
		}
		if d.Scope == SchemaScopeFieldSensitive && d.Transform == RenameUnspecified {
			t.Errorf("%s omits its field-sensitive transformation", d.OldPath)
		}
		for _, replacement := range d.Replacements {
			if strings.TrimSpace(replacement) == "" {
				t.Errorf("%s has empty replacement", d.OldPath)
			}
		}
	}
	for _, path := range []string{"packages", "mise_tools", "profile", "providers.*.models.*.context_window", "brokered.*.grants.*", "loopholes.*.enabled"} {
		if !seen[path] {
			t.Errorf("rename table omits legacy path %q", path)
		}
	}
	if !renameHasTransform(descriptors, "providers.*.models.*.input", RenameFoldWhenEqual) {
		t.Error("shared model facts must be folded only when their values are equal")
	}
}

func renameHasTransform(descriptors []RenameDescriptor, path string, transform RenameTransform) bool {
	for _, d := range descriptors {
		if d.OldPath == path && d.Transform == transform {
			return true
		}
	}
	return false
}

func TestRemovalPoliciesKeepLegacyNullCasesPathSpecific(t *testing.T) {
	policies := map[string]RemovalPolicy{}
	for _, rule := range RemovalPolicies() {
		if _, exists := policies[rule.Path]; exists {
			t.Fatalf("duplicate removal policy for %q", rule.Path)
		}
		policies[rule.Path] = rule.Policy
	}
	if policies["profile"] != RemovalNull || policies["profile.*"] != RemovalNull {
		t.Errorf("profile null removal semantics missing: %#v", policies)
	}
	if policies["providers.*.models.*.*"] != RemovalTombstone || policies["providers.*.models.*"] != RemovalNull {
		t.Errorf("model row/tombstone distinction missing: %#v", policies)
	}
	if policies["env_sources"] != RemovalUnspecified {
		t.Errorf("mixed legacy env_sources null semantics were flattened: %v", policies["env_sources"])
	}
}

func TestReadableSchemaScopeAdapterIsNotWholeKeyAuthorization(t *testing.T) {
	for _, key := range []string{"providers", "mounts", "loopholes", "brokered", "host_files"} {
		if WorkspaceUserScopeOnlyKey(key) {
			t.Errorf("mixed key %q was authorized as wholly user-only", key)
		}
	}
	if !WorkspaceUserScopeOnlyKey("packs") || WorkspaceUserScopeOnlyKey("runtime") {
		t.Fatal("whole-key scope adapter did not retain its narrow result")
	}
}
