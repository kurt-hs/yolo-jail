package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// reapOrphanCnameState is the housekeeping slot's run of the per-container-name state sweep
// (prune.PruneOrphanCnameStateGuarded, whose file header carries the rules): the lock files,
// keeper and sidecar logs, owner files, current-image pointers and approval records every
// distinct container name leaves under global storage.
//
// THE KNOWN SET IS every container the runtime lists IN ANY STATE ∪ tracked ∪ this launch's own
// name, plus every live host-services session's key. Any state, unlike the agent-staging sweep's
// live set, because a stopped container still owns its name. This launch's name for the reason
// reapSmallAutomaticClasses gives: its container does not exist yet while the slot runs.
//
// Tri-state like every class here: a runtime or a session listing that cannot be read declines
// the pass, unstamped, so the next launch retries. Host-only and behind the same opt-out as the
// other automatic classes: in a jail, the runtime is the nested one, and `yolo prune --apply`
// runs the same sweep by hand.
func (o *Options) reapOrphanCnameState(rt, launchingCname string, guard prune.Guard) {
	if o.inJail() || o.Getenv(autoReapOptOutEnv) != "" {
		return
	}
	due, done := o.classDebounce("orphan-cname-state")
	if !due {
		return
	}
	all := prune.AllYoloContainerNames(rt, o.pruneRunFunc())
	if !all.Known {
		return
	}
	sessionKeys, ok := prune.LiveSessionKeys(paths.HostServicesBase(o.IsMacOS))
	if !ok {
		return
	}
	known := prune.TrackedContainerNames(paths.ContainerDir())
	for name := range all.Names {
		known[name] = struct{}{}
	}
	if launchingCname != "" {
		known[launchingCname] = struct{}{}
	}
	dirs := prune.OrphanCnameDirsUnder(paths.GlobalStorage(), paths.BuildDir(), paths.ContainerDir())
	rep := prune.PruneOrphanCnameStateGuarded(dirs, known, sessionKeys, true, true, o.Now(), guard)
	done()
	if rep.Files > 0 || rep.Busy > 0 {
		o.housekeepingNote("orphaned per-jail state: removed %d file(s) of %d container name(s), %d busy lock(s) kept",
			rep.Files, len(rep.Names), rep.Busy)
	}
}
