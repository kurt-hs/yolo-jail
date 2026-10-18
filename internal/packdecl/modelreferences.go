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
// id the composed provider table lists, under the program's own provider id for it; every id the
// program's own model surfaces hold as its derive renders them (Registry); and, for a provider
// the program runs on a list of its own (`built_in_providers`), the ids its installed catalog
// names (`model_catalog`).
//
// Core knows no agent, so the pack says where the references are. Every pointer is an RFC 6901
// JSON Pointer (internal/jsonptr) into the surface's document in which a token that is exactly
// `*` stands for every member of an object or array at that step. A pattern is a string in which
// `*` matches any run of characters and every other character matches itself, ignoring case; a
// pattern with neither a `/` nor a `*` names no provider and is not a model reference.
type ModelReferences struct {
	// Surface is the config surface the references live in, as `<agent>/<name>`.
	Surface string `json:"surface"`
	// Models point at qualified `<provider>/<id>` strings.
	Models []string `json:"models,omitempty"`
	// Patterns point at arrays of patterns over qualified ids.
	Patterns []string `json:"patterns,omitempty"`
	// Scope is the one Patterns pointer whose array admits a child process that runs on the
	// session's own selected model; "" when the program has none. With Selection it is the boot's
	// question "is the model the user picked still inside the scope?" (MM-D46).
	Scope string `json:"scope,omitempty"`
	// Selection points at the surface's selected provider id and bare model id, the pair the
	// program's own model picker writes; nil when the surface holds none.
	Selection *ModelSelectionKeys `json:"selection,omitempty"`
	// ProfileSubagents says the program's derive writes the active profile's `subagents` models
	// (profiles.<name>.subagents) into this surface, resolving each through the provider's list
	// the way the profile's `model` is: an alias of it, else an id of it, else the list's
	// `default` alias in its place, else a refusal of the render. A provider with no list passes
	// the value through. `yolo check` resolves each declared value where the user wrote it.
	ProfileSubagents bool `json:"profile_subagents,omitempty"`
	// Registry are the program's own model surfaces, whose ids join its registry.
	Registry []ModelRegistrySource `json:"registry,omitempty"`
}

// ModelRegistrySource is one surface the program's derive writes model ids into.
type ModelRegistrySource struct {
	// Surface is the surface, as `<agent>/<name>`.
	Surface string `json:"surface"`
	// IDs points at the bare model ids.
	IDs string `json:"ids"`
	// Provider is the program's provider id the ids belong to; "" means the member the pointer's
	// first `*` stands for.
	Provider string `json:"provider,omitempty"`
}

// ModelSelectionKeys are the two pointers of a surface's selected model.
type ModelSelectionKeys struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
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
	for i, src := range r.Registry {
		field := fmt.Sprintf("registry[%d]", i)
		if agent, name, ok := strings.Cut(src.Surface, "/"); !ok || agent == "" || name == "" || strings.Contains(name, "/") {
			problems = append(problems, fmt.Sprintf("%s.%s.surface %q must name a config surface as \"<agent>/<name>\"", label, field, src.Surface))
		}
		pointer(field+".ids", src.IDs)
		if tokens, _ := jsonptr.Parse(src.IDs); src.Provider == "" && !containsToken(tokens, "*") {
			problems = append(problems, fmt.Sprintf("%s.%s needs a \"provider\", or a `*` in \"ids\" that stands for it", label, field))
		}
	}
	for i, p := range r.Models {
		pointer(fmt.Sprintf("models[%d]", i), p)
	}
	for i, p := range r.Patterns {
		pointer(fmt.Sprintf("patterns[%d]", i), p)
	}
	if r.Scope != "" {
		found := false
		for _, p := range r.Patterns {
			found = found || p == r.Scope
		}
		if !found {
			problems = append(problems, fmt.Sprintf("%s.scope %q must be one of its \"patterns\"", label, r.Scope))
		}
	}
	if s := r.Selection; s != nil {
		pointer("selection.provider", s.Provider)
		pointer("selection.model", s.Model)
		if r.Scope == "" {
			problems = append(problems, label+".selection needs a \"scope\" to compare the selected model with")
		}
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

func containsToken(tokens []string, want string) bool {
	for _, t := range tokens {
		if t == want {
			return true
		}
	}
	return false
}
