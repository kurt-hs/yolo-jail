package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// orphanCnameClass is the per-container-name state sweep's debounce class.
const orphanCnameClass = "orphan-cname-state"

// orphanSessionKeys lists the live host-services sessions' keys; a var so a test can make the
// listing unreadable.
var orphanSessionKeys = prune.LiveSessionKeys

// reapOrphanCnameState is the container housekeeping slot's run of the per-container-name state
// sweep (prune.PruneOrphanCnameStateGuarded, whose file header carries the rules): the lock files,
// keeper and sidecar logs, owner files, current-image pointers and approval records every
// distinct container name leaves under global storage. rt is the launch's runtime.
//
// THE KNOWN SET IS every container the runtime lists IN ANY STATE ∪ tracked ∪ this launch's own
// name, plus every live host-services session's key. Any state, unlike the agent-staging sweep's
// live set, because a stopped container still owns its name. This launch's name for the reason
// reapSmallAutomaticClasses gives: its container does not exist yet while the slot runs.
//
// IT RUNS IN A JAIL TOO, unlike the other small classes. A jail's global storage is the jail's
// own, written by the launches nested inside it, and the nested runtime is the one that knows
// those names, so the store and the authority over it are the same jail's. That is where the
// leak was measured: the integration suite and nested launches of a development jail.
//
// Tri-state like every class here: a runtime or a session listing that cannot be read declines
// the pass, unstamped, so the next launch retries. YOLO_NO_AUTO_IMAGE_REAP turns it off with the
// other automatic classes, and `yolo prune --apply` runs the same sweep by hand.
func (o *Options) reapOrphanCnameState(rt, launchingCname string, guard prune.Guard) {
	o.sweepOrphanCnameState([]string{rt}, launchingCname, guard)
}

// reapOrphanCnameStateMacosUser is the macos-user slot's run of the same sweep. That backend has
// no container, so its known set is the tracking files and the sessions, as `yolo prune` reads it
// there (BF-D2); a container runtime installed beside it (podman or Apple Container on PATH) adds
// its any-state listing, and one that is present but cannot be asked declines the pass.
func (o *Options) reapOrphanCnameStateMacosUser(guard prune.Guard) {
	runtimes := prune.InstalledContainerRuntimes(func(rt string) bool {
		_, ok := o.LookPath(rt)
		return ok
	})
	o.sweepOrphanCnameState(runtimes, "", guard)
}

// sweepOrphanCnameState is the shared body: every runtime in runtimes must answer, or the pass
// declines unstamped.
func (o *Options) sweepOrphanCnameState(runtimes []string, launchingCname string, guard prune.Guard) {
	if o.Getenv(autoReapOptOutEnv) != "" {
		return
	}
	due, done := o.classDebounce(orphanCnameClass)
	if !due {
		return
	}
	listed, _, ok := prune.AllYoloContainerNamesOf(runtimes, o.pruneRunFunc())
	if !ok {
		return
	}
	known := prune.TrackedContainerNames(paths.ContainerDir())
	for name := range listed {
		known[name] = struct{}{}
	}
	sessionKeys, sessionsOK := orphanSessionKeys(paths.HostServicesBase(o.IsMacOS))
	if !sessionsOK {
		return
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
