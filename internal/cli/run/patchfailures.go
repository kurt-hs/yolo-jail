package run

// patchfailures.go is A JAIL LAUNCH'S PATCH FAILURE (docs/reference/patched-forks.md §8, PF-D81, PF-D83):
// as soon as the fork-build slot ends, before any line the launch prints about what the slot
// delivered and before the image step and the boot, the launch prints the error block of each selected patched fork
// and patched extension whose series does not apply — the slot's advances hand the typed failure and
// print no block of their own (cli's advance.launchSaysPatchFailure), so the block is said once, here,
// where no build's progress follows it — and then decides:
//
//   - WITH AN INTACT ADMITTED BUILD TO RUN (the slot handed one beside the failure), the launch
//     REFUSES, whatever the command: a serving older build does not turn a patch failure into a
//     warning (PF-D81). paths.AllowPatchFailuresEnv, `1` or names covering the build
//     (internal/patchrecovery's grammar), is the bypass: the launch runs that build for this one
//     launch, saying so in one Continuing line under the block. paths.AllowMissingProgramsEnv does
//     not waive it, since leaving the program out is not what the block offers when a build of it is
//     here (PF-D83).
//   - WITH NOTHING TO RUN, the build is missing, and missingbuilds.go's refusal follows, which
//     paths.AllowMissingProgramsEnv waives by starting without it (PF-D77, PF-D83); the block's
//     hatch list names that, or cached-good recovery (PF-D82) when an older recorded good build of a
//     fork is here.
//   - A PATCHED EXTENSION WITH A FALLBACK and no build gets its fallback, as XB-D7 rules, with the
//     failure as the fallback line's reason (noteTreeDeliveries); one no selected agent pack loads in
//     a jail stops nothing, its block saying so.
//
// THE HATCH LIST is the delivery's Recovery when the advance set it (docs/design/
// patched-build-recovery.md §6.8, PBR-D5: the advance is its one authority); a delivery without one
// derives it here from what the slot handed (Runs, CachedGood). The block is the one shape
// (packsrc.PatchFailure.Render), colored through the launch's own gate (Options.pr).

import (
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// launchPatchFailure is one patched build whose series this launch found does not apply.
type launchPatchFailure struct {
	label   string
	subject patchrecovery.Subject
	recipe  packsrc.RebaseRecipe
	failure *packsrc.PatchFailure
	bypass  packsrc.PatchBypass
	// runs names the intact admitted build this launch would run in the failed series' place, ""
	// when there is none; unneeded says no selected agent pack loads it where this launch runs.
	runs     string
	unneeded bool
}

// launchPatchFailures are this launch's patch failures, in the order the slot ran them: the patched
// forks, then the patched extensions.
func (o *Options) launchPatchFailures(rt string) []launchPatchFailure {
	// EVERY RUNTIME THE SLOT RAN ON, not only those that build: below Apple Container's read-only
	// floor the tree arm builds nothing yet still hands a good build with its recorded failure, and
	// its advance printed no block (cli's advance.launchSaysPatchFailure). Only a jail runs no slot.
	if config.InJail() {
		return nil
	}
	var out []launchPatchFailure
	for _, p := range o.forkPinned {
		if !p.Fork.Patched() || forkUnpublishedHere(p.Fork) != "" {
			continue
		}
		d, ok := o.forkDelivered[p.Fork.Bin]
		if !ok || d.PatchFailure == nil {
			continue
		}
		b := forkPatchBypass(p.Fork.Key(), d)
		out = append(out, launchPatchFailure{label: "fork " + p.Fork.Key(),
			subject: patchrecovery.Subject{Key: p.Fork.Key(), Pack: p.Fork.Pack, Name: p.Fork.Bin},
			recipe:  p.Fork.RebaseRecipe(), failure: d.PatchFailure, bypass: b, runs: b.Runs})
	}
	for _, f := range o.patchedTrees {
		d, ok := o.treeDelivered[f.Key()]
		if !ok || d.PatchFailure == nil || d.Dir == "" && f.Fallback != "" {
			continue
		}
		b := packsrc.PatchBypass{Command: "yolo", Key: f.Key(), Hatches: d.Recovery}
		switch {
		case f.Owner == "" || !f.ListedInJail:
			b.Unneeded = true
		case d.Dir != "":
			b.Runs = d.label()
		default:
			b.Missing = true
		}
		out = append(out, launchPatchFailure{label: f.Label(),
			subject: patchrecovery.Subject{Key: f.Key(), Pack: f.Pack, Name: f.Bin},
			recipe:  f.RebaseRecipe(), failure: d.PatchFailure, bypass: b, runs: b.Runs, unneeded: b.Unneeded})
	}
	return out
}

// forkPatchBypass is a patched fork's hatch list at a jail launch (PF-D83): the advance's Recovery
// when it set one, else derived from what the slot handed — the build it runs, else the older
// recorded good build cached-good recovery runs, else leaving it out.
func forkPatchBypass(owner string, d entrypoint.ForkDelivery) packsrc.PatchBypass {
	b := packsrc.PatchBypass{Command: "yolo", Key: owner, Hatches: d.Recovery}
	switch {
	case d.Key != "":
		b.Runs = d.Runs
		if b.Runs == "" {
			b.Runs = "store entry " + d.Key
		}
	case d.CachedGood != "":
		b.CachedGood, b.CachedGoodLabel = owner, d.CachedGood
	default:
		b.Missing = true
	}
	return b
}

// refusePatchFailures prints every patch failure's error block, then refuses the launch when a
// failure has a build to run and the bypass does not cover it: true when this launch refuses. A
// failure with nothing to run is left to refuseMissingBuilds, after it.
func (o *Options) refusePatchFailures(rt string) bool {
	failures := o.launchPatchFailures(rt)
	if len(failures) == 0 {
		return false
	}
	out := o.pr(o.Stderr)
	bypass := patchrecovery.Read(o.Getenv, patchrecovery.AllowPatchFailuresEnv)
	refused := false
	for i, f := range failures {
		if i > 0 {
			out.print("")
		}
		continuing := f.runs != "" && !f.unneeded && bypass.Covers(f.subject)
		b := f.bypass
		if continuing {
			// The hatch is already set: no list, one line under the block instead.
			b = packsrc.PatchBypass{}
		}
		_, _ = io.WriteString(out.rt.W, f.failure.Render(f.label, f.recipe, b, out.rt.Color))
		switch {
		case f.runs == "" || f.unneeded:
		case continuing:
			out.print("[yellow]" + richtext.Escape("Continuing with "+f.runs+" ("+paths.AllowPatchFailuresEnv+")") + "[/yellow]")
		default:
			refused = true
		}
	}
	return refused
}
