package prune

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

const day = 24 * time.Hour

// copilotVersion writes cache/copilot/pkg/linux-x64/<v> with a file in it; extractedDaysAgo >= 0
// also writes its .extraction-complete marker, dated then, and < 0 leaves the version incomplete.
func copilotVersion(t *testing.T, cache, v string, extractedDaysAgo int) {
	t.Helper()
	dir := filepath.Join(cache, "copilot", "pkg", "linux-x64", v)
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "index.js"), []byte("0123456789"), 0o644))
	if extractedDaysAgo >= 0 {
		m := filepath.Join(dir, copilotPkgComplete)
		must(t, os.WriteFile(m, nil, 0o644))
		at := time.Now().Add(-time.Duration(extractedDaysAgo) * day)
		must(t, os.Chtimes(m, at, at))
	}
}

func noJails() JailStarts { return JailStarts{Known: true} }

func removedVersions(res CopilotPkgResult) []string {
	var got []string
	for _, v := range res.Removed {
		got = append(got, v.Version)
	}
	slices.Sort(got)
	return got
}

func copilotPresent(cache, v string) bool {
	return present(cache, "copilot/pkg/linux-x64/"+v+"/index.js")
}

// TestCopilotPkgKeepsTheNewestAndRemovesTheLongSuperseded is the keep-newest rule: the newest
// CopilotPkgKeep complete versions stay whatever their age (in release order, so 1.0.10 is newer
// than 1.0.9), a name that is not a release number stays, and every older version a newer complete
// one superseded more than the grace ago goes whole when no jail is running.
func TestCopilotPkgKeepsTheNewestAndRemovesTheLongSuperseded(t *testing.T) {
	for _, apply := range []bool{false, true} {
		cache := t.TempDir()
		for _, v := range []string{"1.0.10", "1.0.9", "1.0.8", "1.0.7", "1.0.5-1", "nightly"} {
			copilotVersion(t, cache, v, 60)
		}
		res := PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), noJails, apply, nil)
		if got := removedVersions(res); !slices.Equal(got, []string{"1.0.5-1", "1.0.7"}) || res.Declined != "" || len(res.Failed) != 0 || res.Bytes != 20 {
			t.Errorf("apply=%v: removed %v (%+v), want [1.0.5-1 1.0.7], 20 bytes", apply, got, res)
		}
		for _, v := range []string{"1.0.10", "1.0.9", "1.0.8", "nightly"} {
			if !copilotPresent(cache, v) {
				t.Errorf("apply=%v: %s was removed", apply, v)
			}
		}
		for _, v := range []string{"1.0.7", "1.0.5-1"} {
			if copilotPresent(cache, v) == apply {
				t.Errorf("apply=%v: %s present=%v", apply, v, !apply)
			}
		}
		if ents, _ := os.ReadDir(filepath.Join(cache, "copilot", "pkg", "linux-x64")); apply && len(ents) != 4 {
			t.Errorf("%d entries left, want the 4 kept and no tombstone", len(ents))
		}
	}
}

// TestCopilotPkgDoesNotCountAnIncompleteVersion: a version without .extraction-complete (an
// unpacking in progress) is neither one of the newest kept nor a version that supersedes another,
// so the three complete versions below it are all kept; an old incomplete one below them goes.
func TestCopilotPkgDoesNotCountAnIncompleteVersion(t *testing.T) {
	cache := t.TempDir()
	copilotVersion(t, cache, "1.0.12", -1) // unpacking now
	for _, v := range []string{"1.0.11", "1.0.10", "1.0.9", "1.0.8"} {
		copilotVersion(t, cache, v, 60)
	}
	copilotVersion(t, cache, "1.0.3", -1) // an unpacking that never finished
	res := PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), noJails, true, nil)
	if got := removedVersions(res); !slices.Equal(got, []string{"1.0.3", "1.0.8"}) {
		t.Errorf("removed %v, want [1.0.3 1.0.8]: the incomplete 1.0.12 took one of the kept places", got)
	}
	for _, v := range []string{"1.0.12", "1.0.11", "1.0.10", "1.0.9"} {
		if !copilotPresent(cache, v) {
			t.Errorf("%s was removed", v)
		}
	}
}

// TestCopilotPkgWaitsOutTheGrace: a version superseded only days ago is kept, whatever else.
func TestCopilotPkgWaitsOutTheGrace(t *testing.T) {
	cache := t.TempDir()
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2"} {
		copilotVersion(t, cache, v, 2)
	}
	copilotVersion(t, cache, "1.0.1", 60)
	res := PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), noJails, true, nil)
	if len(res.Removed) != 0 || !copilotPresent(cache, "1.0.1") {
		t.Errorf("removed %v: 1.0.1 was superseded 2 days ago, inside the grace", removedVersions(res))
	}
}

