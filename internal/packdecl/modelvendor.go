package packdecl

// ValidModelVendor reports whether s is shaped like a model's VENDOR: its maker, a term
// coined in docs/design/bedrock-plumbing.md (OQ-BR9) for the fact a provider that serves
// several makers' models declares per entry, so each agent's derive can offer only the
// models its own client can call (claude's Bedrock client serves Anthropic models alone).
//
// One lowercase token of letters, digits, `.`, `_` and `-`, starting with a letter or digit:
// "anthropic", "openai", "moonshotai". Only the SHAPE is core's. The vocabulary is open, core
// interprets no value, and a value no derive asks about makes its entry one no filtering agent
// offers. Lowercase because derives compare it by equality, so "OpenAI" beside "openai" would
// read as two makers.
//
// Read by two layers, declared once: a pack's `model_options.<alias>.vendor`
// (validateContribution) and a user's object-form `providers.<name>.models.<alias>.vendor`
// (config.validateModelEntry).
func ValidModelVendor(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '.' || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

// ModelFacts is a provider's `model_options`: per alias, the flat string facts its derives
// read (Contribution.ModelOptions). A nil value is a TOMBSTONE, the fact written as JSON null
// (docs/design/model-lists-and-pickers.md ML-D10): the model has no such fact, where an absent
// key inherits it from the layer below.
type ModelFacts map[string]map[string]*string

// Strings is m's stated facts as plain strings, every tombstone left out: the view of a reader
// that only asks what a pack declares.
func (m ModelFacts) Strings() map[string]map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(m))
	for alias, facts := range m {
		row := make(map[string]string, len(facts))
		for k, v := range facts {
			if v != nil {
				row[k] = *v
			}
		}
		out[alias] = row
	}
	return out
}
