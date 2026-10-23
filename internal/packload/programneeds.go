package packload

// programneeds.go resolves a `program` NEED — an entry with no recipe — against the recipes the
// other SELECTED packs carry (docs/design/provisioner-sets.md OQ-PS5 and PS-D18, ruled
// 2026-10-23): "pack A … needs a program, and you could have both pack B and C that the user
// could choose on their own, which fills in that program need through whatever way pack B and C
// want."
//
// THE INPUT IS THE SELECTED SET AND NOTHING WIDER, by the OQ-BH14 rule every selection-scoped
// question in this package follows: an unselected pack is treated as if it does not exist, so
// a pack yolo ships but the user did not select never provides a need. Callers pass what they
// loaded; nothing here reads Embedded().
//
// Three outcomes per need, and every reader — the launch pre-flight, `yolo check`, `yolo host
// apply`, the jail's boot assertion — takes them from here so they cannot disagree:
//
//   - one selected pack carries a recipe for the name: SATISFIED, by that pack, whatever recipe
//     kind it carries (npm, installer, a fork, the pack's own tree);
//   - two or more do: a PROVIDER COLLISION (ProgramProviderCollisions), which refuses the launch
//     naming every provider — the user picks the provider by picking the pack, so yolo does not
//     pick for them;
//   - none does: UNPROVIDED, and the need keeps what `requires` did — asserted present on PATH
//     at boot, warned about by name when absent, probed on the host with its install_hints.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// ProgramNeed is one need of one selected pack, resolved.
type ProgramNeed struct {
	// Pack is the pack declaring the need; Bin, the program it needs.
	Pack, Bin string
	// Hints is the need's own install_hints, for the unprovided report.
	Hints map[string]string
	// Providers are the OTHER selected packs carrying a recipe for Bin, sorted. Empty when no
	// selected pack provides it; more than one is a ProgramProviderCollisions refusal.
	Providers []string
}

// Provider is the one pack satisfying the need, or "" when none or several do.
func (n ProgramNeed) Provider() string {
	if len(n.Providers) == 1 {
		return n.Providers[0]
	}
	return ""
}

// ProvidedLine is the sentence a launch prints for a satisfied need, naming where the program
// comes from; "" for a need no single pack satisfies.
func (n ProgramNeed) ProvidedLine() string {
	if n.Provider() == "" {
		return ""
	}
	return fmt.Sprintf("program %s, needed by pack %s, is provided by pack %s", n.Bin, n.Pack, n.Provider())
}

// programProviders maps each bin to the selected packs whose installed programs carry a recipe
// for it, through InstallContributions — the same projection the launchers are written from, so
// a fork's rewritten base program counts once, under its base pack, and a fork pack provides
// nothing of its own (FP-D5).
func programProviders(packs []*Pack) map[string][]string {
	out := map[string][]string{}
	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, in := range p.Decl.InstallContributions() {
			if in.Bin == "" || containsString(out[in.Bin], p.Name) {
				continue
			}
			out[in.Bin] = append(out[in.Bin], p.Name)
		}
	}
	for bin := range out {
		sort.Strings(out[bin])
	}
	return out
}

// ResolveProgramNeeds resolves every need of every selected pack against the others' recipes,
// in pack order then declaration order.
func ResolveProgramNeeds(packs []*Pack) []ProgramNeed {
	providers := programProviders(packs)
	var out []ProgramNeed
	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, req := range p.Decl.RequiredBins() {
			n := ProgramNeed{Pack: p.Name, Bin: req.Bin, Hints: req.Hints}
			for _, prov := range providers[req.Bin] {
				// A pack does not provide its own need: one manifest declaring a program twice is
				// refused at load (packdecl's validateProgramBins), so this is only belt-and-braces.
				if prov != p.Name {
					n.Providers = append(n.Providers, prov)
				}
			}
			out = append(out, n)
		}
	}
	return out
}

// ProvidedNeeds is the selected packs' needs some other selected pack satisfies, keyed
// "<pack>\x00<bin>", for a reader that skips the presence assertion for them.
func ProvidedNeeds(packs []*Pack) map[string]string {
	out := map[string]string{}
	for _, n := range ResolveProgramNeeds(packs) {
		if prov := n.Provider(); prov != "" {
			out[n.Pack+"\x00"+n.Bin] = prov
		}
	}
	return out
}

// ProgramProviderCollisions finds one program NAME that two or more selected packs carry a
// recipe for — PS-D18's refusal. Whether any selected pack needs the name does not matter: two
// launchers for one bin is the collision whether or not a third pack asked for it, and naming
// both providers with the one remedy is what lets the user pick.
//
// EXPORTED for AgentNameCollisions' reason: the launch pre-flight, `yolo check` and `yolo host
// apply` refuse THIS collision specifically, and none of them consults Collisions wholesale.
// AgentNameCollisions skips a name whose every claim is a `program`, so one clash is reported
// once, here, with this remedy.
func ProgramProviderCollisions(packs []*Pack) []Collision {
	providers := programProviders(packs)
	needers := map[string][]string{}
	for _, n := range ResolveProgramNeeds(packs) {
		needers[n.Bin] = append(needers[n.Bin], n.Pack)
	}
	bins := make([]string, 0, len(providers))
	for bin, ps := range providers {
		if len(ps) > 1 {
			bins = append(bins, bin)
		}
	}
	sort.Strings(bins)
	var out []Collision
	for _, bin := range bins {
		out = append(out, Collision{
			Kind:   packdecl.KindProgram,
			Target: bin,
			Packs:  providers[bin],
			Reason: programProviderReason(bin, providers[bin], needers[bin]),
		})
	}
	return out
}

func programProviderReason(bin string, providers, needers []string) string {
	var who []string
	for _, p := range providers {
		who = append(who, "pack "+p)
	}
	msg := "a program has ONE provider in a launch, and " + strings.Join(who, " and ") +
		" each carry a recipe for " + bin
	if len(needers) > 0 {
		msg += " (needed by pack " + strings.Join(needers, ", pack ") + ")"
	}
	return msg + ". You choose the provider by choosing the pack: drop all but one of " +
		strings.Join(providers, ", ") + " from `packs`"
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
