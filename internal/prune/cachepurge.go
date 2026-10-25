package prune

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CachePurgeDefaultSubdirs are the ~/.cache subdirs safe to purge by age —
// content is pure CAS with a fast re-download/recompute path.
//
// `nce` was ADDED by OQ-BF2 (measured: 1.86 GiB already dead here, covered by
// nothing). `staticcheck` is deliberately still ABSENT, and the reason is a
// measurement rather than caution: it SELF-TRIMS. Both it and `go-build` use
// Go's own cache, which evicts entries unused for five days and sweeps at most
// once a day (trimLimit/trimInterval, cmd/go/internal/cache), leaving a
// `trim.txt` marker — verified present in both on 2026-09-08. A 30-day rule
// over a self-trimming cache can only ever be a no-op that costs a walk, and
// §2.1's own table shows it: of every subdir measured for files older than 30
// days, go-build is the only ZERO.
//
// go-build stays on the list because removing it is a behaviour change with no
// bytes behind it, but it must never be cited as evidence that this purge works.
var CachePurgeDefaultSubdirs = []string{
	"uv", "pip", "npm", "go-build", "mise", "pex", "pants", "node-gyp", "gopls", "nce",
}

// CachePurgeHeavySubdirs are opt-in age-purge subdirs with a meaningful re-fetch
// cost (playwright browsers ~400 MiB each; HF models GiBs).
var CachePurgeHeavySubdirs = []string{"ms-playwright", "huggingface"}

// cachePurgeForbidden are subdirs PurgeCacheByAge refuses to touch even when
// explicitly named — they carry live user profile state (cookies, IndexedDB,
// extensions) or the installed binaries of a tool, not regenerable cache.
var cachePurgeForbidden = map[string]struct{}{
	"chromium": {}, "google-chrome": {}, "chrome": {}, "mozilla": {},
	"firefox": {}, "thunderbird": {}, "copilot": {},
}

// CachePurgeBudget bounds one cache purge pass, the walk and the deletions alike (§5.3's 60 s;
// CI-D7 in docs/design/cache-isolation.md). Past it the pass stops and reports itself partial.
const CachePurgeBudget = 60 * time.Second

// purgeBatch is how many directory entries the walk reads at a time (CI-D7), so that the deadline
// is checked between batches even inside one very large directory.
const purgeBatch = 256

// purgeClock is the clock the deadline is read from; a test seam.
var purgeClock = time.Now

// PurgeCacheByAge removes regular files older than olderThanDays under each named
// subdir of cacheRoot. Returns (bytesRemoved, filesRemoved):
//   - only the caller-named subdirs are scanned (no glob, no recursion into the
//     allowlist);
//   - a subdir named in relocations is purged at its real host target instead
//     of under cacheRoot (see below);
//   - forbidden browser-profile subdirs are hard-excluded even if named;
//   - credentials and lock files are always held, whatever their age (cacheHeld);
//   - symlinks are never followed, and never deleted except inside a unit removed whole;
//   - a unit directory (cachePurgeUnits) goes whole, only when it and every file in it are old;
//   - staleness is keyed off mtime (>= cutoff is kept), not atime;
//   - apply=false returns accurate counts without mutating.
//
// relocations maps a cache subdir name to the absolute host directory that
// actually holds its bytes (nil when nothing is relocated). Without it the
// heavy purge would join cacheRoot/huggingface — the empty stub the relocation
// mount lands on — and report a successful purge of 0 B while the GiB it was
// aimed at sit untouched on the other filesystem. A relocated subdir is still
// subject to the forbidden-subdir check above: relocating something does not
// make it purgeable.
//
// now is the clock seam; the cutoff is now - olderThanDays*86400. It has no deadline:
// PurgeCacheByAgeWithin is the bounded form every production caller uses.
func PurgeCacheByAge(cacheRoot string, subdirs []string, relocations map[string]string, olderThanDays float64, apply bool, now time.Time) (bytesRemoved int64, filesRemoved int) {
	return PurgeCacheByAgeGuarded(cacheRoot, subdirs, relocations, olderThanDays, apply, now, nil)
}

