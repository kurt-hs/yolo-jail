package config

// RemovalPolicy describes an authored removal spelling. Unspecified is deliberate: legacy
// null values are path- and presence-sensitive, so an unrecorded path must not inherit a
// generic interpretation.
type RemovalPolicy uint8

const (
	RemovalUnspecified RemovalPolicy = iota
	RemovalUnsupported
	RemovalNull
	RemovalEmptyList
	RemovalOffString
	RemovalNativeString
	RemovalTombstone
	RemovalUnsetList
)

// RemovalRule attaches one explicitly known removal convention to an exact schema path.
type RemovalRule struct {
	Path   string
	Policy RemovalPolicy
}

// RemovalPolicies returns only legacy paths whose null/removal behavior is uniform and known.
// It does not convert values or collapse the legacy null/presence distinctions.
func RemovalPolicies() []RemovalRule {
	return []RemovalRule{
		{Path: "model_facts.*", Policy: RemovalNull},
		{Path: "profile", Policy: RemovalNull},
		{Path: "profile.*", Policy: RemovalNull},
		{Path: "providers.*.models.*", Policy: RemovalNull},
		{Path: "providers.*.models.*.*", Policy: RemovalTombstone},
	}
}

// ReadableRemovalPolicies lists settled removal values in the new authored vocabulary.
func ReadableRemovalPolicies() []RemovalRule {
	return []RemovalRule{
		{Path: "active_provider.*", Policy: RemovalNativeString},
		{Path: "allow.*.always", Policy: RemovalEmptyList},
		{Path: "allow.*.on_request", Policy: RemovalEmptyList},
		{Path: "install.nix", Policy: RemovalEmptyList},
		{Path: "mcp_servers.*", Policy: RemovalOffString},
		{Path: "providers.*", Policy: RemovalOffString},
		{Path: "providers.*.variants.*", Policy: RemovalOffString},
		{Path: "requests.git.push", Policy: RemovalEmptyList},
		{Path: "requests.github.repos", Policy: RemovalEmptyList},
		{Path: "env.unset", Policy: RemovalUnsetList},
	}
}

func removalPolicy(path string) RemovalPolicy {
	for _, rule := range RemovalPolicies() {
		if path == rule.Path {
			return rule.Policy
		}
	}
	return RemovalUnspecified
}
