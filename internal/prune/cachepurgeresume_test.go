package prune

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// tickClock replaces purgeClock with one that advances a second at every read, so a deadline
// a few ticks away stops a walk part-way through with no real time passing.
func tickClock(t *testing.T) func() time.Time {
	t.Helper()
	base := time.Now()
	ticks := 0
	orig := purgeClock
	purgeClock = func() time.Time { ticks++; return base.Add(time.Duration(ticks) * time.Second) }
	t.Cleanup(func() { purgeClock = orig })
	return purgeClock
}

// TestTheCachePurgeResumesAcrossPassesAndCoversEverySubdir is the defect this cursor fixes: a
// budget too small for the whole list used to restart every pass at the first subdir, so the
// subdirs after it were never reached. With the cursor each pass goes on where the last one
// stopped, the measurement adds up over the cycle to the whole figure, and the removal reaches
// every subdir, the last ones included.
func TestTheCachePurgeResumesAcrossPassesAndCoversEverySubdir(t *testing.T) {
	subdirs := []string{"uv", "pip", "npm", "pex", "mise"} // no unit at depth 2 in any: every file stands alone
	var rels []string
	for _, sub := range subdirs {
		for i := 0; i < 4; i++ {
			rels = append(rels, sub+"/d"+strconv.Itoa(i%2)+"/f"+strconv.Itoa(i))
		}
	}

	for _, apply := range []bool{false, true} {
		root := t.TempDir()
		agedTree(t, root, rels...)
		clock := tickClock(t)

		var progress CachePurgeProgress
		covered := map[string]bool{}
		removed, passes := 0, 0
		var total int
		for {
			passes++
			if passes > 100 {
				t.Fatalf("apply=%v: no full cycle after %d passes; the cursor is not moving", apply, passes)
			}
			from := progress.Measure
			if apply {
				from = progress.Apply
			}
			res := PurgeCacheByAgeFrom(root, subdirs, nil, 30, apply, time.Now(), nil, clock().Add(10*time.Second), from)
			for _, s := range res.Covered {
				covered[s] = true
			}
			if res.Partial && res.Next == from && res.Files == 0 {
				t.Fatalf("apply=%v: pass %d stopped where it started (%+v) and did nothing", apply, passes, from)
			}
			removed += res.Files
			if apply {
				progress.Apply = res.Next
				if !res.Partial {
					break
				}
				continue
			}
			_, files, complete := progress.AddMeasurement(res)
			if complete {
				total = files
				break
			}
		}
		if passes < 2 {
			t.Fatalf("apply=%v: one pass covered everything; the budget is not small enough to test resuming", apply)
		}
		for _, s := range subdirs {
			if !covered[s] {
				t.Errorf("apply=%v: no pass ever reached %s", apply, s)
			}
		}
		if !apply && total != len(rels) {
			t.Errorf("the cycle's measurement totalled %d files, want %d", total, len(rels))
		}
		if !progress.Measure.IsStart() || !progress.Apply.IsStart() {
			t.Errorf("apply=%v: a finished cycle left a cursor: %+v", apply, progress)
		}
		if apply {
			if removed != len(rels) {
				t.Errorf("removed %d files over the cycle, want %d", removed, len(rels))
			}
			for _, rel := range rels {
				if present(root, rel) {
					t.Errorf("%s survived a full removal cycle", rel)
				}
			}
		}
	}
}

// TestACursorOnAnUnlistedSubdirStartsOver: a cursor naming a subdir that left the list (or the
// heavy subdirs a `yolo prune --purge-heavy-caches` added) restarts the cycle rather than
// skipping everything.
func TestACursorOnAnUnlistedSubdirStartsOver(t *testing.T) {
	root := t.TempDir()
	agedTree(t, root, "uv/a", "pip/b")
	res := PurgeCacheByAgeFrom(root, []string{"uv", "pip"}, nil, 30, false, time.Now(), nil, time.Time{},
		CachePurgeCursor{Sub: "huggingface", Path: "x"})
	if res.Files != 2 || res.Partial || !slices.Equal(res.Covered, []string{"uv", "pip"}) {
		t.Errorf("got %+v, want both subdirs walked from the start", res)
	}
}