// PurgeCacheByAgeGuarded is PurgeCacheByAge with each file's removal bracketed by guard
// (guard.go). Its recheck re-reads the file right before the removal: a file some jail
// rewrote since the walk saw it is no longer older than the cutoff, and is kept.
func PurgeCacheByAgeGuarded(cacheRoot string, subdirs []string, relocations map[string]string, olderThanDays float64, apply bool, now time.Time, guard Guard) (bytesRemoved int64, filesRemoved int) {
	b, f, _ := PurgeCacheByAgeWithin(cacheRoot, subdirs, relocations, olderThanDays, apply, now, guard, time.Time{})
	return b, f
}

// PurgeCacheByAgeWithin is PurgeCacheByAgeGuarded stopped at deadline (zero: none), always
// starting at the first subdir. partial reports that the deadline stopped the pass: the counts are
// then what it reached, a lower bound, and the caller must not record the pass as complete
// (CI-D7). A caller that runs again later and wants the next pass to go on where this one stopped
// uses PurgeCacheByAgeFrom, which returns that place.
func PurgeCacheByAgeWithin(cacheRoot string, subdirs []string, relocations map[string]string, olderThanDays float64, apply bool, now time.Time, guard Guard, deadline time.Time) (bytesRemoved int64, filesRemoved int, partial bool) {
	r := PurgeCacheByAgeFrom(cacheRoot, subdirs, relocations, olderThanDays, apply, now, guard, deadline, CachePurgeCursor{})
	return r.Bytes, r.Files, r.Partial
}

// CachePurgeCursor is where a bounded cache purge resumes: the subdir, and the slash-separated
// path within it of the first entry the stopped pass had not finished. The zero cursor is the
// start of the list.
//
// THE WALK IS IN NAME ORDER so that a path is a place to resume from: each directory is read whole
// (in batches, the deadline checked between them) and sorted, and a resumed walk skips, at every
// level of the cursor's path, the names that sort before it. Before the cursor every pass
// restarted at the first subdir, and a 60 s budget one large subdir (uv) used up meant no pass ever
// reached the subdirs after it.
type CachePurgeCursor struct {
	Sub  string `json:"sub,omitempty"`
	Path string `json:"path,omitempty"`
}

// IsStart reports whether c is the start of the list: no cycle in progress.
func (c CachePurgeCursor) IsStart() bool { return c.Sub == "" }

// CachePurgeResult is one bounded pass of PurgeCacheByAgeFrom.
type CachePurgeResult struct {
	Bytes   int64
	Files   int
	Partial bool // the deadline stopped the pass; Bytes and Files are a lower bound
	// Next is where the next pass resumes: the zero cursor when this pass reached the end of the
	// list, which completes a cycle that may have begun several passes earlier.
	Next CachePurgeCursor
	// Covered is every subdir this pass walked, wholly or (the last, when Partial) in part.
	// Remaining is every subdir it did not finish, the one it stopped in first; empty unless Partial.
	Covered, Remaining []string
	// Failed names each unit the pass renamed out of its tool's way but could not remove, with the
	// error: the next pass tries again, and the caller says so.
	Failed []string
}

