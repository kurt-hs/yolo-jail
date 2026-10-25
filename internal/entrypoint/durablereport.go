package entrypoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/durable"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// ReportDurableDir is the launch line for the durable dir (docs/reference/jail-home.md#storage-classes-and-the-durable-dir
// §5.4, OQ-DS2): yolo never deletes a worktree you made there (only clean, idle ones a pack's
// tool left, DS-P4), so every fresh launch says how many
// there are, how big they are and how long the oldest has sat idle — and, in a second line,
// how many worktree registrations point at directories that are gone, which is the /tmp
// incident class caught at the next launch.
//
// COMPUTED HERE, IN THE JAIL, once per fresh launch: by the container's main process as it
// boots, and by the macos-user bootstrap, which has no attach. An attach DOES run the
// entrypoint, as every session of a hold-main jail does, the first included, and its pass
// skips this step (the table's notSessionPass), since the launch already said it and the walk
// was every pass's largest step (DS-D11). Here the registrations may be checked against the
// filesystem the agent sees, and the host never stats a path a jail-written admin file named
// (DS-D3). It never refuses or fails the boot, and the size walk stops at durable.WalkBudget.
// A launch with no durable dir exported no variable, and the launcher has already said why.
func ReportDurableDir(e *Env) { reportDurableDir(e, time.Now) }

func reportDurableDir(e *Env, now func() time.Time) {
	dir := e.Getenv(durable.EnvVar)
	if dir == "" {
		e.note("durable dir: none this launch ($" + durable.EnvVar + " unset)")
		return
	}
	ws := e.WorkspaceDir()
	// A worktree made on the host records the host's spelling of the workspace; in a
	// container jail that is YOLO_HOST_DIR, and the same directory is /workspace here. One made
	// in a container jail records /workspace/…, which on macos-user is the real path.
	aliases := map[string]string{"/workspace": ws}
	if host := e.Getenv("YOLO_HOST_DIR"); host != "" && host != ws {
		aliases[host] = ws
	}
	// A registration is judged gone only below the workspace or the per-launch set, which is
	// where a restart deletes trees: one the host made beside the repository is merely
	// invisible from a container jail, and calling it gone at every launch was a false alarm.
	sc := durable.ScanDir(durable.ScanOptions{Workspace: ws, Aliases: aliases,
		GoneRoots: append([]string{ws}, paths.ScratchDests()...)})
	sz, err := durable.Measure(ws, durable.WalkBudget, now)
	if err == nil {
		// A walk that finished is the true total, kept for the launch whose walk will not. One
		// that stopped is shown beside the last complete count, and a count with no budget
		// starts in the background for the next launch, so boot never waits on it.
		if !sz.Partial {
			_ = durable.RecordSize(ws, sz, now())
		} else {
			if rec, ok := durable.LastSize(ws); ok {
				sz.Last = rec
			}
			sz.Counting = startDurableCount(e, ws)
		}
	}
	lines := durable.LaunchLines(sc, sz, err, durable.WalkBudget, now())
	for _, l := range lines {
		e.warn(l)
	}
	if len(lines) == 0 {
		e.note("durable dir: " + dir + " holds nothing to report")
	}
}

// startDurableCountFn spawns `yolo-jaild durable-size` detached, its output appended to
// logPath; a seam for tests.
var startDurableCountFn = func(bin, ws, logPath string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(bin, "durable-size", "--workspace", ws)
	cmd.Env = os.Environ()
	cmd.Stdout, cmd.Stderr = log, log
	// Its own process group, so a Ctrl-C meant for the jail's foreground is not its.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped, as the root watcher is (nixrootwatcher.go): a released child of the jail's main
	// process would stay a zombie for the jail's life.
	go func() { _ = cmd.Wait() }()
	return nil
}

// startDurableCount starts the background count and reports whether it did. A jail without
// yolo-jaild, or one that cannot start it, shows the floor and the last count alone.
func startDurableCount(e *Env, ws string) bool {
	bin, err := lookJaild("yolo-jaild")
	if err != nil {
		return false
	}
	logPath := filepath.Join(e.Home, filepath.FromSlash(paths.JailDaemonLogsRel()), "durable-size.log")
	if err := startDurableCountFn(bin, ws, logPath); err != nil {
		e.note("durable dir: the background size count did not start: " + err.Error())
		return false
	}
	return true
}
