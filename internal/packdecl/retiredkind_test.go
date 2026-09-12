package packdecl

import (
	"strings"
	"testing"
)

// A RETIRED KIND FAILS LOUDLY AND NAMES ITS REPLACEMENT — it does not fall through to the
// generic "unknown kind (expected one of …)".
//
// This is the repo's established answer for vocabulary that was real last release and is not
// now: `internal/config/validate.go` does it for the removed `journal` and `host_processes`
// config keys, and retiredFieldProblems does it for removed contribution FIELDS. The generic
// unknown-kind message is right for a typo and wrong here, because it reads identically
// whether the kind never existed or was deliberately taken away, and it tells an author
// nothing about what to write instead.
func TestTheRetiredLaunchKindRefusesWithItsReplacement(t *testing.T) {
	_, problems := Decode([]byte(
		`{"name":"x","contributes":[{"kind":"launch","bin":"copilot","flags":["--yolo"]}]}`))
	if len(problems) == 0 {
		t.Fatal("`kind: \"launch\"` was accepted — the kind is retired and a manifest still " +
			"declaring it must hear so, or its flags silently stop being injected")
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"launch", "REMOVED", "autonomy", "autonomous"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the refusal does not contain %q — it must name the kind and the "+
				"replacement:\n%s", want, joined)
		}
	}
	// Not the generic diagnostic, which would send the author looking for a misspelling.
	if strings.Contains(joined, "expected one of") {
		t.Errorf("a retired kind got the UNKNOWN-kind message, which says nothing about the "+
			"migration:\n%s", joined)
	}
	// And it is genuinely out of the closed set, so nothing renders it.
	if KnownKind("launch") {
		t.Error("`launch` is still a known kind, so the refusal above is the only thing gone")
	}
}

// THE JAIL STILL BOOTS. The authoring path refuses; the version boundary SKIPS — the
// asymmetry the `tier` incident bought and packdecl.go's DecodeTolerant doc records ("an
// author must hear; a jail must boot"). A staged tree carrying a manifest this build's
// entrypoint reads must not take the jail down over a kind that left the vocabulary.
//
// The note must also not repeat the unknown-kind promise that "a build that knows the kind
// will render it": no build will, and a reader sent looking for a newer yolo is worse off
// than one told the truth.
func TestARetiredKindIsSkippedNotRefusedAcrossTheVersionBoundary(t *testing.T) {
	m, problems, skipped := DecodeTolerant([]byte(`{"name":"x","contributes":[
		{"kind":"launch","bin":"copilot","flags":["--yolo"]},
		{"kind":"skills","from":"skills","into":".x/skills"}]}`))
	if len(problems) != 0 {
		t.Fatalf("a retired kind became a PROBLEM on the tolerant path, and the boot treats "+
			"every problem as fatal: %v", problems)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "retired") ||
		!strings.Contains(skipped[0], "autonomy") {
		t.Errorf("the skip note must say the kind is retired and name the replacement: %v", skipped)
	}
	if strings.Contains(strings.Join(skipped, "\n"), "a build that knows the kind will render it") {
		t.Errorf("the skip note promises a future build will render a kind that is gone: %v", skipped)
	}
	if cs := m.Contributions(); len(cs) != 1 || cs[0].Kind != KindSkills {
		t.Errorf("the sibling contribution must survive the skip: %+v", cs)
	}
}