// TestAResumedWalkSkipsWhatThePassBeforeFinished: the cursor's path skips, level by level, the
// names that sort before it, and walks the rest of the subdir and every subdir after it.
func TestAResumedWalkSkipsWhatThePassBeforeFinished(t *testing.T) {
	root := t.TempDir()
	agedTree(t, root, "uv/a/1", "uv/b/1", "uv/b/2", "uv/c/1", "pip/x")
	res := PurgeCacheByAgeFrom(root, []string{"uv", "pip"}, nil, 30, true, time.Now(), nil, time.Time{},
		CachePurgeCursor{Sub: "uv", Path: "b/2"})
	for rel, want := range map[string]bool{"uv/a/1": true, "uv/b/1": true, "uv/b/2": false, "uv/c/1": false, "pip/x": false} {
		if present(root, rel) != want {
			t.Errorf("%s present=%v, want %v", rel, !want, want)
		}
	}
	if res.Files != 3 || res.Partial || !res.Next.IsStart() {
		t.Errorf("got %+v, want 3 files and a finished cycle", res)
	}
}

// agedUnit writes files under dir (relative to root), each with its own age in days, and then
// ages the directories on the way by dirDays.
func agedUnit(t *testing.T, root, dir string, dirDays int, files map[string]int) {
	t.Helper()
	now := time.Now()
	for rel, days := range files {
		p := filepath.Join(root, filepath.FromSlash(dir), filepath.FromSlash(rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte("xx"), 0o600))
		at := now.Add(-time.Duration(days) * 24 * time.Hour)
		must(t, os.Chtimes(p, at, at))
	}
	at := now.Add(-time.Duration(dirDays) * 24 * time.Hour)
	_ = filepath.Walk(filepath.Join(root, filepath.FromSlash(dir)), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			must(t, os.Chtimes(p, at, at))
		}
		return nil
	})
}

// TestAUvArchiveEntryIsPurgedWholeOrNotAtAll: uv links a package out of archive-v0/<id> on the
// strength of a pointer naming the id, and never checks inside, so a half-purged entry is a broken
// install waiting to happen. An entry whose every file is old goes whole, directory and all; one
// with a single fresh file is kept whole; one whose directory is fresh (uv unpacked it recently) is
// kept too. A file outside the archive (a pointer) is still purged on its own.
func TestAUvArchiveEntryIsPurgedWholeOrNotAtAll(t *testing.T) {
	for _, apply := range []bool{false, true} {
		root := t.TempDir()
		agedUnit(t, root, "uv/archive-v0/old", 40, map[string]int{"pkg/__init__.py": 40, "pkg/mod.py": 40, "pkg-1.0.dist-info/RECORD": 40})
		agedUnit(t, root, "uv/archive-v0/onefresh", 40, map[string]int{"pkg/__init__.py": 40, "pkg/mod.py": 1, "pkg-1.0.dist-info/RECORD": 40})
		agedUnit(t, root, "uv/archive-v0/freshdir", 1, map[string]int{"pkg/__init__.py": 40})
		agedTree(t, root, "uv/wheels-v5/pypi/pkg/pkg-1.0.http")

		bytes, files, partial := PurgeCacheByAgeWithin(root, []string{"uv"}, nil, 30, apply, time.Now(), nil, time.Time{})
		if partial || files != 4 || bytes != 3*2+1 {
			t.Errorf("apply=%v: (bytes=%d, files=%d, partial=%v), want the old entry's 3 files and the pointer",
				apply, bytes, files, partial)
		}
		if present(root, "uv/archive-v0/old") == apply {
			t.Errorf("apply=%v: the all-old entry present=%v", apply, !apply)
		}
		for _, rel := range []string{"uv/archive-v0/onefresh/pkg/__init__.py", "uv/archive-v0/onefresh/pkg/mod.py",
			"uv/archive-v0/onefresh/pkg-1.0.dist-info/RECORD", "uv/archive-v0/freshdir/pkg/__init__.py"} {
			if !present(root, rel) {
				t.Errorf("apply=%v: %s was removed from an entry that is still in use", apply, rel)
			}
		}
		if present(root, "uv/wheels-v5/pypi/pkg/pkg-1.0.http") == apply {
			t.Errorf("apply=%v: the aged pointer file was not purged on its own", apply)
		}
		if ents, _ := os.ReadDir(filepath.Join(root, "uv", "archive-v0")); apply && len(ents) != 2 {
			t.Errorf("archive-v0 holds %d entries after the purge, want the 2 kept (no tombstone left)", len(ents))
		}
	}
}

