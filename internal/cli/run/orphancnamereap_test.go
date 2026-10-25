package run

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// Every per-name file this package writes must parse as its cname's in the sweep's directory, or
// it leaks again with every sweep test green. A writer that changes its spelling, or a new
// per-name file, fails here.
func TestEveryPerNameWriterParsesForTheOrphanSweep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const c = "yolo-some-workspace-0123abcd"
	dirs := prune.OrphanCnameDirsUnder(paths.GlobalStorage(), paths.BuildDir(), paths.ContainerDir())
	type spelled struct{ class, dir, path string }
	cases := []spelled{
		{prune.OrphanClassLocks, dirs.Locks, launchLockPath(c)},
		{prune.OrphanClassLocks, dirs.Locks, livenessLockPath(c)},
		{prune.OrphanClassLocks, dirs.Locks, sessionLockPath(c)},
		{prune.OrphanClassLocks, dirs.Locks, hostDaemonSpawnGuardPath(c)},
		{prune.OrphanClassOwners, dirs.Owners, ownerPIDFile(c)},
		{prune.OrphanClassLogs, dirs.Logs, keeperLogPath(c)},
		{prune.OrphanClassLogs, dirs.Logs, socatLogPath(c)},
		{prune.OrphanClassLogs, dirs.Logs, socatLogPath(c) + ".1"},
		{prune.OrphanClassLogs, dirs.Logs, socatLogPath(c) + ".lock"},
	}
	for _, notch := range append([]string{""}, macosUserKeeperNotches...) {
		key := keeperKey(c, notch)
		cases = append(cases,
			spelled{prune.OrphanClassLocks, dirs.Locks, livenessLockPath(key)},
			spelled{prune.OrphanClassLocks, dirs.Locks, arrivalLockPath(key)},
			spelled{prune.OrphanClassLocks, dirs.Locks, keyedSessionsDir(key)},
		)
	}
	for _, sp := range cases {
		if filepath.Dir(sp.path) != sp.dir {
			t.Errorf("%s is written outside the sweep's %s directory %s", sp.path, sp.class, sp.dir)
			continue
		}
		if got, ok := prune.OrphanCnameOf(sp.class, filepath.Base(sp.path)); !ok || got != c {
			t.Errorf("%s does not parse as %s's %s file (%q, %v): the orphan sweep would never remove it",
				filepath.Base(sp.path), c, sp.class, got, ok)
		}
	}
}

// orphanAt writes one aged per-name file set for cname under the HOME's global storage and
// returns its paths.
func orphanAt(t *testing.T, cname string, at time.Time) []string {
	t.Helper()
	files := []string{launchLockPath(cname), livenessLockPath(cname), sessionLockPath(cname), ownerPIDFile(cname),
		keeperLogPath(cname)}
	for _, p := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// The slot's wiring, by behavior: an orphan's per-name state is removed through the slot's
// guard, the launching jail's own name is kept though nothing lists or tracks it yet, and a
// runtime that cannot be asked declines unstamped.
func TestReapOrphanCnameStateDeletesThroughTheGuardAndSparesTheLaunch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	o := &Options{}
	fillDefaults(o)
	o.Now = time.Now
	o.Getenv = func(string) string { return "" }
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		if len(argv) > 2 && argv[1] == "ps" && argv[2] == "-a" {
			return ExecResult{Ran: true, RC: 0} // podman answers: no containers at all
		}
		return ExecResult{Ran: false}
	}
	old := time.Now().Add(-prune.OrphanCnameAgeFloor - 24*time.Hour)
	orphan := orphanAt(t, "yolo-orphan-11111111", old)
	launching := orphanAt(t, "yolo-launching-22222222", old)
	g := &recordingGuard{}
	o.reapOrphanCnameState("podman", "yolo-launching-22222222", g.guard())

	if g.calls != len(orphan) {
		t.Errorf("%d deletions went through the guard, want %d", g.calls, len(orphan))
	}
	for _, p := range orphan {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("orphan file %s kept (%v)", filepath.Base(p), err)
		}
	}
	for _, p := range launching {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("the launching jail's %s was removed (%v)", filepath.Base(p), err)
		}
	}
	if due, _ := o.classDebounce("orphan-cname-state"); due {
		t.Error("a completed pass did not stamp its debounce")
	}
}

func TestReapOrphanCnameStateDeclinesUnstampedWhenTheRuntimeCannotBeAsked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	o := &Options{}
	fillDefaults(o)
	o.Now = time.Now
	o.Getenv = func(string) string { return "" }
	o.Exec = func([]string, string, []string, time.Duration) ExecResult { return ExecResult{Ran: false} }
	orphan := orphanAt(t, "yolo-orphan-11111111", time.Now().Add(-prune.OrphanCnameAgeFloor-24*time.Hour))
	o.reapOrphanCnameState("podman", "", nil)
	for _, p := range orphan {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s removed under unknown liveness (%v)", filepath.Base(p), err)
		}
	}
	if due, _ := o.classDebounce("orphan-cname-state"); !due {
		t.Error("a declined pass stamped its debounce")
	}
}
