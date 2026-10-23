package packdecl

// programneed.go is the one predicate pair that splits the `program` kind in two
// (docs/design/provisioner-sets.md OQ-PS11 and OQ-PS5, ruled 2026-10-23): an entry WITH a
// recipe is installed by yolo, and one WITHOUT is a NEED that another selected pack's recipe
// satisfies or that must already be on PATH. Every reader asks these two methods rather
// than spelling `c.Via == ""` itself, so the line between the shapes is drawn in one place.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Provides reports whether c is a `program` entry carrying a RECIPE — a `via` — and so one
// yolo installs and owns a launcher for. A recipe whose via this build does not know never
// reaches a reader: the strict path refuses it and the tolerant path skips it.
func (c Contribution) Provides() bool {
	return c.Kind == KindProgram && c.Via != ""
}

// IsNeed reports whether c is a `program` entry with NO recipe: a name the pack needs on PATH
// and installs nothing for. What the retired `requires` kind declared.
func (c Contribution) IsNeed() bool {
	return c.Kind == KindProgram && c.Via == ""
}

// needFields are the only fields a need takes, by JSON name: what `requires` took. Every other
// field is a recipe's (via, package, url, update, …) or an agent's (protocols, model_menu, …),
// and with no recipe there is nothing installed for any consumer to read it against.
//
// `profile` is skipped rather than allowed: it is refused on every `program`, recipe or not, by
// its own kind-wide check, and naming it twice would be one mistake reported as two.
var needFields = map[string]bool{"kind": true, "bin": true, "install_hints": true, "profile": true}

// needFieldProblems refuses every field a need does not take, naming each by its manifest
// spelling and the one fix. Reflection over Contribution rather than a hand list so a field
// added tomorrow is refused on a need by default: the failure that list would invite is a
// recipe field silently ignored on an entry whose author meant it to install something.
func needFieldProblems(label string, c Contribution) []string {
	var extra []string
	v := reflect.ValueOf(c)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" || needFields[name] {
			continue
		}
		if !v.Field(i).IsZero() {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	var problems []string
	for _, name := range extra {
		problems = append(problems, fmt.Sprintf(
			"%s: a program with no \"via\" does not take %q — with no recipe it is a NEED, "+
				"which takes only \"bin\" and \"install_hints\". Add a \"via\" (%s) if this "+
				"pack installs %s, or delete the field if another selected pack provides it or "+
				"it is already on PATH", label, name, viaList(), c.Bin))
	}
	return problems
}
