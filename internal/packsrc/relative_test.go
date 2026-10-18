package packsrc

import (
	"strings"
	"testing"
)

// THE RELATIVE LOCATOR's grammar (relative.go): a path starting `.`, with a mandatory ?ref= whose `..` is
// still refused, and no //subdirectory; a host name with no scheme is never read as a directory.
func TestParseFromReadsTheRelativeLocatorsGrammar(t *testing.T) {
	a, err := ParseFrom("../..?ref=carry/pi-subagents", "")
	if err != nil || a.Kind != KindGit || a.Ref != "carry/pi-subagents" || a.Repo != "" {
		t.Fatalf("ParseFrom of a relative locator with no directory = %+v, %v", a, err)
	}
	for raw, want := range map[string]string{
		"../..":                   "missing required ?ref=",
		"../..?ref=a..b":          "must not contain '..'",
		"../..//sub?ref=main":     "no //subdirectory",
		"github.com/you/pi?ref=x": "relative to the pack",
		"file:///srv/pi?ref=mine": "git+file:///srv/pi?ref=mine",
	} {
		if _, err := ParseFrom(raw, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseFrom(%q) = %v, want an error containing %q", raw, err, want)
		}
	}
	if _, err := Parse("../..?ref=main"); err == nil {
		t.Error("Parse, which reads `packs` entries, accepted a relative locator")
	}
}
