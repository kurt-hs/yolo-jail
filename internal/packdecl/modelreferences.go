package packdecl

import (
	"fmt"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
)

// ModelReferences is the optional `references` member of one program's `model_list_check` fact:
// where that program's own config surface names models, so `yolo check` and the jail's boot can
// resolve each against the program's MODEL REGISTRY and warn about one that matches nothing
// (docs/design/model-lists-and-pickers.md MM-D45).
//
// Two terms are coined for it. A MODEL REFERENCE is one value in the surface that names a model
// a later process will look up: a qualified `<provider>/<id>` string, or a pattern over such
// strings. A program's MODEL REGISTRY is what that program can resolve one against: every model
// id the composed provider table lists, under the program's own provider id for it, plus, for a
// provider the program runs on a list of its own (`built_in_providers`), the ids its installed
// catalog names (`model_catalog`).
//
// Core knows no agent, so the pack says where the references are. Every pointer is an RFC 6901
// JSON Pointer (internal/jsonptr) into the surface's document in which a token that is exactly
// `*` stands for every member of an object or array at that step. A pattern is a string in which
// `*` matches any run of characters and every other character matches itself.
type ModelReferences struct {
	// Surface is the config surface the references live in, as `<agent>/<name>`.
	Surface string `json:"surface"`
	// Models point at qualified `<provider>/<id>` strings.
	Models []string `json:"models,omitempty"`
	// Patterns point at arrays of patterns over qualified ids.
	Patterns []string `json:"patterns,omitempty"`
	// ProfileSubagents says the program's derive writes the active profile's `subagents` models
	// (profiles.<name>.subagents) into this surface, so `yolo check` resolves each declared value
	// where the user wrote it rather than only where the derive put it.
	ProfileSubagents bool `json:"profile_subagents,omitempty"`
}

// modelReferencesProblems validates one `references` declaration.
func modelReferencesProblems(label string, r *ModelReferences) []string {
	label += ".references"
	var problems []string
	if agent, name, ok := strings.Cut(r.Surface, "/"); !ok || agent == "" || name == "" || strings.Contains(name, "/") {
		problems = append(problems, fmt.Sprintf("%s.surface %q must name a config surface as \"<agent>/<name>\"", label, r.Surface))
	}
	if len(r.Models) == 0 && len(r.Patterns) == 0 && !r.ProfileSubagents {
		problems = append(problems, label+": declares nothing to check; name \"models\", \"patterns\" or \"profile_subagents\"")
	}
	pointer := func(field, p string) {
		if tokens, err := jsonptr.Parse(p); err != nil || len(tokens) == 0 {
			problems = append(problems, fmt.Sprintf("%s.%s %q must be a JSON Pointer to a key below the document's root", label, field, p))
		}
	}
	for i, p := range r.Models {
		pointer(fmt.Sprintf("models[%d]", i), p)
	}
	for i, p := range r.Patterns {
		pointer(fmt.Sprintf("patterns[%d]", i), p)
	}
	return problems
}

// ProgramModelReferences is the `references` member of bin's model_list_check fact, nil when the
// pack declares none for it.
func (m *Manifest) ProgramModelReferences(bin string) *ModelReferences {
	check, ok := m.ProgramModelListCheck(bin)
	if !ok || check == nil {
		return nil
	}
	return check.References
}
