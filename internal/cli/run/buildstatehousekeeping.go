package run

import (
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// buildstatehousekeeping.go is the slot half of two classes that used to run only
// from `yolo prune` or from nowhere (disk-footprint.md "Build-state links"): the
// build dir's GC-root links, and the embedded pack trees.

// BuildStateHousekeepingClass is the debounce stamp of reapBuildStateLinks.
const BuildStateHousekeepingClass = "build-state"

// EmbeddedPacksHousekeepingClass is the debounce stamp of reapEmbeddedPackTrees.
const EmbeddedPacksHousekeepingClass = "embedded-packs"

// reapBuildStateLinks removes the build dir's GC-root links nothing else removed:
// orphaned prefix roots, the retired jail-prefix-* out-links, aged image-copier links
// and aged package-profile roots (internal/prune/buildstate.go has each policy).
//
// IT RUNS BEFORE THE STORE-OUTPUT CLASS, and the order is the point: a prefix root or
// a retired out-link is what keeps a superseded install prefix out of that reaper's
// reach (an out-link is a nix GC root, so `nix store delete` refuses its target).
//
// Liveness is asked once. Unknown declines every liveness-gated part and leaves the
// class unstamped, so the next launch retries; the copier links need no liveness and
// are swept either way. Runs in a jail too: every link here lives in the jail's own
// build dir, and the runtime asked is the jail's own.
func (o *Options) reapBuildStateLinks(rt string, guard prune.Guard) {
	if o.Getenv(autoReapOptOutEnv) != "" {
		return
	}
	due, done := o.classDebounce(BuildStateHousekeepingClass)
	if !due {
		return
	}
	buildDir := paths.BuildDir()
	copiers := prune.PruneImageCopierLinks(buildDir, prune.BuildLinkRetention, true, o.Now(), guard)

	run := o.pruneRunFunc()
	live := prune.LiveYoloContainers(rt, run)
	sources, srcKnown, srcWhy := prune.LivePrefixSources(rt, live, prune.PrefixBinMountDest, run)
	var prefixRoots, outLinks []string
	if srcKnown {
		prefixRoots = prune.PruneOrphanPrefixRootsGuarded(filepath.Join(buildDir, "prefix-roots"),
			sources, srcKnown, true, o.Now(), guard)
		outLinks = prune.PruneRetiredPrefixOutLinks(buildDir, sources, srcKnown, true, guard)
	} else {
		o.housekeepingNote("build state: declined prefix roots and retired prefix out-links — %s; "+
			"a later launch retries, or run `yolo prune --apply`", srcWhy)
	}
	profiles, profKnown, profWhy := prune.LiveStoreProfiles(rt, live, run)
	var packageRoots []string
	if profKnown {
		packageRoots = prune.PrunePackageRoots(paths.PackageRootsDir(), profiles, profKnown,
			prune.BuildLinkRetention, true, o.Now(), guard)
	} else {
		o.housekeepingNote("build state: declined package-profile roots — %s; a later launch retries", profWhy)
	}
	if len(copiers)+len(prefixRoots)+len(outLinks)+len(packageRoots) > 0 {
		o.housekeepingNote("build state: removed %d prefix root(s), %d retired prefix out-link(s), "+
			"%d image-copier link(s), %d package-profile root(s) (store paths reclaimed by a later "+
			"store-output pass or nix GC)", len(prefixRoots), len(outLinks), len(copiers), len(packageRoots))
	}
	if srcKnown && profKnown {
		done()
	}
}

// reapEmbeddedPackTrees removes other builds' embedded pack trees no process holds a
// lease on (prune.PruneEmbeddedPackTrees: this build's tree is always kept, an unknown
// hash keeps every tree, a held or unaskable lease keeps its tree). Each yolo build
// leaves one tree, so a machine that installs often grows one per install.
//
// Every backend and in a jail: the trees live in the state dir of whoever ran the
// build, and the evidence is a lease lock, not a container runtime. The reaper renames
// a tree away under its own exclusive lease lock before deleting it, the race the
// shared housekeeping lock exists for elsewhere, so it takes no guard.
func (o *Options) reapEmbeddedPackTrees() {
	if o.Getenv(autoReapOptOutEnv) != "" {
		return
	}
	due, done := o.classDebounce(EmbeddedPacksHousekeepingClass)
	if !due {
		return
	}
	current, known := packload.EmbeddedHash()
	r := prune.PruneEmbeddedPackTrees(paths.EmbeddedPacksDir(), current, known, true, o.Now())
	if r.Declined != "" {
		o.housekeepingNote("embedded pack trees: declined — %s; a later launch retries", r.Declined)
		return
	}
	done()
	if n := r.RemovedCount(); n > 0 {
		o.housekeepingNote("embedded pack trees: reclaimed %s in %d tree(s) of other builds",
			prune.FmtBytes(r.Bytes), n)
	}
}