// PurgeCacheByAgeFrom is PurgeCacheByAgeWithin starting at from (a cursor an earlier pass
// returned in Next) rather than at the first subdir. A cursor naming a subdir no longer on the list
// starts at the beginning.
func PurgeCacheByAgeFrom(cacheRoot string, subdirs []string, relocations map[string]string, olderThanDays float64, apply bool, now time.Time, guard Guard, deadline time.Time, from CachePurgeCursor) CachePurgeResult {
	cutoff := now.Add(-time.Duration(olderThanDays * 86400 * float64(time.Second)))
	var res CachePurgeResult

	first, resume := 0, []string(nil)
	if i := slices.Index(subdirs, from.Sub); i >= 0 {
		first = i
		if from.Path != "" {
			resume = strings.Split(from.Path, "/")
		}
	}

	// The cache root is opened FOLLOWING a link at it, and so is a relocation target: neither
	// is a path the jail can replace (each is a jail's mountpoint, and the components above are
	// the host's own), and a user may well have put the machine store or a relocated cache on
	// another disk through one. Everything BELOW them is the jail's to write, so it is walked
	// and removed beneath the root (purgeOldFilesUnder).
	cache, _ := os.OpenRoot(cacheRoot)
	if cache != nil {
		defer cache.Close()
	}
	for i := first; i < len(subdirs); i++ {
		sub := subdirs[i]
		if _, forbidden := cachePurgeForbidden[sub]; forbidden {
			continue
		}
		w := purgeWalk{cutoff: cutoff, apply: apply, guard: guard, deadline: deadline,
			hold:  func(rel string) bool { return cacheHeld(sub, rel) },
			units: cachePurgeUnits[sub]}
		if i == first {
			w.resume = resume
		}
		res.Covered = append(res.Covered, sub)
		if target := relocations[sub]; target != "" {
			if r, err := os.OpenRoot(target); err == nil {
				w.run(r, ".")
				r.Close()
			}
		} else if cache != nil {
			w.run(cache, sub)
		}
		res.Bytes += w.bytes
		res.Files += w.files
		res.Failed = append(res.Failed, w.failed...)
		if w.partial {
			res.Partial = true
			res.Next = CachePurgeCursor{Sub: sub, Path: w.cursor}
			res.Remaining = append([]string{}, subdirs[i:]...)
			return res
		}
	}
	return res
}

// cachePurgeUnits names, per purged subdir, the directories the purge treats as ONE ITEM: removed
// whole, and only when the directory itself and every regular file in it are older than the
// cutoff and nothing in it is held. Each pattern is a path relative to the subdir, "*" matching
// any one name; a directory at a matching path is a unit.
//
// THE RULE FOR BEING ON THIS LIST: the tool trusts the directory's EXISTENCE as proof that its
// contents are complete. Purging file by file there leaves a half-empty (in the end an empty)
// directory the tool goes on using: uv links a package out of archive-v0/<id> on the strength of
// a pointer under wheels-v*/ naming that id, and never looks inside for what is missing. Those are:
//
//   - uv: archive-v0/<id> (an unpacked wheel), environments-v2/<id> (a cached ephemeral
//     environment, a whole venv) and builds-v0/<id> (a build environment).
//   - pex, and the pex root pants keeps under named_caches: each cache kind is
//     <kind>/<layout version>/<hash>, made by pex's atomic_directory; installed_wheels and venvs
//     nest the chroot one level further, beside its .atomic_directory.lck, so the unit is the
//     level below the hash and the held lock stays out of it.
//   - npm: _npx/<hash>, a whole node_modules install npx reuses by its presence.
//   - nce: <hash>, a scie's unpacked install. node-gyp: <version>, headers it trusts once its
//     installVersion file is there.
//
// EVERYTHING ELSE IS PURGED PER FILE, deliberately, because each file there is a whole entry
// whose absence the tool reads as a miss and fetches again: uv's pointer and metadata files
// (wheels-v*, simple-v*, sdists-v*, interpreter-v*); pip's http-v2 responses (a missing body or
// header file is a cachecontrol miss) and its built wheels, one file each; npm's _cacache, whose
// index entries name content by digest and verify it on read, so an entry whose content is gone
// is a miss npm refetches; Go's build cache (go-build, gopls), which treats a missing output as a
// miss; and mise's downloaded archives.
var cachePurgeUnits = map[string][][]string{
	"uv":       {{"archive-v0", "*"}, {"environments-v2", "*"}, {"builds-v0", "*"}},
	"pex":      pexUnits(),
	"pants":    pexUnits("named_caches", "pex_root"),
	"npm":      {{"_npx", "*"}},
	"nce":      {{"*"}},
	"node-gyp": {{"*"}},
}

// pexUnits is pex's cache layout (see cachePurgeUnits) under prefix.
func pexUnits(prefix ...string) [][]string {
	var out [][]string
	for _, kind := range []string{"installed_wheels", "venvs"} {
		out = append(out, append(append([]string{}, prefix...), kind, "*", "*", "*"))
	}
	// The kinds whose <hash> directory is the atomic_directory itself, as observed on a real
	// cache. A kind not named here (built_wheels, downloads, packed_wheels, ...) keeps the
	// per-file purge until its layout is read the same way.
	for _, kind := range []string{"unzipped_pexes", "bootstraps", "user_code", "isolated", "interpreters"} {
		out = append(out, append(append([]string{}, prefix...), kind, "*", "*"))
	}
	return out
}

