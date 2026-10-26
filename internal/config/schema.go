package config

import (
	"sort"
	"strings"
)

// SchemaScope describes which authored config scope may contain a key. It is descriptive
// metadata only; validation remains the authority for mixed and value-sensitive keys.
type SchemaScope uint8

const (
	SchemaScopeBoth SchemaScope = iota + 1
	SchemaScopeUserOnly
	SchemaScopeWorkspaceOnly
	SchemaScopeFieldSensitive
)

// SchemaType is the existing schema's value-shape category. It is not a TOML decoder.
type SchemaType uint8

const (
	SchemaTypeAny SchemaType = iota + 1
	SchemaTypeObject
	SchemaTypeString
	SchemaTypeBoolean
	SchemaTypeNumber
	SchemaTypeList
	SchemaTypeObjectOrList
	SchemaTypeMixed
)

// SchemaKey is one path in the legacy live-and-retired config field census. Wildcard
// components (for example providers.*) represent named entries, not literal key names.
type SchemaKey struct {
	Path    string
	Scope   SchemaScope
	Type    SchemaType
	Removal RemovalPolicy
}

// SchemaKeys returns the deterministic path census used as the schema foundation for
// format conversion. It describes the extant JSONC vocabulary; it does not enable TOML reads.
func SchemaKeys() []SchemaKey {
	paths := map[string]struct{}{}
	add := func(path string) { paths[path] = struct{}{} }
	for path := range knownTopLevelConfigKeys {
		add(path)
	}
	addSet := func(prefix string, keys map[string]struct{}) {
		for key := range keys {
			add(prefix + "." + key)
		}
	}
	for prefix, keys := range map[string]map[string]struct{}{
		"network": knownNetworkKeys, "security": knownSecurityKeys,
		"security.blocked_tools.*": knownBlockedToolKeys,
		"packages.*":               knownPackageKeys, "lsp_servers.*": knownLSPServerKeys,
		"mcp_servers.*": knownMCPServerKeys, "providers.*": knownProviderKeys,
		"providers.*.endpoints.*":   knownEndpointKeys,
		"providers.*.models.*":      knownModelKeys,
		"providers.*.models.*.cost": knownModelCostKeys,
		"model_facts.*":             knownSharedModelKeys, "devices.*": knownDeviceKeys,
		"resources": knownResourcesKeys, "loopholes.*": knownHostServiceKeys,
		"loopholes.*.settings": knownLoopholeOverrideKeys,
		"gpu":                  knownGPUKeys, "prune": knownPruneKeys,
	} {
		addSet(prefix, keys)
	}
	for _, path := range []string{
		"mounts.*", "ports.*", "network.ports.*", "network.forward_host_ports.*",
		"env_sources.*", "env_sources.*.*", "mise_tools.*", "lsp_servers.*.args.*",
		"mcp_servers.*.args.*", "mcp_servers.*.env.*", "providers.*.options.*",
		"providers.*.models.*.input.*", "providers.*.models.*", "mcp_servers.*",
		"profiles.*.options.*", "profiles.*.subagents.*",
		"profiles.*.provider", "profiles.*.via", "profiles.*.enforce_models",
		"profiles.*.subagents.default", "profiles.*.subagents.roles.*",
		"profile.*", "profile.*.*", "brokered.*.grants.*", "brokered.*.grants.*.*",
		"brokered.*.repos.*", "loopholes.*.settings.*", "loopholes.*.env.*",
		"host_files.*", "host_wrappers.*", "programs.*", "programs.overrides.*",
		"cache_relocations.*", "writable_home_dirs.*", "include_if_found.*",
		"required_capabilities.*", "adapters.*", "local_clones.*", "host_import_exclusions.*",
	} {
		add(path)
	}
	out := make([]SchemaKey, 0, len(paths))
	for path := range paths {
		scope := SchemaScopeBoth
		switch {
		case WorkspaceUserScopeOnlyKey(path):
			scope = SchemaScopeUserOnly
		case fieldSensitivePath(path):
			scope = SchemaScopeFieldSensitive
		}
		out = append(out, SchemaKey{Path: path, Scope: scope, Type: schemaType(path), Removal: removalPolicy(path)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func fieldSensitivePath(path string) bool {
	for _, root := range []string{"providers", "mounts", "loopholes", "brokered", "host_files"} {
		if path == root || strings.HasPrefix(path, root+".") {
			return true
		}
	}
	return false
}

func schemaType(path string) SchemaType {
	switch path {
	case "runtime", "confinement", "repo_path", "host_path", "macos_user_mode",
		"providers.*.region", "providers.*.base_url", "providers.*.wire_api",
		"providers.*.endpoints.*.base_url", "providers.*.endpoints.*.wire_api",
		"providers.*.models.*.id", "providers.*.models.*.name", "providers.*.models.*.description",
		"providers.*.models.*.vendor", "providers.*.models.*.base", "providers.*.models.*.wire_id":
		return SchemaTypeString
	case "packages", "mounts", "env_sources", "include_if_found", "writable_home_dirs", "required_capabilities",
		"providers.*.capabilities", "providers.*.models.*.input", "providers.*.models.*.input.*":
		return SchemaTypeList
	case "packs", "providers.*.api_key_env_name", "profile", "profile.*", "profile.*.*", "providers.*.models.*":
		return SchemaTypeMixed
	case "network", "security", "providers", "profiles", "loopholes", "brokered", "host_files", "resources", "gpu",
		"providers.*.endpoints", "providers.*.models", "providers.*.options", "providers.*.models.*.cost",
		"providers.*.models.*.openrouter_routing", "model_facts", "model_facts.*", "profiles.*",
		"profiles.*.options", "profiles.*.subagents", "loopholes.*.settings":
		return SchemaTypeObject
	case "providers.*.models.*.reasoning":
		return SchemaTypeBoolean
	case "providers.*.models.*.context_window", "providers.*.models.*.max_tokens",
		"providers.*.models.*.cost.*":
		return SchemaTypeNumber
	default:
		return SchemaTypeAny
	}
}
