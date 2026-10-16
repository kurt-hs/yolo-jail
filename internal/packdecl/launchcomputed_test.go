package packdecl

// launchcomputed_test.go pins the pack-wide `launch_computed` declaration
// (Manifest.LaunchComputedByBin; docs/reference/pack-system.md#launch_computed): projected onto the
// owning program's LaunchSelection.Computed as a copy, never onto a fork's, and refused wherever no
// launch could hand it. Through Decode and the real validator.

import (
	"reflect"
	"strings"
	"testing"
)

const computedProgram = `{"kind":"program","bin":"tool","via":"npm","package":"tool","launch_selection":
  {"surface":".t/settings.json","flags":[{"key":"defaultModel","argv":["--model","{value}"]}]}}`

func TestLaunchComputedIsProjectedOntoItsProgramsSelection(t *testing.T) {
	m, problems := Decode([]byte(`{"launch_computed":{"tool":[
	  {"key":"subagents","argv":["--subagents","{value}"],"opt_in":"T_ON"}]},
	  "contributes":[` + computedProgram + `]}`))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	installs := m.InstallContributions()
	want := []LaunchComputedFlag{{Key: "subagents", Argv: []string{"--subagents", "{value}"}, OptIn: "T_ON"}}
	if len(installs) != 1 || !reflect.DeepEqual(installs[0].LaunchSelection.Computed, want) {
		t.Fatalf("projected %+v, want %+v", installs[0].LaunchSelection, want)
	}
	if w := want[0].Word(); w != "--subagents" {
		t.Errorf("Word() = %q", w)
	}
	installs[0].LaunchSelection.Computed[0].Argv[0] = "edited"
	if again := m.InstallContributions(); again[0].LaunchSelection.Computed[0].Argv[0] != "--subagents" {
		t.Error("the projection aliases the manifest")
	}
}

// forkOfTool is a fork of pack base's program tool, which declares no launch selection of its own.
const forkOfTool = `{"kind":"program","bin":"tool","via":"source","fork_of":"base",
  "source":"git+https://example.test/tool.git?ref=main",
  "build":"make","produces":[".local/bin/tool"]}`

// A FORK'S PACK MAY DECLARE launch_computed FOR THE BIN IT FORKS (pack-system.md#LC-D1): its own
// validation checks the entries' shape alone, since the launch selection they ride is the base's,
// and ForkLaunchComputedProblems makes the rest against the base's selection.
func TestAForksPackMayDeclareLaunchComputedForItsBin(t *testing.T) {
	m, problems := Decode([]byte(`{"launch_computed":{"tool":[{"key":"policy","argv":["--policy","{value}"]}]},
	  "contributes":[` + forkOfTool + `]}`))
	if len(problems) != 0 {
		t.Fatalf("a fork's pack was refused: %v", problems)
	}
	if got := m.LaunchComputedByBin["tool"]; len(got) != 1 || got[0].Key != "policy" {
		t.Errorf("decoded %+v", got)
	}
	_, problems = Decode([]byte(`{"launch_computed":{"tool":[{"key":"policy","argv":["--policy"]}]},
	  "contributes":[` + forkOfTool + `]}`))
	if !containsSubstr(problems, "must carry the value") {
		t.Errorf("a fork's entry escapes the shape checks: %v", problems)
	}
	ls := &LaunchSelection{Surface: ".t/s.json", Flags: []LaunchSelectionFlag{{Key: "defaultModel",
		Argv: []string{"--model", "{value}"}}}}
	if p := ForkLaunchComputedProblems("fork", "tool", ls, []LaunchComputedFlag{{Key: "defaultModel",
		Argv: []string{"--m", "{value}"}}}); !containsSubstr(p, "hand already") || !containsSubstr(p, "pack fork: ") {
		t.Errorf("a selection key from a fork: %v", p)
	}
	if p := ForkLaunchComputedProblems("fork", "tool", nil, []LaunchComputedFlag{{Key: "policy",
		Argv: []string{"--p", "{value}"}}}); !containsSubstr(p, `declares no "launch_selection"`) {
		t.Errorf("a base with no selection: %v", p)
	}
}