// purgeTombstonePrefix names a unit the purge has renamed out of the tool's way and not yet
// finished removing. The rename is what makes a unit's removal all-or-nothing as the tool sees it:
// the unit's own name is gone in one step, so an interrupted RemoveAll leaves a tombstone no tool
// reads rather than a half-empty unit, and the next pass removes the tombstone whatever its age.
const purgeTombstonePrefix = ".yolo-purge-"

// cacheHeld reports whether the file at rel (slash-separated, relative to the bucket's root) is
// one the age purge must keep whatever its age: the plan's "always hold" rows
// (docs/design/cache-isolation-plan.md, V6). A credential is not cache — Hugging Face keeps its
// login token at huggingface/token and its named tokens in huggingface/stored_tokens, both caught
// by the "token" rule — and a lock or control file's age says nothing about whether a process
// holds it (uv/.lock is uv's live cache lock).
func cacheHeld(bucket, rel string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		l := strings.ToLower(part)
		if strings.Contains(l, "token") || strings.Contains(l, "auth") {
			return true
		}
	}
	base := strings.ToLower(parts[len(parts)-1])
	switch {
	case isLockName(base):
		return true
	case base == "cachedir.tag":
		return true
	case bucket == "uv" && rel == ".gitignore":
		return true
	}
	return false
}

// isLockName reports whether a file name (lowercased) is a lock file's.
func isLockName(base string) bool {
	return base == "lock" || base == ".lock" || strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".lck")
}

// purgeBeforeRemove, when set, is called with each file's path (relative to the purge's root)
// just before the purge removes it. It is a test seam, nil in production: the window it opens is
// the one in which a jail can swap a directory on the way for a link, between the walk's check
// and the removal.
var purgeBeforeRemove func(rel string)

// purgeOldFilesUnder removes regular files under rel below r whose mtime is before cutoff,
// returning (bytesRemoved, filesRemoved), with no deadline and nothing held. Shared by
// PurgeAgentLogs; the cache purge runs the same walk (purgeWalk) with its holds and deadline.
// Discipline (identical to the cache-purge contract):
//   - a missing/non-dir rel is a no-op (returns 0,0), and so is a symbolic link at rel;
//   - symlinks are never followed or deleted;
//   - only regular files are counted/removed (dirs are left as mount anchors);
//   - mtime >= cutoff is kept;
//   - apply=false computes accurate counts without mutating.
//
// BENEATH r, never by plain path (docs/reference/jail-home.md, "Host code in jail-writable
// state"): every tree this purges is one a jail can write, and the purge runs as the host user.
// A directory the jail swaps for a link, before the walk or between the walk's check and the
// removal, leaves the root, and r refuses the removal rather than deleting the host file of the
// same name behind it.
func purgeOldFilesUnder(r *os.Root, rel string, cutoff time.Time, apply bool, guard Guard) (bytesRemoved int64, filesRemoved int) {
	w := purgeWalk{cutoff: cutoff, apply: apply, guard: guard}
	w.run(r, rel)
	return w.bytes, w.files
}

// purgeWalk is one bounded walk of a tree beneath an os.Root (CI-D7): directories are read
// purgeBatch entries at a time and walked in name order, and the deadline is checked before each
// batch, each entry and each removal. Where it stops it leaves cursor, the path (relative to the
// walk's start) of the first entry it did not finish.
type purgeWalk struct {
	cutoff   time.Time
	apply    bool
	guard    Guard
	hold     func(rel string) bool // rel is slash-separated, relative to the walk's start; nil holds nothing
	deadline time.Time             // zero: none
	units    [][]string            // cachePurgeUnits' patterns for this walk; nil: every file on its own
	resume   []string              // a cursor's path, split: what sorts before it is skipped

	bytes   int64
	files   int
	partial bool
	cursor  string
	failed  []string // units renamed away whose removal failed: path (relative to r) and error
}

