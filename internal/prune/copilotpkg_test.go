package prune

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// copilotVersion writes cache/copilot/pkg/linux-x64/<v> with a file, its directory and file
// modified ageDays ago and the file last read readDays ago.
func copilotVersion(t *testing.T, cache, v string, ageDays, readDays int) {
	t.Helper()
	dir := filepath.Join(cache, "copilot", "pkg", "linux-x64", v)
	must(t, os.MkdirAll(dir, 0o755))
	f := filepath.Join(dir, "index.js")
	must(t, os.WriteFile(f, []byte("0123456789"), 0o644))
	now := time.Now()
	mod := now.Add(-time.Duration(ageDays) * 24 * time.Hour)
	read := now.Add(-time.Duration(readDays) * 24 * time.Hour)
	must(t, os.Chtimes(f, read, mod))
	must(t, os.Chtimes(dir, mod, mod))
}

// TestCopilotPkgKeepsTheNewestAndTheRecentlyUsed is the keep-newest rule for Copilot's
// self-update store: the newest CopilotPkgKeep versions stay whatever their age (in release
// order, so 1.0.10 is newer than 1.0.9), an older version some jail read within the window stays,
// a name that is not a release number stays, and every other old version goes whole.
func TestCopilotPkgKeepsTheNewestAndTheRecentlyUsed(t *testing.T) {
	for _, apply := range []bool{false, true} {
		cache := t.TempDir()
		copilotVersion(t, cache, "1.0.10", 60, 60) // newest by release order, old by time: kept
		copilotVersion(t, cache, "1.0.9", 60, 60)
		copilotVersion(t, cache, "1.0.8", 60, 60)
		copilotVersion(t, cache, "1.0.7", 60, 60) // past the keep, unused: removed
		copilotVersion(t, cache, "1.0.6", 60, 2)  // past the keep, read 2 days ago: kept
		copilotVersion(t, cache, "1.0.5-1", 60, 60)
		copilotVersion(t, cache, "nightly", 60, 60) // cannot be ordered: kept

		removed, failed, bytes := PruneCopilotPkgVersions(cache, 3, 7*24*time.Hour, time.Now(), apply, nil)
		var got []string
		for _, v := range removed {
			got = append(got, v.Version)
		}
		slices.Sort(got)
		if !slices.Equal(got, []string{"1.0.5-1", "1.0.7"}) || len(failed) != 0 || bytes != 20 {
			t.Errorf("apply=%v: removed %v (failed %v, %d bytes), want [1.0.5-1 1.0.7], 20 bytes", apply, got, failed, bytes)
		}
		for _, v := range []string{"1.0.10", "1.0.9", "1.0.8", "1.0.6", "nightly"} {
			if !present(cache, "copilot/pkg/linux-x64/"+v+"/index.js") {
				t.Errorf("apply=%v: %s was removed", apply, v)
			}
		}
		for _, v := range []string{"1.0.7", "1.0.5-1"} {
			if present(cache, "copilot/pkg/linux-x64/"+v) == apply {
				t.Errorf("apply=%v: %s present=%v", apply, v, !apply)
			}
		}
		if ents, _ := os.ReadDir(filepath.Join(cache, "copilot", "pkg", "linux-x64")); apply && len(ents) != 5 {
			t.Errorf("%d entries left, want the 5 kept and no tombstone", len(ents))
		}
	}
}

// TestCopilotPkgRechecksUnderTheGuard: a version some jail started reading between the walk and
// the removal is kept, because the guard's recheck reads its last use again.
func TestCopilotPkgRechecksUnderTheGuard(t *testing.T) {
	cache := t.TempDir()
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, cache, v, 60, 60)
	}
	guard := Guard(func(recheck func() bool, del func()) bool {
		f := filepath.Join(cache, "copilot", "pkg", "linux-x64", "1.0.1", "index.js")
		now := time.Now()
		must(t, os.Chtimes(f, now, now.Add(-60*24*time.Hour))) // read just now
		if !recheck() {
			return false
		}
		del()
		return true
	})
	removed, _, _ := PruneCopilotPkgVersions(cache, 3, 7*24*time.Hour, time.Now(), true, guard)
	if len(removed) != 0 || !present(cache, "copilot/pkg/linux-x64/1.0.1/index.js") {
		t.Errorf("removed %v: a version read under the lock was removed", removed)
	}
}

// TestCopilotIsStillForbiddenToTheAgePurge: the age purge must never walk copilot/, whose files
// carry the release's 1985 timestamps; the keep-newest rule is its only reclaimer.
func TestCopilotIsStillForbiddenToTheAgePurge(t *testing.T) {
	cache := t.TempDir()
	copilotVersion(t, cache, "1.0.1", 60, 60)
	if _, files := PurgeCacheByAge(cache, []string{"copilot"}, nil, 30, true, time.Now()); files != 0 {
		t.Errorf("the age purge removed %d files under copilot/", files)
	}
}

// TestCopilotPkgIgnoresADirectorysAccessTime: listing a directory moves its access time, this
// reaper's own walk included, so a directory's access time is no evidence of use. A version whose
// only fresh time is a subdirectory's access time is removed.
func TestCopilotPkgIgnoresADirectorysAccessTime(t *testing.T) {
	cache := t.TempDir()
	for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
		copilotVersion(t, cache, v, 60, 60)
	}
	sub := filepath.Join(cache, "copilot", "pkg", "linux-x64", "1.0.1", "assets")
	must(t, os.MkdirAll(sub, 0o755))
	old := time.Now().Add(-60 * 24 * time.Hour)
	must(t, os.Chtimes(sub, time.Now(), old))
	must(t, os.Chtimes(filepath.Dir(sub), old, old))
	removed, _, _ := PruneCopilotPkgVersions(cache, 3, 7*24*time.Hour, time.Now(), true, nil)
	if len(removed) != 1 || removed[0].Version != "1.0.1" {
		t.Errorf("removed %v, want 1.0.1: a directory's access time was read as a use", removed)
	}
}
