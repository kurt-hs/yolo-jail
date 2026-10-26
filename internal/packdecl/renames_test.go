package packdecl

// renames_test.go pins renames.go: a manifest field this build renamed is answered with its new
// spelling and the version it moved at, on the authoring read (Decode) and on both use reads
// (DecodeTolerant in a jail, DecodeForUse on the host), instead of the "update yolo" step an
// unknown field gets — the one step that cannot help a field OLDER than this build.

import (
	"strings"
	"testing"
)

const carryManifest = `{"name":"p","contributes":[{"kind":"files","into":"~/.pi/x","from":"x",` +
	`"carry":"git+https://example.com/r?ref=b"}]}`

const carryStep = `"carry" was renamed to "patch_branch" in yolo 0.14.x; write "patch_branch" in its place`

func TestARenamedFieldNamesItsNewSpelling(t *testing.T) {
	_, problems := Decode([]byte(carryManifest))
	if len(problems) != 1 || !strings.Contains(problems[0], `unknown field "carry"`) ||
		!strings.Contains(problems[0], carryStep) || strings.Contains(problems[0], "update yolo") {
		t.Errorf("Decode: want one refusal naming the rename and not `update yolo`, got %q", problems)
	}

	for name, decode := range map[string]func([]byte) (*Manifest, []string, []string){
		"DecodeTolerant": DecodeTolerant,
		"DecodeForUse":   DecodeForUse,
	} {
		_, problems, skipped := decode([]byte(carryManifest))
		if len(problems) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0], carryStep) ||
			strings.Contains(skipped[0], "update yolo") {
			t.Errorf("%s: want one skip naming the rename, got problems=%q skipped=%q", name, problems, skipped)
		}
	}
}

// A field never renamed keeps both next steps, since this build cannot tell a typo from a field
// a newer yolo reads.
func TestAnUnknownFieldStillNamesBothSteps(t *testing.T) {
	_, problems := Decode([]byte(strings.Replace(carryManifest, `"carry"`, `"carri"`, 1)))
	if len(problems) != 1 || !strings.Contains(problems[0], "check its spelling") ||
		!strings.Contains(problems[0], "update yolo") {
		t.Errorf("Decode: want the spelling-or-update hint, got %q", problems)
	}
}

// Every entry renames to a field this build reads, so the step it names is one that works.
func TestEveryRenameNamesAFieldThisBuildReads(t *testing.T) {
	for _, r := range fieldRenames {
		if r.Scope != "" {
			continue
		}
		manifest := strings.Replace(carryManifest, `"carry"`, `"`+r.New+`"`, 1)
		if _, problems := Decode([]byte(manifest)); len(problems) > 0 &&
			strings.Contains(strings.Join(problems, "\n"), "unknown field") {
			t.Errorf("rename %q -> %q: this build does not read %q: %q", r.Old, r.New, r.New, problems)
		}
		if r.Since == "" {
			t.Errorf("rename %q -> %q names no version", r.Old, r.New)
		}
	}
}