func (w *purgeWalk) expired() bool {
	if !w.partial && !w.deadline.IsZero() && !purgeClock().Before(w.deadline) {
		w.partial = true
	}
	return w.partial
}

// stopAt records rel as where the walk stopped, unless a deeper call already did.
func (w *purgeWalk) stopAt(rel string) {
	if w.cursor == "" {
		w.cursor = rel
	}
}

func (w *purgeWalk) run(r *os.Root, rel string) {
	info, err := r.Lstat(rel)
	if err != nil || !info.IsDir() {
		return
	}
	resume := w.resume
	w.resume = nil
	w.dir(r, rel, "", resume)
}

// readSorted reads the directory at path whole, purgeBatch entries at a time, and sorts it by
// name. ok is false when the deadline passed before it finished.
func (w *purgeWalk) readSorted(r *os.Root, path string) (ents []fs.DirEntry, ok bool) {
	f, err := r.Open(path)
	if err != nil {
		return nil, true
	}
	defer f.Close()
	for {
		if w.expired() {
			return nil, false
		}
		batch, err := f.ReadDir(purgeBatch)
		ents = append(ents, batch...)
		if err != nil { // io.EOF, or a directory that stopped reading
			break
		}
	}
	slices.SortFunc(ents, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return ents, true
}

// dir walks the directory at path (relative to r); under is its path relative to the walk's
// start, slash-separated, for the holds, the units and the cursor. resume is what is left of a
// cursor's path at this level.
func (w *purgeWalk) dir(r *os.Root, path, under string, resume []string) {
	ents, ok := w.readSorted(r, path)
	if !ok {
		w.stopAt(under)
		return
	}
	for _, e := range ents {
		var rest []string
		if len(resume) > 0 {
			c := strings.Compare(e.Name(), resume[0])
			if c < 0 {
				continue // the pass that left the cursor finished it
			}
			if c == 0 {
				rest = resume[1:]
			}
			resume = nil
		}
		p := filepath.Join(path, e.Name())
		u := e.Name()
		if under != "" {
			u = under + "/" + e.Name()
		}
		if w.expired() {
			w.stopAt(u)
			return
		}
		switch {
		case e.IsDir() && w.isUnit(u):
			w.unit(r, p, u)
		case e.IsDir():
			w.dir(r, p, u, rest)
		default:
			w.file(r, p, u)
		}
		if w.partial {
			w.stopAt(u)
			return
		}
	}
}

// isUnit reports whether the directory at rel matches one of the walk's unit patterns.
func (w *purgeWalk) isUnit(rel string) bool {
	if len(w.units) == 0 {
		return false
	}
	parts := strings.Split(rel, "/")
next:
	for _, pat := range w.units {
		if len(pat) != len(parts) {
			continue
		}
		for i, c := range pat {
			if c != "*" && c != parts[i] {
				continue next
			}
		}
		return true
	}
	return false
}

// unit removes the directory at path as one item (cachePurgeUnits): only when it and every
// regular file in it are older than the cutoff and no lock file sits at its top, by renaming it to
// a tombstone and removing that. A tombstone an earlier pass left goes whatever its age.
//
// NOT THE BUCKET-WIDE HOLDS (cacheHeld). Their substring rules exist for a bucket's own credential
// and lock files; inside an unpacked package they match ordinary content (requests/auth.py,
// the tokenization_*.py modules of transformers, node_modules/js-tokens, a bundled poetry.lock),
// and holding on them kept those whole units for ever. No unit layout on the list keeps a
// credential inside a unit, and the one lock any of them keeps near one (the .atomic_directory.lck
// of pex) sits beside it, outside the walk of the unit; a lock file directly at a unit's top is
// still held (scanUnit).
//
// NO TOOL LOCK IS TAKEN, as the per-file purge before it took none: uv and npm may be reading a
// unit while it is renamed away. The age rule is the protection, the same exposure the per-file
// purge had, and the rename leaves the tool a miss rather than a half-empty entry.
func (w *purgeWalk) unit(r *os.Root, path, under string) {
	tomb := strings.HasPrefix(filepath.Base(path), purgeTombstonePrefix)
	if !tomb && !w.unitDirOld(r, path) {
		return
	}
	size, files, old, ok := w.scanUnit(r, path, under, tomb, w.deadline)
	if !ok {
		w.partial = true
		w.stopAt(under)
		return
	}
	if !old && !tomb {
		return
	}
	if w.apply {
		if w.expired() {
			w.stopAt(under)
			return
		}
		removed := false
		w.guard.Do(func() bool {
			// Under the guard's lock, the walk's own test, asked again of the whole unit.
			if tomb {
				return true
			}
			if !w.unitDirOld(r, path) {
				return false
			}
			_, _, again, done := w.scanUnit(r, path, under, false, time.Time{})
			return done && again
		}, func() {
			if purgeBeforeRemove != nil {
				purgeBeforeRemove(path)
			}
			gone := path
			if !tomb {
				gone = filepath.Join(filepath.Dir(path), purgeTombstonePrefix+filepath.Base(path))
				if r.Rename(path, gone) != nil {
					return
				}
			}
			if err := r.RemoveAll(gone); err != nil {
				// Renamed out of the tool's way, so nothing reads the rest; the next pass
				// tries again, and the caller reports it with a next step.
				w.failed = append(w.failed, filepath.ToSlash(gone)+": "+err.Error())
				return
			}
			removed = true
		})
		if !removed {
			return
		}
	}
	w.bytes += size
	w.files += files
}

// unitDirOld reports whether the unit at path is a real directory (not a link) older than the
// cutoff: a tool that unpacked into it recently set its modification time then.
func (w *purgeWalk) unitDirOld(r *os.Root, path string) bool {
	st, err := r.Lstat(path)
	return err == nil && st.IsDir() && st.ModTime().Before(w.cutoff)
}

// scanUnit sums the regular files under the unit at path and reports whether every one is older
// than the cutoff and none is held; it stops at the first that is not, unless whole (a tombstone,
// which goes whatever it holds) asks it to sum everything. ok is false when deadline (zero: none)
// passed before it finished.
func (w *purgeWalk) scanUnit(r *os.Root, path, under string, whole bool, deadline time.Time) (size int64, files int, old, ok bool) {
	old, ok = true, true
	var walk func(p, u string)
	walk = func(p, u string) {
		f, err := r.Open(p)
		if err != nil {
			return
		}
		defer f.Close()
		for old && ok {
			if !deadline.IsZero() && !purgeClock().Before(deadline) {
				ok = false
				return
			}
			ents, err := f.ReadDir(purgeBatch)
			for _, e := range ents {
				cp, cu := filepath.Join(p, e.Name()), u+"/"+e.Name()
				if !whole && p == path && !e.IsDir() && isLockName(strings.ToLower(e.Name())) {
					old = false // a lock at the unit's top: its age says nothing about its holder
					return
				}
				if e.IsDir() {
					walk(cp, cu)
					if !old || !ok {
						return
					}
					continue
				}
				st, err := r.Lstat(cp)
				if err != nil || !st.Mode().IsRegular() {
					continue // a link goes with its unit, never followed
				}
				if !whole && !st.ModTime().Before(w.cutoff) {
					old = false
					return
				}
				size += st.Size()
				files++
			}
			if err != nil { // io.EOF, or a directory that stopped reading
				return
			}
		}
	}
	walk(path, under)
	return size, files, old, ok
}

func (w *purgeWalk) file(r *os.Root, path, under string) {
	if w.hold != nil && w.hold(under) {
		return
	}
	st, err := r.Lstat(path)
	if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return
	}
	// Kept when mtime >= cutoff.
	if !st.ModTime().Before(w.cutoff) {
		return
	}
	size := st.Size()
	if w.apply {
		if w.expired() {
			w.stopAt(under)
			return
		}
		removed := false
		w.guard.Do(func() bool {
			// Under the guard's lock, the walk's own test, asked again.
			again, err := r.Lstat(path)
			return err == nil && again.Mode().IsRegular() && again.ModTime().Before(w.cutoff)
		}, func() {
			if purgeBeforeRemove != nil {
				purgeBeforeRemove(path)
			}
			removed = r.Remove(path) == nil
		})
		if !removed {
			return
		}
	}
	w.bytes += size
	w.files++
}
