package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// agentLogAgeDays is the age past which the launch's slot removes an agent log file: `yolo
// prune`'s own default for the same class (its --cache-age), so both reclaimers keep the same
// month of logs.
const agentLogAgeDays = 30

// reapWorkspaceAgentLogs is the agent-log class on the launch path, for THIS launch's
// workspace (docs/reference/disk-footprint.md "Lifecycle sweeps"): the allowlisted log
// directories in its overlay home (prune.AgentLogSubdirs — copilot, gemini, pi and codex
// logs, never a session transcript), each file older than agentLogAgeDays removed under the
// shared lock.
//
// THE LAUNCHING WORKSPACE IS ALWAYS KNOWN, so this needs no registry and no runtime: `yolo
// prune` sweeps only the workspaces a live container names (prune.FindYoloWorkspaces), and a
// workspace whose jail was never running when someone ran it grew its logs without bound.
//
// NO DEBOUNCE STAMP: the walk covers a handful of small log directories of one workspace, and
// the class stamps are the machine's (classDebounce), so a daily stamp would sweep only the
// first workspace launched each day. The shared cache's gemini-cli logs stay a `yolo prune`
// class: ~/.cache is the offered tier's (measureAndPurgeCache).
//
// Host-only: in a jail the overlay is the enclosing jail's own home, its host's to sweep.
func (o *Options) reapWorkspaceAgentLogs(guard prune.Guard) {
	if o.inJail() || o.Workspace == "" {
		return
	}
	_, files := prune.PurgeAgentLogsGuarded([]string{o.Workspace}, "", agentLogAgeDays, true, o.Now(), guard)
	if files > 0 {
		o.housekeepingNote("agent logs: removed %d log file(s) older than %d days from %s",
			files, agentLogAgeDays, paths.WorkspaceStateDir(o.Workspace)+"/home")
	}
}
