package config

// RemovalPolicy describes an existing authored removal spelling. Unspecified is deliberate:
// legacy null values are path- and presence-sensitive, so an unrecorded path must not inherit
// a generic interpretation.
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

// RemovalRule attaches one explicitly known removal convention to an exact legacy schema path.
type RemovalRule struct {
	Path   string
	Policy RemovalPolicy
}

// RemovalPolicies returns only legacy paths whose null/removal behavior is uniform and known.
// It does not project the new TOML vocabulary or turn a removal convention into a parser rule.
func RemovalPolicies() []RemovalRule {
	return []RemovalRule{
		{Path: "model_facts.*", Policy: RemovalNull},
		{Path: "profile", Policy: RemovalNull},
		{Path: "profile.*", Policy: RemovalNull},
		{Path: "providers.*.models.*", Policy: RemovalNull},
		{Path: "providers.*.models.*.*", Policy: RemovalTombstone},
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
