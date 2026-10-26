package packdecl

// renames.go answers a manifest field this build refuses because it was RENAMED: the refusal names
// the new spelling and the version it moved at, where an unknown field otherwise sends the author
// to update yolo — the one step that cannot help when the field is older than this build, not
// newer.

import (
	"fmt"
	"strings"
)

// fieldRename is one manifest field that moved to a new spelling with no alias kept: Old is the
// spelling a pack written before Since carries, New is the one this build reads.
type fieldRename struct {
	Old, New, Since string
	Scope           string
}

type scopedRename struct {
	Path   string
	Rename *fieldRename
}

// fieldRenames is every manifest field renamed without an alias, one entry per field, with the
// release line it moved in. A field renamed WITH a decodable retirement (`tier`, which
// retiredFieldProblems answers) needs no entry: it never reaches the unknown-field refusal.
var fieldRenames = []fieldRename{
	// docs/reference/patched-forks.md §3.5: the owner named the key on 2026-10-16, and `carry`
	// is not accepted as an alias.
	{Old: "carry", New: "patch_branch", Since: "0.14.x"},
	// Grouped manifests use the codec's concise overlay/model vocabulary. These are
	// format-specific refusals; legacy JSON keeps its existing spellings.
	{Scope: "overlay", Old: "config", New: "set"},
	{Scope: "provider-model", Old: "id", New: "the model table key"},
	{Scope: "provider-model", Old: "input", New: "accepts"},
	{Scope: "provider-model", Old: "cost", New: "price"},
	{Scope: "provider-model", Old: "context_window", New: "context"},
}

// renamedField is the rename recorded for field, ok false when field was never renamed.
func renamedField(field string) (fieldRename, bool) {
	return renamedFieldInScope("", field)
}

func renamedFieldInScope(scope, field string) (fieldRename, bool) {
	for _, r := range fieldRenames {
		if r.Old == field && r.Scope == scope {
			return r, true
		}
	}
	return fieldRename{}, false
}

func renameMessage(r fieldRename) string {
	if r.Since != "" {
		return fmt.Sprintf("%q was renamed to %q in yolo %s; write %q in its place", r.Old, r.New, r.Since, r.New)
	}
	return fmt.Sprintf("%q is not accepted in grouped TOML; use %q", r.Old, r.New)
}

// renameStep is the next step for a renamed field: write the new spelling. "" for a field that
// was never renamed.
func renameStep(field string) string {
	r, ok := renamedField(field)
	if !ok {
		return ""
	}
	return renameMessage(r)
}

// fieldNextStep is the next step a line about an unknown field names: the rename where the field
// was renamed, else the two a build cannot tell apart (skewNextStep).
func fieldNextStep(field string) string {
	if step := renameStep(field); step != "" {
		return step
	}
	return skewNextStep
}

// unknownFieldName is the field encoding/json's strict decode refused, "" for any other error.
func unknownFieldName(err error) string {
	rest, ok := strings.CutPrefix(err.Error(), "json: unknown field ")
	if !ok {
		return ""
	}
	return strings.Trim(rest, `"`)
}
