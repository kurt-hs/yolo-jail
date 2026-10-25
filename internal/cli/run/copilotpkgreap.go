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
// prune.CopilotPkgKeep versions per platform, it removes every version no jail has used for
// prune.CopilotPkgRecent (prune.PruneCopilotPkgVersions says why each rule is what it is).
//
// AUTOMATIC, like the other reapers on the slot and unlike the age purge beside it: a removed
// version is one release download if Copilot ever wants it again, and the version a launched
// copilot resolves to (the newest) is never a candidate. Host-only, debounced to once a day, and
// it honors the automatic reapers' opt-out.
func (o *Options) reapCopilotPkgVersions(guard prune.Guard) {
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
	removed, failed, bytes := prune.PruneCopilotPkgVersions(filepath.Join(paths.GlobalStorage(), "cache"),
		prune.CopilotPkgKeep, prune.CopilotPkgRecent, o.Now(), true, guard)
	if len(removed) > 0 {
		o.housekeepingNote("copilot: reclaimed %s in %d old self-update version(s), keeping the newest %d "+
			"and any used in the last %d days", prune.FmtBytes(bytes), len(removed), prune.CopilotPkgKeep,
			int(prune.CopilotPkgRecent.Hours()/24))
	}
	for _, v := range failed {
		o.housekeepingNote("copilot: could not remove %s: %s; the next launch retries, or run `yolo prune --apply`",
			v.Display(), v.Err)
	}
	if len(failed) == 0 {
		done()
	}
}
