package config

// WorkspaceScopeRestriction describes one existing rule that refuses a workspace-scope
// value. Path uses config-key spellings and `*` for a map key or list index. When is empty
// for an unconditional restriction; otherwise it names the condition the validator checks.
// The description is metadata only: validation remains in the value-specific decider.
type WorkspaceScopeRestriction struct {
	Path string
	When string
}

// workspaceScopeRestrictions is the scope authority shared by validators and applied-fact
// consumers. Keep conditional restrictions beside their deciders; they must not be mistaken
// for whole-key rules.
var workspaceScopeRestrictions = []WorkspaceScopeRestriction{
	{Path: "config.adapters"},
	{Path: "config.agent_updates"},
	{Path: "config.brokered.*.grant_kinds"},
	{Path: "config.brokered.*.sets"},
	{Path: "config.cache_relocations"},
	{Path: "config.host_apply_on_launch"},
	{Path: "config.host_files[*]", When: "entry names a host source (including string shorthand)"},
	{Path: "config.host_floor"},
	{Path: "config.host_import_exclusions"},
	{Path: "config.host_management"},
	{Path: "config.host_path"},
	{Path: "config.host_wrappers"},
	{Path: "config.local_clones"},
	{Path: "config.loopholes.*", When: "entry declares a command (install)"},
	{Path: "config.loopholes.*.doctor_cmd", When: "entry is not backed by a manifest"},
	{Path: "config.loopholes.*.env"},
	{Path: "config.loopholes.*.settings.*", When: "manifest declares the setting user-scope"},
	{Path: "config.macos_user_mode"},
	{Path: "config.macos_user_shared_tmp"},
	{Path: "config.mounts[*]", When: "mount is read-write"},
	{Path: "config.on_request"},
	{Path: "config.packs"},
	{Path: "config.perf_logging"},
	{Path: "config.profile"},
	{Path: "config.profiles"},
	{Path: "config.programs"},
	{Path: "config.promotion_target"},
	{Path: "config.providers", When: "providers value is null"},
	{Path: "config.providers.*", When: "provider value is null"},
	{Path: "config.providers.*.api_key_env_name"},
	{Path: "config.providers.*.endpoints"},
	{Path: "config.providers.*.endpoints.*.base_url"},
	{Path: "config.providers.*.platform"},
	{Path: "config.provisioners"},
	{Path: "config.share_host_fonts"},
	{Path: "config.update_check"},
	{Path: "config.use_profiles"},
}

// WorkspaceScopeRestrictions returns the canonical workspace-scope refusals. The slice is
// copied so callers cannot mutate validation policy.
func WorkspaceScopeRestrictions() []WorkspaceScopeRestriction {
	return append([]WorkspaceScopeRestriction(nil), workspaceScopeRestrictions...)
}

// WorkspaceUserScopeOnlyKey reports keys whose complete top-level value is rejected in
// workspace scope by validation. It is derived from the scope rules so mixed keys do not
// become whole-key restrictions by accident. Keep the public predicate as the validator
// seam for unconditional top-level refusals.
func WorkspaceUserScopeOnlyKey(key string) bool {
	path := "config." + key
	for _, rule := range workspaceScopeRestrictions {
		if rule.Path == path && rule.When == "" {
			return true
		}
	}
	return false
}

// workspaceScopeRestriction applies the canonical rule identity beside the value decider.
// Dynamic map keys belong in diagnostics, never in policy matching.
func workspaceScopeRestriction(rulePath, when string) bool {
	for _, rule := range workspaceScopeRestrictions {
		if rule.Path == rulePath && rule.When == when {
			return true
		}
	}
	return false
}