// TestATombstoneAnInterruptedRemovalLeftIsFinished: a unit renamed out of the tool's way whose
// removal was cut short goes at the next pass, whatever its files' age.
func TestATombstoneAnInterruptedRemovalLeftIsFinished(t *testing.T) {
	root := t.TempDir()
	agedUnit(t, root, "uv/archive-v0/"+purgeTombstonePrefix+"abc", 1, map[string]int{"pkg/a.py": 1})
	_, files, _ := PurgeCacheByAgeWithin(root, []string{"uv"}, nil, 30, true, time.Now(), nil, time.Time{})
	if files != 1 || present(root, "uv/archive-v0/"+purgeTombstonePrefix+"abc") {
		t.Errorf("files=%d, tombstone present=%v: want it removed", files, present(root, "uv/archive-v0/"+purgeTombstonePrefix+"abc"))
	}
}

// TestAUnitHoldingALockIsKept: a held file inside a unit keeps the whole unit, and the pex unit
// sits below the hash so pex's own atomic_directory lock beside it does not.
func TestAUnitHoldingALockIsKept(t *testing.T) {
	root := t.TempDir()
	agedUnit(t, root, "pex/installed_wheels/0/h1", 40, map[string]int{
		".pkg.whl.atomic_directory.lck": 40, "pkg.whl/pkg/a.py": 40})
	agedUnit(t, root, "pex/installed_wheels/0/h2", 40, map[string]int{"pkg.whl/pkg/a.py": 40, "pkg.whl/x.lock": 40})
	_, _, _ = PurgeCacheByAgeWithin(root, []string{"pex"}, nil, 30, true, time.Now(), nil, time.Time{})
	if present(root, "pex/installed_wheels/0/h1/pkg.whl") || !present(root, "pex/installed_wheels/0/h1/.pkg.whl.atomic_directory.lck") {
		t.Errorf("h1: want the chroot removed whole and pex's lock beside it held")
	}
	if !present(root, "pex/installed_wheels/0/h2/pkg.whl/pkg/a.py") {
		t.Errorf("h2: a unit with a held file in it was purged")
	}
}

// TestACutPassResumesInsideTheSubdirItStoppedIn: the cursor carries a place within a subdir, so
// a single subdir larger than one pass's budget is finished over several passes, not restarted.
func TestACutPassResumesInsideTheSubdirItStoppedIn(t *testing.T) {
	root := t.TempDir()
	var rels []string
	for i := 0; i < 12; i++ {
		rels = append(rels, "uv/wheels-v5/f"+strconv.Itoa(10+i))
	}
	agedTree(t, root, rels...)
	clock := tickClock(t)
	res := PurgeCacheByAgeFrom(root, []string{"uv", "pip"}, nil, 30, true, time.Now(), nil, clock().Add(8*time.Second), CachePurgeCursor{})
	if !res.Partial || res.Next.Sub != "uv" || res.Next.Path == "" || res.Files == 0 {
		t.Fatalf("got %+v, want a cut inside uv with a path", res)
	}
	if !slices.Equal(res.Remaining, []string{"uv", "pip"}) {
		t.Errorf("Remaining = %v, want [uv pip]", res.Remaining)
	}
	if d := res.Describe(); d == "" {
		t.Errorf("no description")
	}
}
