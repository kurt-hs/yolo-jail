package packdecl

import (
	"strings"
	"testing"
)

// A `references` declaration decodes beside the rules and is read through
// ProgramModelReferences; each malformed member is refused by name (MM-D45).
func TestModelListCheckReferencesDecodeAndValidate(t *testing.T) {
	m, problems := Decode([]byte(modelListCheckManifest(`{"references":{"surface":"synthetic/settings",
	  "models":["/sub/default"],"patterns":["/sub/scope","/sub/roles/*/allow"],"scope":"/sub/scope",
	  "selection":{"provider":"/provider","model":"/model"},"profile_subagents":true}}`)))
	if len(problems) != 0 {
		t.Fatalf("valid references refused: %v", problems)
	}
	refs := m.ProgramModelReferences("synthetic")
	if refs == nil || refs.Scope != "/sub/scope" || refs.Selection == nil || !refs.ProfileSubagents || len(refs.Patterns) != 2 {
		t.Fatalf("ProgramModelReferences() = %+v, want the whole declaration", refs)
	}
	for _, tc := range []struct{ name, refs, want string }{
		{"bad surface", `{"surface":"settings","models":["/a"]}`, `must name a config surface`},
		{"nothing to check", `{"surface":"a/b"}`, `declares nothing to check`},
		{"root pointer", `{"surface":"a/b","models":[""]}`, `models[0] "" must be a JSON Pointer`},
		{"bad pointer", `{"surface":"a/b","patterns":["x"]}`, `patterns[0] "x" must be a JSON Pointer`},
		{"scope not a pattern", `{"surface":"a/b","patterns":["/p"],"scope":"/q"}`, `must be one of its "patterns"`},
		{"selection without scope", `{"surface":"a/b","models":["/a"],"selection":{"provider":"/p","model":"/m"}}`,
			`needs a "scope"`},
		{"unknown member", `{"surface":"a/b","models":["/a"],"future":1}`, `unknown field`},
		{"registry with no provider", `{"surface":"a/b","models":["/a"],"registry":[{"surface":"a/m","ids":"/models/0/id"}]}`,
			`needs a "provider", or a`},
		{"registry bad surface", `{"surface":"a/b","models":["/a"],"registry":[{"surface":"m","ids":"/*/id"}]}`,
			`registry[0].surface "m" must name`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := Decode([]byte(modelListCheckManifest(`{"references":` + tc.refs + `}`)))
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems = %v, want one containing %q", problems, tc.want)
			}
		})
	}
}
