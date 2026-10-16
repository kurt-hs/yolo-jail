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

func TestLaunchComputedIsRefusedWhereNoLaunchCouldHandIt(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"another pack's bin": {`{"launch_computed":{"other":[{"key":"s","argv":["--s","{value}"]}]},
		  "contributes":[` + computedProgram + `]}`, "must name a non-fork program declared by this pack"},
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
