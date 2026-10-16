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
//
// A FORK'S PACK MAY DECLARE IT TOO (pack-system.md#LC-D1, an implementation decision, reversible):
// a pack whose `program` contribution forks bin X (`fork_of`) may declare `launch_computed` for X,
// since a flag only the patched build registers belongs with the pack that builds it. The
// selection's fork rewrite (packload.ApplyForks) merges it into the base's: the fork's entry for a
// key REPLACES the base's for that key, and its other keys follow the base's. Each merged entry
// names its fork (ForkedBy), and a launch that runs the base's upstream program in the fork's place
// (YOLO_SKIP_PATCHES) hands the base's own entries alone (BaseComputed). A pack that neither
// declares nor forks X is refused.

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

	// ForkedBy names the fork pack that declared this entry, "" for the program's own pack's: set
	// only by the selection's fork rewrite (packload.ApplyForks), never read from a manifest.
	ForkedBy string `json:"-"`
	// Replaces is the program's own pack's entry for the same key, which a fork's entry replaced;
	// nil when the base declares none. Set with ForkedBy.
	Replaces *LaunchComputedFlag `json:"-"`
}

// clone is a deep copy of f.
func (f LaunchComputedFlag) clone() LaunchComputedFlag {
	out := f
	out.Argv = append([]string(nil), f.Argv...)
	if f.Replaces != nil {
		r := f.Replaces.clone()
		out.Replaces = &r
	}
	return out
}

// BaseComputed is flags as the program's own pack declares them, every fork's entry dropped or
// returned to the base entry it replaced: what a launch hands when the program running is not the
// fork's build. Nil for none.
func BaseComputed(flags []LaunchComputedFlag) []LaunchComputedFlag {
	var out []LaunchComputedFlag
	for _, f := range flags {
		switch {
		case f.ForkedBy == "":
			out = append(out, f.clone())
		case f.Replaces != nil:
			out = append(out, f.Replaces.clone())
		}
	}
	return out
}

// MergeForkComputed is base's computed flags with a fork's merged in, the fork pack forkPack's
// entries each marked ForkedBy: a fork's entry for a key the base declares takes that entry's
// place, remembering it (Replaces), and the fork's other keys follow the base's in declaration
// order. Nil when both are empty.
func MergeForkComputed(base, fork []LaunchComputedFlag, forkPack string) []LaunchComputedFlag {
	byKey := map[string]int{}
	var out []LaunchComputedFlag
	for _, f := range base {
		byKey[f.Key] = len(out)
		out = append(out, f.clone())
	}
	for _, f := range fork {
		g := f.clone()
		g.ForkedBy, g.Replaces = forkPack, nil
		if i, ok := byKey[f.Key]; ok {
			prev := out[i]
			if prev.ForkedBy != "" {
				// Merging over an earlier merge: replace what the base had, or nothing when the
				// key was the fork's alone.
				if prev.Replaces == nil {
					out[i] = g
					continue
				}
				prev = *prev.Replaces
			}
			g.Replaces = &prev
			out[i] = g
			continue
		}
		byKey[f.Key] = len(out)
		out = append(out, g)
	}
	return out
}

// ForkLaunchComputedProblems refuses a fork's `launch_computed` for bin against the BASE program's
// launch selection ls, which the fork pack alone cannot see: the checks validateLaunchComputed
// makes of a pack's own program, labelled with the fork pack's name.
func ForkLaunchComputedProblems(forkPack, bin string, ls *LaunchSelection, flags []LaunchComputedFlag) []string {
	return launchComputedProblems(fmt.Sprintf("pack %s: launch_computed[%q]", forkPack, bin), bin, ls, flags, true)
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
		owned, forked := false, false
		for _, c := range m.Contributions() {
			if bin == "" || c.Kind != KindProgram || c.Bin != bin {
				continue
			}
			if c.IsFork() {
				forked = true
				continue
			}
			owned, ls = true, c.LaunchSelection
			break
		}
		if !owned && !forked {
			problems = append(problems, label+": must name a program this pack declares or forks (`fork_of`); "+
				"move it to the pack that declares "+bin+", or to the pack that forks it")
			continue
		}
		// A fork's base program, whose launch selection the checks below need, is another pack's:
		// the selection's fork rewrite checks it there (ForkLaunchComputedProblems).
		problems = append(problems, launchComputedProblems(label, bin, ls, m.LaunchComputedByBin[bin], owned)...)
	}
	return problems
}

// launchComputedProblems is one bin's checks. withSelection makes the checks against ls, the
// program's launch selection; without it only the entries' own shape is checked.
func launchComputedProblems(label, bin string, ls *LaunchSelection, flags []LaunchComputedFlag, withSelection bool) []string {
	var problems []string
	selectionKeys := map[string]bool{}
	if withSelection {
		switch {
		case ls == nil:
			return append(problems, label+": program "+bin+" declares no \"launch_selection\", whose surface "+
				"and composition the computed keys ride: declare one first")
		case len(ls.Each) == 0 && len(ls.Flags) == 0:
			return append(problems, label+": program "+bin+"'s \"launch_selection\" hands its selection in "+
				"a variable, and a computed key is argv words: use \"each\" or \"flags\" there")
		}
		for _, f := range ls.Flags {
			selectionKeys[f.Key] = true
		}
	}
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
		out = append(out, f.clone())
	}
	return out
}