// TestCopilotPkgKeepsAVersionARunningJailMayUse is the liveness veto: a jail that started before
// the superseding version was extracted may be running the old one, so it stays; a jail started
// after it cannot be, so it goes.
func TestCopilotPkgKeepsAVersionARunningJailMayUse(t *testing.T) {
	for _, early := range []bool{true, false} {
		cache := t.TempDir()
		for _, v := range []string{"1.0.4", "1.0.3", "1.0.2"} {
			copilotVersion(t, cache, v, 30)
		}
		copilotVersion(t, cache, "1.0.1", 60)
		start := time.Now().Add(-10 * day)
		if early {
			start = time.Now().Add(-40 * day)
		}
		jails := func() JailStarts { return JailStarts{Known: true, Starts: []time.Time{start}} }
		res := PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), jails, true, nil)
		if copilotPresent(cache, "1.0.1") != early {
			t.Errorf("jail started before the superseding version=%v: 1.0.1 present=%v (removed %v)",
				early, !early, removedVersions(res))
		}
	}
}

// TestCopilotPkgDeclinesWhenTheRuntimeCannotBeAsked: with a candidate and no answer about the
// running jails, nothing is removed and the result says why; with no candidate the runtime is not
// asked at all.
func TestCopilotPkgDeclinesWhenTheRuntimeCannotBeAsked(t *testing.T) {
	cache := t.TempDir()
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, cache, v, 60)
	}
	unknown := func() JailStarts { return JailStarts{Why: "no answer"} }
	res := PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), unknown, true, nil)
	if res.Declined != "no answer" || len(res.Removed) != 0 || !copilotPresent(cache, "1.0.1") {
		t.Errorf("got %+v, want a decline and nothing removed", res)
	}

	quiet := t.TempDir()
	for _, v := range []string{"1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, quiet, v, 60)
	}
	asked := false
	res = PruneCopilotPkgVersions(quiet, 3, 7*day, time.Now(), func() JailStarts { asked = true; return JailStarts{} }, true, nil)
	if asked || res.Declined != "" {
		t.Errorf("asked=%v, declined=%q with nothing to judge: want the runtime left alone", asked, res.Declined)
	}
}

// TestRunningJailStartsReadsPodmanAndDeclinesOtherwise: podman's StartedAt (unix seconds) for the
// yolo-* containers; a yolo-* row without one, a failed listing, and a runtime that reports no
// start time all decline.
func TestRunningJailStartsReadsPodmanAndDeclinesOtherwise(t *testing.T) {
	run := func(out string, rc int) RunFunc {
		return func(argv []string, _ time.Duration) ProbeResult {
			if strings.Join(argv, " ") != "podman ps --format {{.Names}} {{.StartedAt}}" {
				t.Errorf("asked %q", argv)
			}
			return ProbeResult{Stdout: out, RC: rc, Ran: true}
		}
	}
	js := RunningJailStarts("podman", run("yolo-a 1792894046\nbuildkit 5\n\n", 0))
	if !js.Known || len(js.Starts) != 1 || js.Starts[0].Unix() != 1792894046 {
		t.Errorf("got %+v, want one start", js)
	}
	for name, got := range map[string]JailStarts{
		"no start time": RunningJailStarts("podman", run("yolo-a\n", 0)),
		"listing fails": RunningJailStarts("podman", run("", 125)),
		"apple":         RunningJailStarts("container", run("", 0)),
		"macos-user":    RunningJailStarts("macos-user", run("", 0)),
	} {
		if got.Known || got.Why == "" {
			t.Errorf("%s: got %+v, want a decline with a reason", name, got)
		}
	}
}

// TestCopilotPkgRechecksUnderTheGuard: a guard whose recheck refuses keeps the version.
func TestCopilotPkgRechecksUnderTheGuard(t *testing.T) {
	cache := t.TempDir()
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, cache, v, 60)
	}
	guard := Guard(func(recheck func() bool, del func()) bool {
		// A jail swapped the version for a link between the walk and the lock.
		dir := filepath.Join(cache, "copilot", "pkg", "linux-x64", "1.0.1")
		must(t, os.Rename(dir, dir+".real"))
		must(t, os.Symlink(dir+".real", dir))
		if !recheck() {
			return false
		}
		del()
		return true
	})
	res := PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), noJails, true, guard)
	if len(res.Removed) != 0 || !present(cache, "copilot/pkg/linux-x64/1.0.1.real/index.js") {
		t.Errorf("removed %v: the recheck under the lock did not hold", removedVersions(res))
	}
}

// TestCopilotIsStillForbiddenToTheAgePurge: the age purge must never walk copilot/, whose files
// carry the release's 1985 timestamps; the keep-newest rule is its only reclaimer.
func TestCopilotIsStillForbiddenToTheAgePurge(t *testing.T) {
	cache := t.TempDir()
	copilotVersion(t, cache, "1.0.1", 60)
	old := time.Now().Add(-60 * day)
	must(t, os.Chtimes(filepath.Join(cache, "copilot/pkg/linux-x64/1.0.1/index.js"), old, old))
	if _, files := PurgeCacheByAge(cache, []string{"copilot"}, nil, 30, true, time.Now()); files != 0 {
		t.Errorf("the age purge removed %d files under copilot/", files)
	}
}

