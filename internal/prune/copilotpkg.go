package prune

import (
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// copilot/pkg is the GitHub Copilot CLI's self-update store in the shared cache:
// copilot/pkg/<platform>/<version>/, one whole unpacked release (~150-200 MB) per version, marked
// complete by an .extraction-complete file. Each self-update adds a version and nothing removes
// the old ones, so the store grows by one release per update without bound (16 versions, 2.4 GB,
// measured on the maintainer's host).
//
// The age purge must never touch it (cachePurgeForbidden names copilot): its files carry the
// release's own 1985 timestamps, so every file in the version a jail is running reads as decades
// old. This class judges whole version directories instead, by version order and by use.
//
// AUTOMATIC, NOT OFFERED (disk-footprint.md, "Copilot's self-update store"): what it removes is a
// release Copilot downloads again on its own if it is ever wanted, never the newest CopilotPkgKeep
// (one of which is the release a launched copilot resolves to, the newest complete one), and never
// one any jail has read in the last CopilotPkgRecent. The offered tier is for classes whose
// regeneration nothing can bound; this one's is one release download, at most.

// CopilotPkgKeep is how many of the newest versions per platform are always kept.
const CopilotPkgKeep = 3

// CopilotPkgRecent keeps any older version some jail read (or wrote) this recently.
//
// IT STANDS IN FOR LIVENESS, which the slot cannot ask: a running jail's copilot keeps no record of
// the version it runs that the host can read. A copilot started on an old version reads its files
// as it loads them, and the access time that leaves is what this window sees. On a filesystem
// mounted noatime it sees only the extraction time, and the newest CopilotPkgKeep is then the
// whole protection.
const CopilotPkgRecent = 7 * 24 * time.Hour

// copilotPkgRel is the store's path under the shared cache root.
const copilotPkgRel = "copilot/pkg"

// CopilotPkgVersion is one version directory of the store.
type CopilotPkgVersion struct {
	Platform, Version string
	Bytes             int64
	Err               string // set when its removal failed
}

// Display is platform/version.
func (v CopilotPkgVersion) Display() string { return v.Platform + "/" + v.Version }

// PruneCopilotPkgVersions removes, under cacheRoot/copilot/pkg/<platform>/, every version
// directory past the newest keep that nothing has read or written within recent (apply=false:
// reports what it would remove). A version whose name does not parse as a dotted release number is
// never removed: nothing can say where it sorts. A tombstone an interrupted removal left is
// finished whatever its age.
//
// Beneath a root on cacheRoot, like the age purge: a jail writes this tree. Each removal is
// bracketed by guard, whose recheck reads the version's last use again under the lock; the removal
// renames the version to a tombstone first, so copilot never sees a half-removed release.
func PruneCopilotPkgVersions(cacheRoot string, keep int, recent time.Duration, now time.Time, apply bool, guard Guard) (removed, failed []CopilotPkgVersion, bytes int64) {
	r, err := os.OpenRoot(cacheRoot)
	if err != nil {
		return nil, nil, 0
	}
	defer r.Close()
	plats, err := readDirBeneath(r, copilotPkgRel)
	if err != nil {
		return nil, nil, 0
	}
	for _, plat := range plats {
		if !plat.IsDir() {
			continue
		}
		pdir := path.Join(copilotPkgRel, plat.Name())
		ents, err := readDirBeneath(r, pdir)
		if err != nil {
			continue
		}
		type ver struct {
			name string
			num  []int
		}
		var vers []ver
		for _, e := range ents {
			if !e.IsDir() { // a link is never followed or removed
				continue
			}
			if strings.HasPrefix(e.Name(), purgeTombstonePrefix) {
				v := CopilotPkgVersion{Platform: plat.Name(), Version: e.Name()}
				v.Bytes, _ = treeUse(r, path.Join(pdir, e.Name()))
				if !apply || r.RemoveAll(path.Join(pdir, e.Name())) == nil {
					removed = append(removed, v)
					bytes += v.Bytes
				}
				continue
			}
			if num, ok := parseReleaseVersion(e.Name()); ok {
				vers = append(vers, ver{e.Name(), num})
			}
		}
		slices.SortFunc(vers, func(a, b ver) int { return slices.Compare(b.num, a.num) })
		for i, v := range vers {
			if i < keep {
				continue
			}
			rel := path.Join(pdir, v.name)
			size, last := treeUse(r, rel)
			if last.IsZero() || now.Sub(last) < recent {
				continue
			}
			cv := CopilotPkgVersion{Platform: plat.Name(), Version: v.name, Bytes: size}
			if apply {
				var rmErr error
				ran := guard.Do(func() bool {
					_, again := treeUse(r, rel)
					return !again.IsZero() && now.Sub(again) >= recent
				}, func() {
					tomb := path.Join(pdir, purgeTombstonePrefix+v.name)
					if rmErr = r.Rename(rel, tomb); rmErr == nil {
						rmErr = r.RemoveAll(tomb)
					}
				})
				if !ran {
					continue
				}
				if rmErr != nil {
					cv.Err = rmErr.Error()
					failed = append(failed, cv)
					continue
				}
			}
			removed = append(removed, cv)
			bytes += size
		}
	}
	return removed, failed, bytes
}

// readDirBeneath lists the directory at rel beneath r.
func readDirBeneath(r *os.Root, rel string) ([]fs.DirEntry, error) {
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// treeUse sums the regular files under rel and returns the latest time anything there was used:
// the directory's own modification time (when it was unpacked) and each file's access and
// modification times. A zero time means it could not be read, which keeps the version.
func treeUse(r *os.Root, rel string) (size int64, last time.Time) {
	st, err := r.Lstat(rel)
	if err != nil || !st.IsDir() {
		return 0, time.Time{}
	}
	last = st.ModTime()
	_ = fs.WalkDir(r.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			// A directory's modification time only: its ACCESS time is moved by any listing,
			// this walk's own included, so reading it would make every walk a fresh use.
			if info.IsDir() && info.ModTime().After(last) {
				last = info.ModTime()
			}
			return nil
		}
		for _, t := range []time.Time{info.ModTime(), accessTime(info)} {
			if t.After(last) {
				last = t
			}
		}
		size += info.Size()
		return nil
	})
	return size, last
}

// parseReleaseVersion parses a dotted release number ("1.0.92", "1.0.92-3"), its prerelease or
// build suffix ignored for order. ok is false for anything else.
func parseReleaseVersion(name string) (num []int, ok bool) {
	core := name
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	for _, part := range strings.Split(core, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		num = append(num, n)
	}
	return num, len(num) > 0
}

// accessTime is info's last access time, zero when the platform's stat is not at hand.
func accessTime(info fs.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return statAccessTime(st)
	}
	return time.Time{}
}
