package packdecl

// launchcomputed.go is the LAUNCH COMPUTED FLAG declaration — a term coined here (2026-10-15) for
// a top-level key of a program's config-surface COMPUTED layer, outside the reserved `selection`
// namespace, that one `yolo host -p` launch hands the program as argv words instead of the file
// `yolo host apply` writes for the configured profile alone, so a `-p` that moves the program's
// session by its launch selection (docs/design/model-lists-and-pickers.md MM-D30) can move a
// per-profile block its derive composes with it (docs/reference/pack-system.md#launch_computed).
// No shipped pack declares one (MM-D42, amended 2026-10-16); the kind is for any pack declaring
// the program, a user's own included, whose program reads such a flag.
//
// It EXTENDS the program's launch selection (LaunchSelection): the same surface, the same
// derive run, the same comparison with the configured profile, the same subcommand rule, the same
// YOLO_NO_LAUNCH_FLAGS escape and disclosure, and nothing in a jail. What differs is the value: an
// object is spelled as ONE word of compact JSON, keys sorted and a tombstoned key as null, so the
// program can decode it as the very block its file would hold. And it is compared apart: a -p
// whose selection agrees with the configured profile's but whose computed key differs hands that
// key alone, so a model the user picked in the program since `yolo host apply` stands.
//
// PACK-WIDE, keyed by the program's bin (`"launch_computed": {"tool": [...]}`), the place
// model_list_check and provider_keys took for the same reason: a new field inside a `program`
// contribution makes an older yolo's tolerant reader skip the WHOLE program, while it ignores an
// unknown top-level key (packs/releasedecode_test.go measures both).

import (
	"fmt"
	"sort"
	"strings"
)

// LaunchComputedFlag is one computed key a program's launch hands as argv (see the file comment).
type LaunchComputedFlag struct {
	// Key is the top-level key of the launch selection's surface's computed layer: `policy`.
	Key string `json:"key"`
	// Argv is the words, one holding LaunchSelectionValue, which becomes the key's value as one
	// word of compact JSON: `["--policy", "{value}"]`.
	Argv []string `json:"argv"`
	// OptIn, when set, names a variable the launch's environment must set to "1" before the
	// flag is handed. A release of the program that does not read the flag refuses the launch
	// over an unknown option (pi 1.0.1 exits on one), so a flag the program's current release
	// does not read ships OFF, the variable naming how to turn it on; a launch that would hand it
	// with the variable unset says so.
	OptIn string `json:"opt_in,omitempty"`
}

// Word is the flag's own word, the first of its argv carrying no value: what a launch's line
// names when it holds the flag back.
func (f LaunchComputedFlag) Word() string {
	for _, w := range f.Argv {
		if !strings.Contains(w, LaunchSelectionValue) {
			return w
		}
	}
	return strings.Join(f.Argv, " ")
}

// validateLaunchComputed refuses a `launch_computed` no launch could hand: a bin that is not a
// non-fork program of this pack declaring a launch selection in an argv form (the words ride the
// selection's own composition and go after argv[0]), an empty or repeated key, a key the
// selection's own flags already hand, words that never carry the value or hold an empty word, or
// an opt-in that is not a variable name.
func (m *Manifest) validateLaunchComputed() []string {
	bins := make([]string, 0, len(m.LaunchComputedByBin))
	for bin := range m.LaunchComputedByBin {
		bins = append(bins, bin)
	}
	sort.Strings(bins)
	var problems []string
	for _, bin := range bins {
		label := fmt.Sprintf("launch_computed[%q]", bin)
		var ls *LaunchSelection
		owned := false
		for _, c := range m.Contributions() {
			if bin != "" && c.Kind == KindProgram && c.Bin == bin && !c.IsFork() {
				owned, ls = true, c.LaunchSelection
				break
			}
		}
		switch {
		case !owned:
			problems = append(problems, label+": must name a non-fork program declared by this pack; move it to the owning pack")
			continue
		case ls == nil:
			problems = append(problems, label+": program "+bin+" declares no \"launch_selection\", whose surface "+
				"and composition the computed keys ride: declare one first")
			continue
		case len(ls.Each) == 0 && len(ls.Flags) == 0:
			problems = append(problems, label+": program "+bin+"'s \"launch_selection\" hands its selection in "+
				"a variable, and a computed key is argv words: use \"each\" or \"flags\" there")
			continue
		}
		selectionKeys := map[string]bool{}
		for _, f := range ls.Flags {
			selectionKeys[f.Key] = true
		}
		flags := m.LaunchComputedByBin[bin]
		if len(flags) == 0 {
			problems = append(problems, label+": names no key; omit the bin instead")
		}
		seen := map[string]bool{}
		for i, f := range flags {
			field := fmt.Sprintf("%s[%d]", label, i)
			switch {
			case f.Key == "":
				problems = append(problems, field+": names no \"key\"")
			case f.Key == "selection":
				problems = append(problems, field+": \"selection\" is the reserved namespace the launch "+
					"selection hands already, not a computed key")
			case seen[f.Key]:
				problems = append(problems, fmt.Sprintf("%s: hands key %q a second time", field, f.Key))
			case selectionKeys[f.Key]:
				problems = append(problems, fmt.Sprintf("%s: key %q is a selection key its \"launch_selection\" "+
					"flags hand already", field, f.Key))
			}
			seen[f.Key] = true
			if len(f.Argv) == 0 {
				problems = append(problems, field+": has no \"argv\"")
			}
			value := false
			for _, w := range f.Argv {
				if w == "" {
					problems = append(problems, field+": \"argv\" has an empty word")
				}
				value = value || strings.Contains(w, LaunchSelectionValue)
			}
			if len(f.Argv) > 0 && !value {
				problems = append(problems, fmt.Sprintf("%s: \"argv\" must carry the value with %q in one of its words",
					field, LaunchSelectionValue))
			}
			if f.OptIn != "" && !ValidEnvName(f.OptIn) {
				problems = append(problems, fmt.Sprintf("%s: \"opt_in\" %q is not a variable name "+
					"(must match [A-Za-z_][A-Za-z0-9_]*)", field, f.OptIn))
			}
		}
	}
	return problems
}

// launchComputedFor is a copy of the computed flags this pack declares for bin, nil for none.
func (m *Manifest) launchComputedFor(bin string) []LaunchComputedFlag {
	flags := m.LaunchComputedByBin[bin]
	if len(flags) == 0 {
		return nil
	}
	out := make([]LaunchComputedFlag, 0, len(flags))
	for _, f := range flags {
		out = append(out, LaunchComputedFlag{Key: f.Key, Argv: append([]string(nil), f.Argv...), OptIn: f.OptIn})
	}
	return out
}
