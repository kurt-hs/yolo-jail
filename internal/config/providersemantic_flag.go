package config

// ParseActiveProviderFlag is the readable-provider counterpart to the legacy profile -p parser.
// Both accept the established -p receiver grammar; the old ParseProfileFlag remains untouched for
// legacy profile resolution.
func ParseActiveProviderFlag(value string) (ActiveProviderSelection, error) {
	bare, named, err := ParseProfileFlag(value)
	if err != nil {
		return ActiveProviderSelection{}, err
	}
	if named == nil {
		return ActiveProviderSelection{Default: append([]string(nil), bare...)}, nil
	}
	copied := make(map[string][]string, len(named))
	for receiver, targets := range named {
		copied[receiver] = append([]string(nil), targets...)
	}
	return ActiveProviderSelection{Named: copied}, nil
}
