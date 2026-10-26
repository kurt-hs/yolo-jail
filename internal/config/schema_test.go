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
		"profiles.*", "profile", "loopholes.*.settings.*", "brokered.*.grants.*",
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
		"prune": knownPruneKeys, "profiles.*": knownProfileKeys,
		"brokered.*": knownBrokeredSourceKeys, "brokered.*.grants.*": knownBrokeredGrantKeys,
		"brokered.*.sets.*": knownBrokeredSetKeys, "programs": knownProgramsKeys,
		"programs.overrides.*": knownProgramOverrideKeys, "host_files.*": knownHostFileKeys,
	} {
		for field := range fields {
			path := prefix + "." + field
			if _, ok := got[path]; !ok {
				t.Errorf("schema census omits nested field %q", path)
			}
		}
	}
	for _, path := range []string{
		"mounts.*", "mounts.*.host", "mounts.*.at", "mounts.*.mode", "mounts.*.optional",
		"programs.autoprune", "programs.overrides.*.path", "programs.overrides.*.prebuilt",
		"programs.overrides.*.run", "profiles.*.model", "profiles.*.*",
		"brokered.*.grants.*.kind", "brokered.*.grants.*.repo", "brokered.*.grants.*.branch",
		"brokered.*.sets.*.name", "brokered.*.sets.*.mode", "brokered.*.sets.*.window",
		"brokered.*.sets.*.max_window", "brokered.*.sets.*.uses", "brokered.*.sets.*.admit",
		"brokered.*.sets.*.admit.*", "host_files.*.path", "host_files.*.source",
		"host_files.*.content", "host_files.*.codec", "host_files.*.managed",
		"host_files.*.defaults", "host_files.*.mode",
	} {
		if _, ok := got[path]; !ok {
			t.Errorf("legacy config field census omits %q", path)
		}
	}
	for _, path := range []string{"providers", "mounts", "loopholes", "brokered", "host_files"} {
		key := got[path]
		if key.Scope != SchemaScopeFieldSensitive {
			t.Errorf("%s scope = %v, want field-sensitive", path, key.Scope)
		}
	}
	for _, path := range []string{"profiles.*.provider", "profiles.*.model", "profile.*", "programs.overrides.*.path"} {
		if got[path].Scope != SchemaScopeUserOnly {
			t.Errorf("user-only root scope not inherited by %s: got %v", path, got[path].Scope)
		}
	}
	if got["providers.*.models.*.cost.cache_read"].Type != SchemaTypeNumber ||
		got["resources.memory"].Type != SchemaTypeString || got["resources.cpus"].Type != SchemaTypeMixed ||
		got["gpu.enabled"].Type != SchemaTypeBoolean || got["profiles.*.model"].Type != SchemaTypeString {
		t.Errorf("legacy nested types are incomplete: cost=%v memory=%v cpus=%v gpu=%v model=%v",
			got["providers.*.models.*.cost.cache_read"].Type, got["resources.memory"].Type,
			got["resources.cpus"].Type, got["gpu.enabled"].Type, got["profiles.*.model"].Type)
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
		if d.Transform != RenameDrop && d.Transform != RenameUnrepresentable && len(d.Replacements) == 0 {
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
	for _, path := range []string{"providers.*.models.*.max_tokens", "providers.*.models.*.reasoning"} {
		if !renameHasTransform(descriptors, path, RenameFoldWhenEqual) {
			t.Errorf("%s must retain per-model values and fold only equal shared facts", path)
		}
	}
	if replacementContains(descriptors, "providers.*.models.*.max_tokens", "providers.*.models.*.max_tokens") == false ||
		replacementContains(descriptors, "providers.*.models.*.max_tokens", "providers.*.every_model.max_tokens") == false {
		t.Error("max_tokens rename must describe both row preservation and optional shared fold")
	}
	if replacementContains(descriptors, "network.mode", "machine.network") {
		t.Error("network.mode has no accepted rename to machine.network")
	}
	if !replacementContains(descriptors, "network.mode", "network.mode") {
		t.Error("unchanged network.mode must remain explicit in the rename contract")
	}
	for _, path := range []string{
		"profiles.*", "profiles.*.provider", "profiles.*.model", "profiles.*.via",
		"profiles.*.enforce_models", "profiles.*.subagents", "profiles.*.*", "profile.*",
		"brokered.*.repos", "brokered.*.grants.*.kind", "brokered.*.grants.*.repo",
		"brokered.*.grants.*.branch", "brokered.*.sets.*",
	} {
		if !seen[path] {
			t.Errorf("provider/profile/broker migration descriptor omits %q", path)
		}
	}
	if !renameHasTransform(descriptors, "profiles.*", RenameProfileVariant) ||
		!renameHasTransform(descriptors, "profile.*", RenameProfileSelection) ||
		!renameHasTransform(descriptors, "brokered.*.sets.*", RenameUnrepresentable) {
		t.Error("provider variants, native/default selections, and unrepresentable grants need explicit transforms")
	}
}

func replacementContains(descriptors []RenameDescriptor, oldPath, replacement string) bool {
	for _, d := range descriptors {
		if d.OldPath != oldPath {
			continue
		}
		for _, got := range d.Replacements {
			if got == replacement {
				return true
			}
		}
	}
	return false
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

func TestReadableSchemaKeysDescribeNewVocabularyWithoutActivatingIt(t *testing.T) {
	got := map[string]SchemaKey{}
	for _, key := range ReadableSchemaKeys() {
		if _, exists := got[key.Path]; exists {
			t.Errorf("duplicate readable path %q", key.Path)
		}
		got[key.Path] = key
	}
	for _, path := range []string{
		"install.nix", "install.mise.*", "machine.gpu", "machine.resources.memory",
		"machine.ports.*.host", "machine.forward.*.jail", "env.files.*", "env.set.*", "env.unset.*",
		"allow.*.always", "allow.*.on_request", "requests.git.push", "requests.github.repos",
		"active_provider.default", "active_provider.*", "providers.*.variants.*.model",
		"providers.*.every_model.accepts", "providers.*.models.*.context", "providers.*.models.*.price.input",
	} {
		if _, ok := got[path]; !ok {
			t.Errorf("readable schema omits %q", path)
		}
	}
	if got["mounts"].Scope != SchemaScopeFieldSensitive || got["host_files.*.source"].Scope != SchemaScopeFieldSensitive ||
		got["programs.overrides.*.path"].Scope != SchemaScopeUserOnly || got["network.mode"].Type != SchemaTypeString {
		t.Errorf("unchanged legacy path metadata was not retained: mounts=%#v source=%#v program=%#v network=%#v",
			got["mounts"], got["host_files.*.source"], got["programs.overrides.*.path"], got["network.mode"])
	}
	if _, ok := got["machine.network"]; ok {
		t.Error("schema invents machine.network instead of retaining network.mode")
	}
	if got["allow.*.always"].Scope != SchemaScopeUserOnly || got["requests.*.repos"].Scope != SchemaScopeWorkspaceOnly ||
		got["providers.*.variants.*.model"].Scope != SchemaScopeUserOnly {
		t.Errorf("readable schema scope contract is incomplete: allow=%v request=%v variant=%v",
			got["allow.*.always"].Scope, got["requests.*.repos"].Scope, got["providers.*.variants.*.model"].Scope)
	}
	for _, path := range []string{
		"network.mode", "mounts.*.host", "host_files.*.source", "programs.overrides.*.path",
		"providers.*.models.*.max_tokens", "providers.*.models.*.reasoning",
		"providers.*.models.*.accepts", "providers.*.models.*.images.max_width",
		"providers.*.models.*.extends", "providers.*.models.*.twin_of",
		"providers.*.every_model.api", "providers.*.variants.*.subagents.default",
	} {
		if _, ok := got[path]; !ok {
			t.Errorf("readable schema omits settled provider field %q", path)
		}
	}
	if got["providers.*.models.*.max_tokens"].Type != SchemaTypeNumber ||
		got["providers.*.models.*.reasoning"].Type != SchemaTypeBoolean ||
		got["providers.*.models.*.accepts"].Type != SchemaTypeList ||
		got["providers.*.models.*.price.input"].Type != SchemaTypeNumber {
		t.Errorf("new provider/model nested types are incomplete: %#v %#v %#v %#v",
			got["providers.*.models.*.max_tokens"], got["providers.*.models.*.reasoning"],
			got["providers.*.models.*.accepts"], got["providers.*.models.*.price.input"])
	}
	if got["providers.*"].Removal != RemovalOffString || got["providers.*.variants.*"].Removal != RemovalOffString ||
		got["providers.*"].Type != SchemaTypeMixed || got["providers.*.variants.*"].Type != SchemaTypeMixed {
		t.Errorf("provider off values must be mixed shapes and explicit removals: provider=%#v variant=%#v", got["providers.*"], got["providers.*.variants.*"])
	}
	if got["install.nix"].Removal != RemovalEmptyList || got["active_provider.*"].Removal != RemovalNativeString ||
		got["mcp_servers.*"].Removal != RemovalOffString {
		t.Errorf("readable removal contract is incomplete: %#v %#v %#v", got["install.nix"], got["active_provider.*"], got["mcp_servers.*"])
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
