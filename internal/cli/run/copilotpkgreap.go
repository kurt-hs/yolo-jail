package run

import (
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// CopilotPkgHousekeepingClass is the Copilot self-update store's class (reapCopilotPkgVersions),
// by its debounce name.
const CopilotPkgHousekeepingClass = "copilot-pkg"

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
// THE CONTAINER SLOT ONLY. The macos-user slot asks no runtime, by design, and this class cannot
// judge without one; the container launches that fill the shared cache run it.
func (o *Options) reapCopilotPkgVersions(rt string, guard prune.Guard) {
	if o.inJail() {
		return // the host cache is the host's to sweep
	}
	if o.Getenv(autoReapOptOutEnv) != "" {
		return
	}
	due, done := o.classDebounce(CopilotPkgHousekeepingClass)
	if !due {
		return
	}
	res := prune.PruneCopilotPkgVersions(filepath.Join(paths.GlobalStorage(), "cache"),
		prune.CopilotPkgKeep, prune.CopilotPkgGrace, o.Now(),
		func() prune.JailStarts { return prune.RunningJailStarts(rt, o.pruneRunFunc()) }, true, guard)
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
