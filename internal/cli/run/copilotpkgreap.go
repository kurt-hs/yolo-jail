package run

import (
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// CopilotPkgHousekeepingClass is the Copilot self-update store's class (reapCopilotPkgVersions),
// by its debounce name.
const CopilotPkgHousekeepingClass = "copilot-pkg"

// CopilotPkgMacosUserHousekeepingClass is the macos-user slot's class for its own Copilot store
// (reapMacosUserCopilotPkgVersions). Its own stamp, not the container class's: the two judge
// different stores, and on a Mac running both backends one slot's stamp must not hold back the
// other's store for a day.
const CopilotPkgMacosUserHousekeepingClass = "copilot-pkg-macos-user"

// reapCopilotPkgVersions bounds the shared cache's copilot/pkg store: past the newest
// prune.CopilotPkgKeep complete versions per platform, it removes each version a newer complete
// one superseded more than prune.CopilotPkgGrace ago, unless a running jail started before that
// (prune.PruneCopilotPkgVersions says why each rule is what it is).
//
// AUTOMATIC, like the other reapers on the slot and unlike the age purge beside it: its veto is the
// runtime's own list of running jails, and a removed version is one release download if Copilot
// ever wants it again. A runtime it cannot ask DECLINES the class: noted, not stamped, and the next
// launch asks again. Host-only, debounced to once a day, and it honors the automatic reapers'
// opt-out.
//
// The container slot judges the shared cache's store, which container jails fill, by the
// runtime's running jails. The macos-user slot runs the same rule on that backend's own store by
// its sessions (reapMacosUserCopilotPkgVersions).
//
// ON APPLE CONTAINER THE CLASS IS SKIPPED, not declined: yolo reads no start time from that
// runtime, so a decline would repeat at every launch with a next step that cannot succeed. The
// skip is noted once a day, under the class's own stamp, naming the documented gap.
func (o *Options) reapCopilotPkgVersions(rt string, guard prune.Guard) {
	if rt == "container" { // parity: Warned — yolo reads no Apple Container start time, so the Copilot store is not judged there; noted once a day, naming disk-footprint.md's open gap
		if o.inJail() || o.Getenv(autoReapOptOutEnv) != "" {
			return
		}
		if due, done := o.classDebounce(CopilotPkgHousekeepingClass); due {
			o.housekeepingNote("copilot: the self-update store is not judged on Apple Container, which yolo " +
				"reads no jail start time from yet; disk-footprint.md's open gap \"Parity gap: Apple " +
				"Container's Copilot self-update class\" is the fix and names the Mac check that unblocks it")
			done()
		}
		return
	}
	o.reapCopilotPkgStore(CopilotPkgHousekeepingClass, filepath.Join(paths.GlobalStorage(), "cache"),
		func() prune.JailStarts { return prune.RunningJailStarts(rt, o.pruneRunFunc()) }, guard)
}

// reapMacosUserCopilotPkgVersions is the class on the macos-user slot: macos-user's own store
// (prune.CopilotPkgCacheMacosUser, the run-as-you machine tier), its running jails the
// host-services sessions (prune.MacosUserJailStarts). The shared cache's store is not this
// slot's: macos-user runs none of the Linux releases in it, and this slot asks no container
// runtime, so the container slot judges that one.
func (o *Options) reapMacosUserCopilotPkgVersions(guard prune.Guard) {
	o.reapCopilotPkgStore(CopilotPkgMacosUserHousekeepingClass, prune.CopilotPkgCacheMacosUser(paths.GlobalStorageUnder(o.macosUserHostHome())),
		func() prune.JailStarts { return prune.MacosUserJailStarts(paths.HostServicesBase(o.IsMacOS)) }, guard)
}

// reapCopilotPkgStore is one store's pass under class's debounce.
func (o *Options) reapCopilotPkgStore(class, cacheRoot string, jails func() prune.JailStarts, guard prune.Guard) {
	if o.inJail() {
		return // the host cache is the host's to sweep
	}
	if o.Getenv(autoReapOptOutEnv) != "" {
		return
	}
	due, done := o.classDebounce(class)
	if !due {
		return
	}
	res := prune.PruneCopilotPkgVersions(cacheRoot, prune.CopilotPkgKeep, prune.CopilotPkgGrace, o.Now(), jails, true, guard)
	if res.Declined != "" {
		o.housekeepingNote("copilot: declined — %s; the next launch asks again, or `yolo prune --apply` "+
			"on a machine whose runtime can list its running jails", res.Declined)
		return // not stamped
	}
	if len(res.Removed) > 0 {
		o.housekeepingNote("copilot: reclaimed %s in %d superseded self-update version(s), keeping the newest %d",
			prune.FmtBytes(res.Bytes), len(res.Removed), prune.CopilotPkgKeep)
	}
	for _, v := range res.Failed {
		o.housekeepingNote("copilot: could not remove %s: %s; check its permissions, then the next launch "+
			"retries, or run `yolo prune --apply`", v.Display(), v.Err)
	}
	if len(res.Failed) == 0 {
		done()
	}
}
