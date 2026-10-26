package config

// RenameTransform names a conversion operation for later migration code. Descriptors contain
// data only; they do not convert, alias, or activate any authored spelling.
type RenameTransform uint8

const (
	RenameUnspecified RenameTransform = iota
	RenameKey
	RenameMove
	RenameSplit
	RenameValueRewrite
	RenameFoldWhenEqual
	RenameDropDefault
	RenameDropDerived
	RenameDrop
)

// RenameDescriptor describes one old key and its replacement(s) without implementing the
// transformation. Field-sensitive entries intentionally defer value handling to a converter.
type RenameDescriptor struct {
	OldPath      string
	Replacements []string
	Scope        SchemaScope
	Transform    RenameTransform
}

var configRenames = []RenameDescriptor{
	{OldPath: "packages", Replacements: []string{"install.nix"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "mise_tools", Replacements: []string{"install.mise"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "gpu", Replacements: []string{"machine.gpu"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "resources", Replacements: []string{"machine.resources"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "network.ports", Replacements: []string{"machine.ports"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "network.forward_host_ports", Replacements: []string{"machine.forward"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "network.mode", Replacements: []string{"machine.network"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "network", Replacements: []string{"machine.network", "machine.ports", "machine.forward"}, Scope: SchemaScopeBoth, Transform: RenameSplit},
	{OldPath: "env_sources", Replacements: []string{"env.files", "env.set", "env.unset"}, Scope: SchemaScopeBoth, Transform: RenameSplit},
	{OldPath: "brokered", Replacements: []string{"allow.*", "requests.*"}, Scope: SchemaScopeFieldSensitive, Transform: RenameValueRewrite},
	{OldPath: "brokered.*.grants.*", Replacements: []string{"allow.*.always", "requests.*"}, Scope: SchemaScopeFieldSensitive, Transform: RenameValueRewrite},
	{OldPath: "brokered.*.grant_kinds", Replacements: []string{"allow.*.on_request"}, Scope: SchemaScopeUserOnly, Transform: RenameMove},
	{OldPath: "profile", Replacements: []string{"active_provider"}, Scope: SchemaScopeUserOnly, Transform: RenameMove},
	{OldPath: "providers.*.models.*.context_window", Replacements: []string{"providers.*.models.*.context"}, Scope: SchemaScopeBoth, Transform: RenameKey},
	{OldPath: "providers.*.models.*.input", Replacements: []string{"providers.*.models.*.accepts", "providers.*.every_model.accepts"}, Scope: SchemaScopeBoth, Transform: RenameFoldWhenEqual},
	{OldPath: "providers.*.models.*.cost", Replacements: []string{"providers.*.models.*.price"}, Scope: SchemaScopeBoth, Transform: RenameKey},
	{OldPath: "providers.*.models.*.id", Replacements: []string{"providers.*.models.<id>"}, Scope: SchemaScopeBoth, Transform: RenameDropDerived},
	{OldPath: "providers.*.models.*.vendor", Replacements: []string{"providers.*.models.<id>"}, Scope: SchemaScopeBoth, Transform: RenameDropDerived},
	{OldPath: "providers.*.models.*.max_tokens", Replacements: []string{"providers.*.every_model.max_tokens"}, Scope: SchemaScopeBoth, Transform: RenameMove},
	{OldPath: "providers.*.models.*.reasoning", Replacements: []string{"providers.*.every_model.reasoning"}, Scope: SchemaScopeBoth, Transform: RenameMove},

	{OldPath: "loopholes.*.enabled", Replacements: []string{"off"}, Scope: SchemaScopeFieldSensitive, Transform: RenameValueRewrite},
	{OldPath: "loopholes.*.settings.*", Replacements: []string{"allow.*.*"}, Scope: SchemaScopeFieldSensitive, Transform: RenameValueRewrite},
	{OldPath: "host_processes", Replacements: []string{"the host-processes pack"}, Scope: SchemaScopeUserOnly, Transform: RenameDrop},
	{OldPath: "journal", Replacements: []string{"the journal pack"}, Scope: SchemaScopeUserOnly, Transform: RenameDrop},
}

// RenameDescriptors returns a detached copy of the migration metadata in declaration order.
func RenameDescriptors() []RenameDescriptor {
	out := make([]RenameDescriptor, len(configRenames))
	for i, d := range configRenames {
		out[i] = d
		out[i].Replacements = append([]string(nil), d.Replacements...)
	}
	return out
}
