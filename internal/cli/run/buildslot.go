package run

// buildslot.go is THE FORK-BUILD SLOT as one act (docs/design/pi-extension-store-builds.md §5.2,
// XB-D10): a fresh launch hands every key its slot serves — each plain fork's missing build, each
// patched fork's advance, each patched extension's advance and copy — to the build act at once
// (Options.BuildSlot), whose pool runs them together, and starts the image's own build beside it
// (imageprewarm.go), which nothing the slot does feeds.
//
// The slot's two halves stay the triggers they were: forkBuildPlan and treeBuildPlan decide each
// key's reason, or that the act readies it, and finishForkDeliveries and finishTreeDeliveries merge
// the act's answers. Only the act's call is one call, so one pool and one interrupt scope cover both
// halves: a Ctrl-C in any build ends every wait of the slot (PF-D57), and the keys print in
// declaration order, the forks' before the extensions', as the two halves always did (PPX-D13).

import (
	goruntime "runtime" // stdlib; this package's `runtime` is yolo's own (run.go)
	"sort"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

// BuildSlotRequest is the fork-build slot's whole act (Options.BuildSlot): the fork builds' request
// and the tree arm's, either nil when its half has nothing for the act, and how many checks and
// sealed build jails the act runs at once.
type BuildSlotRequest struct {
	// Perf is the launch collector; nil keeps preparation phase timing disabled.
	Perf  *perf.Log
	Forks *ForkBuildRequest
	Trees *TreeBuildRequest
	// MaxChecks and MaxBuilds bound the pool (SlotChecks, SlotBuildJails); 0 takes those.
	MaxChecks, MaxBuilds int
}

// SlotChecks is how many upstream checks a launch's fork-build slot runs at once (XB-D10): a check
// is network-bound and cheap (§5.2, MEASURED in the research pass).
const SlotChecks = 8

// SlotBuildJails is how many sealed build jails a launch's fork-build slot runs at once on rt
// (XB-D10): min(4, max(1, CPUs/2)), a build being CPU-bound and 4 being pi's own
// GIT_UPDATE_CONCURRENCY; and one on Apple Container, where a capture jail cannot start beside a
// running jail (INFERRED, program-delivery.md OQ-PD25) and a second build jail is one.
func SlotBuildJails(rt string) int {
	return SlotBuildJailsOn(rt, goruntime.NumCPU())
}

// SlotBuildJailsOn is SlotBuildJails on a machine of cpus CPUs: the one statement of the rule, which
// a caller that reads its CPU count through a seam of its own (cli's pools) applies to that count.
func SlotBuildJailsOn(rt string, cpus int) int {
	if !BuildJailsSideBySide(rt) {
		return 1
	}
	return min(4, max(1, cpus/2))
}

// handedForksMu guards Options.handedForks, which every advance of the slot's pool appends to at
// once through ForkBuildRequest.Hand.
var handedForksMu sync.Mutex

// runForkBuildSlot is the slot: both halves planned, one call of the act for every key they ready,
// and both answered and recorded. With no BuildSlot (a caller that injects the halves alone) each
// half calls its own act, the forks' first.
//
// ON macos-user ONLY THE FORK HALF RUNS (docs/reference/patched-forks.md PF-D97): a fork gets a
// darwin build there, while a patched extension still has none, and says so itself
// (noteMacosUserTrees).
func (o *Options) runForkBuildSlot(cfg *jsonx.OrderedMap, rt, repoRoot string) {
	withTrees := rt != "macos-user" // parity: Warned — macos-user builds no patched extension, and noteMacosUserTrees says so
	if o.BuildSlot == nil {
		o.forkDelivered = o.forkDeliveriesFor(rt)
		if withTrees {
			o.treeDelivered = o.treeDeliveriesFor(rt)
		}
		return
	}
	forks, fReq := o.forkBuildPlan(rt)
	var trees map[string]TreeDelivery
	var tReq *TreeBuildRequest
	if withTrees {
		trees, tReq = o.treeBuildPlan(rt)
	}
	if fReq != nil || tReq != nil {
		// THE IMAGE'S OWN BUILD, BESIDE THE POOL: nothing the slot does feeds it, so it starts now
		// rather than once the slowest key has ended.
		o.startImagePrewarm(cfg, rt, repoRoot)
		fAns, tAns := o.BuildSlot(BuildSlotRequest{Forks: fReq, Trees: tReq, Perf: o.Perf, MaxChecks: SlotChecks,
			MaxBuilds: SlotBuildJails(rt)})
		if fReq != nil {
			o.finishForkDeliveries(forks, fReq, fAns)
		}
		if tReq != nil {
			o.finishTreeDeliveries(trees, tAns)
		}
	}
	if forks != nil {
		o.recordHandedForks(forks)
	}
	if trees != nil {
		o.recordHandedTrees(trees)
	}
	o.forkDelivered, o.treeDelivered = forks, trees
}

// runForkBuildSlotAndRefusals is a fresh launch's whole fork-build slot, both arms' one sequence
// (the container path's runContainer and the macos-user arm, PF-D97): the recovery hatches' scope
// said once, the slot run, then the refusals the slot's answers decide — a failed fresh
// preparation, a patch failure (patchfailures.go; PF-D81, PF-D83) and a missing patched build
// (missingbuilds.go; PPX-D40, PF-D77) — with the fallback lines between them. False when the
// launch refuses, having said why; the caller lets go of what it holds and returns 1.
func (o *Options) runForkBuildSlotAndRefusals(cfg *jsonx.OrderedMap, rt, repoRoot string) bool {
	forkSpan := o.Perf.Span("launch.fork_builds")
	o.noteRecoveryHatches(rt, false) // recoveryscope.go: what each set recovery hatch reaches, once
	o.runForkBuildSlot(cfg, rt, repoRoot)
	// A PATCH SERIES THAT DOES NOT APPLY IS FATAL: its error block first, before any line about what
	// the slot delivered, then the refusal when an intact older build would otherwise run in silence;
	// YOLO_ALLOW_PATCH_FAILURES=1 runs that build for this launch.
	if o.refuseFreshPreparation() || o.refusePatchFailures(rt) {
		forkSpan.End()
		return false
	}
	o.noteTreeDeliveries(rt)
	forkSpan.End()
	// A MISSING PATCHED BUILD IS FATAL, whatever the command, naming each cause once and the ways
	// back; YOLO_ALLOW_MISSING_PROGRAMS=1 goes on without them.
	return !o.refuseMissingBuilds(rt)
}

// refuseFreshPreparation runs before the ordinary patch/missing-build hatches.
func (o *Options) refuseFreshPreparation() bool {
	if !o.Update {
		return false
	}
	failures := map[string]string{}
	for key, d := range o.forkDelivered {
		if d.FreshFailure != "" {
			failures[key] = d.FreshFailure
		}
	}
	for key, d := range o.treeDelivered {
		if d.FreshFailure != "" {
			failures[key] = d.FreshFailure
		}
	}
	keys := make([]string, 0, len(failures))
	for key := range failures {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		o.pr(o.Stderr).printf("Refusing --update: fresh preparation of %s failed: %s. Repair the named problem and retry, or launch without --update to use ordinary recovery.", key, failures[key])
	}
	return len(keys) > 0
}