// THE MERGE: a fork's entry for a key the base declares takes its place, remembering it; its other
// keys follow the base's; every fork entry names its pack; and BaseComputed undoes it exactly.
func TestAForksComputedFlagsReplaceTheBasesByKey(t *testing.T) {
	base := []LaunchComputedFlag{{Key: "a", Argv: []string{"--a", "{value}"}}, {Key: "b", Argv: []string{"--b", "{value}"}}}
	fork := []LaunchComputedFlag{{Key: "c", Argv: []string{"--c", "{value}"}, OptIn: "C_ON"},
		{Key: "b", Argv: []string{"--b2", "{value}"}}}
	merged := MergeForkComputed(base, fork, "f")
	var words, by []string
	for _, f := range merged {
		words, by = append(words, f.Word()), append(by, f.ForkedBy)
	}
	if !reflect.DeepEqual(words, []string{"--a", "--b2", "--c"}) || !reflect.DeepEqual(by, []string{"", "f", "f"}) {
		t.Fatalf("merged words %q by %q", words, by)
	}
	if merged[1].Replaces == nil || merged[1].Replaces.Word() != "--b" || merged[2].Replaces != nil {
		t.Errorf("Replaces: %+v", merged)
	}
	if got := BaseComputed(merged); !reflect.DeepEqual(got, base) {
		t.Errorf("BaseComputed = %+v, want the base's own %+v", got, base)
	}
	if got := BaseComputed(MergeForkComputed(nil, fork, "f")); got != nil {
		t.Errorf("a fork-only set leaves %+v without the fork", got)
	}
	merged[1].Replaces.Argv[0] = "edited"
	if base[1].Argv[0] != "--b" {
		t.Error("the merge aliases the base's entries")
	}
}

func TestLaunchComputedIsRefusedWhereNoLaunchCouldHandIt(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"another pack's bin": {`{"launch_computed":{"other":[{"key":"s","argv":["--s","{value}"]}]},
		  "contributes":[` + computedProgram + `]}`, "must name a program this pack declares or forks"},
		"a program with no selection": {`{"launch_computed":{"q":[{"key":"s","argv":["--s","{value}"]}]},
		  "contributes":[{"kind":"program","bin":"q","via":"npm","package":"q"}]}`, `declares no "launch_selection"`},
		"a selection in a variable": {`{"launch_computed":{"q":[{"key":"s","argv":["--s","{value}"]}]},
		  "contributes":[{"kind":"program","bin":"q","via":"npm","package":"q",
		  "launch_selection":{"surface":".q/c.json","env":"Q_CONFIG"}}]}`, "use \"each\" or \"flags\" there"},
		"no key": {`{"launch_computed":{"tool":[]},"contributes":[` + computedProgram + `]}`, "names no key"},
		"an empty key": {`{"launch_computed":{"tool":[{"key":"","argv":["--s","{value}"]}]},
		  "contributes":[` + computedProgram + `]}`, `names no "key"`},
		"the selection namespace": {`{"launch_computed":{"tool":[{"key":"selection","argv":["--s","{value}"]}]},
		  "contributes":[` + computedProgram + `]}`, "reserved namespace"},
		"a key twice": {`{"launch_computed":{"tool":[{"key":"s","argv":["--s","{value}"]},
		  {"key":"s","argv":["--t","{value}"]}]},"contributes":[` + computedProgram + `]}`, "a second time"},
		"a selection key": {`{"launch_computed":{"tool":[{"key":"defaultModel","argv":["--s","{value}"]}]},
		  "contributes":[` + computedProgram + `]}`, "hand already"},
		"no value": {`{"launch_computed":{"tool":[{"key":"s","argv":["--s"]}]},
		  "contributes":[` + computedProgram + `]}`, "must carry the value"},
		"no argv": {`{"launch_computed":{"tool":[{"key":"s"}]},
		  "contributes":[` + computedProgram + `]}`, `has no "argv"`},
		"a bad opt-in": {`{"launch_computed":{"tool":[{"key":"s","argv":["--s","{value}"],"opt_in":"T-ON"}]},
		  "contributes":[` + computedProgram + `]}`, `"opt_in" "T-ON" is not a variable name`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, problems := Decode([]byte(tc.doc))
			if !containsSubstr(problems, tc.want) {
				t.Errorf("problems = %v, want one saying %q", problems, tc.want)
			}
			for _, p := range problems {
				if !strings.Contains(p, "launch_computed") {
					t.Errorf("a problem outside the declaration: %q", p)
				}
			}
		})
	}
}