// TestManualPruneRunsTheCopilotSection pins `yolo prune`'s call site: --apply removes a long
// superseded version when podman lists no running jail, and the report names it.
func TestManualPruneRunsTheCopilotSection(t *testing.T) {
	o, gs := baseOpts(t)
	o.Apply = true
	o.Now = time.Now
	cache := filepath.Join(gs, "cache")
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, cache, v, 60)
	}
	var buf strings.Builder
	o.Out = &buf
	Run(o)
	if copilotPresent(cache, "1.0.1") || !strings.Contains(buf.String(), "removed linux-x64/1.0.1") {
		t.Errorf("`yolo prune --apply` did not reap 1.0.1:\n%s", buf.String())
	}
}

// plantDatedSession makes a macos-user session dir under base whose dir, record and lock are all
// dated started; live holds its lock for the rest of the test, and !live leaves it free (gone).
func plantDatedSession(t *testing.T, base string, live bool, notch string, started time.Time) {
	t.Helper()
	dir, err := os.MkdirTemp(base, paths.HostServicesSessionPrefix("yolo-ws"))
	must(t, err)
	must(t, runtime.WriteSessionRecord(dir, runtime.SessionRecord{Notch: notch}))
	lock := filepath.Join(dir, paths.HostServicesSessionLockName)
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600)
	must(t, err)
	t.Cleanup(func() { _ = f.Close() })
	if live {
		must(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	}
	for _, p := range []string{lock, filepath.Join(dir, runtime.SessionRecordName), dir} {
		must(t, os.Chtimes(p, started, started))
	}
}

// TestMacosUserJailStartsReadsTheLiveSessions: a live macos-user session's start is the earliest
// of its dir, record and lock times; a gone session and a `yolo host` session are not jails.
func TestMacosUserJailStartsReadsTheLiveSessions(t *testing.T) {
	base := redirectSessions(t)
	early := time.Now().Add(-40 * day).Truncate(time.Second)
	plantDatedSession(t, base, true, runtime.NotchMacosUser, early)
	plantDatedSession(t, base, false, runtime.NotchMacosUser, time.Now().Add(-80*day))
	plantDatedSession(t, base, true, runtime.NotchHost, time.Now().Add(-80*day))
	js := MacosUserJailStarts(base)
	if !js.Known || len(js.Starts) != 1 || !js.Starts[0].Equal(early) {
		t.Errorf("got %+v, want the one live macos-user session, started %v", js, early)
	}
}

// TestCopilotPkgOnMacosUserKeepsAVersionALiveSessionMayUse is the veto through the session reader:
// a live session older than the superseding extraction keeps the version; with none it goes.
func TestCopilotPkgOnMacosUserKeepsAVersionALiveSessionMayUse(t *testing.T) {
	for _, early := range []bool{false, true} {
		base := redirectSessions(t)
		if early {
			plantDatedSession(t, base, true, runtime.NotchMacosUser, time.Now().Add(-90*day))
		}
		cache := t.TempDir()
		for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
			copilotVersion(t, cache, v, 60)
		}
		PruneCopilotPkgVersions(cache, 3, 7*day, time.Now(), func() JailStarts { return MacosUserJailStarts(base) }, true, nil)
		if copilotPresent(cache, "1.0.1") != early {
			t.Errorf("session before the superseding version=%v: 1.0.1 present=%v", early, !early)
		}
	}
}

// TestManualPruneOnMacosUserJudgesItsOwnStore pins `yolo prune`'s macos-user call site: it reaps
// macos-user's own store, by its sessions, and leaves the shared cache's Linux store alone.
func TestManualPruneOnMacosUserJudgesItsOwnStore(t *testing.T) {
	redirectSessions(t)
	o, gs := baseOpts(t)
	o.Apply = true
	o.Now = time.Now
	o.DetectRuntime = func() string { return macosUserRuntime }
	o.Exec = noMacosUserExec(t)
	own := CopilotPkgCacheMacosUser(gs)
	shared := filepath.Join(gs, "cache")
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, own, v, 60)
		copilotVersion(t, shared, v, 60)
	}
	var buf strings.Builder
	o.Out = &buf
	Run(o)
	if copilotPresent(own, "1.0.1") || !strings.Contains(buf.String(), "removed linux-x64/1.0.1") {
		t.Errorf("`yolo prune --apply` on macos-user did not reap its own store's 1.0.1:\n%s", buf.String())
	}
	if !copilotPresent(shared, "1.0.1") {
		t.Errorf("`yolo prune` on macos-user removed from the shared cache's store")
	}
}
