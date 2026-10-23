package run

// packtreepins.go is the launch's half of a PACK-TREE PROGRAM (docs/design/pack-buildable-programs.md,
// a `program` delivered `via: "pack"`): each one this launch's packs carry, keyed on the tree
// digest of its source directory in the launch's OWN staged pack tree, which nothing edits after
// staging (§6.1). The digest stands where a fork's pinned commit stands (packload.ForkPin.Commit),
// so the fork route's trigger, build slot and delivery (forkbuild.go) carry the program unchanged:
// a store entry for this digest is a hit and prints nothing, and anything else is built once.
//
// Where the pack ships no source directory, or no build can ever run for this backend, the
// program's declared PREBUILT artifact for the jail's platform stands in (§7,
// packload.PackTreeResolve): its sha256 is the pin, the build slot fetches and verifies it, and the
// launch discloses it with that sha256 on every fresh launch and attach (prebuiltline.go).

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// packTreePins are this launch's pack-tree programs with the digest each is built at, the prebuilt
// artifact each takes in its place, or why it has neither. Like noteForkPins, nothing is decided
// inside a capture or build jail (CapturesDir ""), whose own launch never builds.
//
// The digest reads the source directory whole on every fresh launch: the same order as the staging
// copy every launch already makes of the pack (the design's §6.1 warning), and a hit costs nothing
// more.
func (o *Options) packTreePins(packs []*packload.Pack, rt string) []packload.ForkPin {
	if o.CapturesDir() == "" {
		return nil
	}
	var out []packload.ForkPin
	platform := jailPlatform(rt)
	for _, f := range packload.PackTreePrograms(packs) {
		out = append(out, packload.PackTreeResolve(f, platform, packTreeBuildable(rt)))
	}
	return out
}

// addPackTreePins joins this launch's pack-tree pins to the fork pins the slot reads, the dev
// overrides dropped as they are from the forks: keyed on their staged tree's digest, they ride the
// forks' trigger and slot from here, and say nothing above the dispatch — a hit is silent, and a
// build says its own lines (BG-D10). Called on the FRESH-LAUNCH path alone, just before the slot,
// on both arms: the digest reads each source directory whole, which an attach and a dry run never
// do (§10). Idempotent, for a path that reaches the slot again.
func (o *Options) addPackTreePins(packs []*packload.Pack, rt string) {
	kept := o.forkPinned[:0:0]
	for _, p := range o.forkPinned {
		if !p.Fork.IsPackTree() {
			kept = append(kept, p)
		}
	}
	o.forkPinned = dropOverriddenPins(append(kept, o.packTreePins(packs, rt)...), o.programOverridden)
}

// packTreeBuildable reports whether a build of a pack's tree can ever run for a jail on rt with this
// yolo: false on macos-user, whose darwin sealed act does not stage a pack's tree yet
// (docs/design/pack-buildable-programs-plan.md Task 10). That is a fixed fact of this binary, never
// a busy builder, so the prebuilt standing in for it (OQ-BG2's leaning) does not make the bytes
// depend on timing.
func packTreeBuildable(rt string) bool {
	return rt != "macos-user" // parity: Warned — the darwin act builds no pack tree yet (Task 10); a declared darwin prebuilt stands in, and without one the launch names why
}
