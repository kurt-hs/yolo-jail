package prune

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// copilot/pkg is the GitHub Copilot CLI's self-update store in the shared cache:
// copilot/pkg/<platform>/<version>/, one whole unpacked release (~150-200 MB) per version, marked
// complete by an .extraction-complete file. Each self-update adds a version and nothing removes
// the old ones, so the store grows by one release per update without bound (16 versions, 2.4 GB,
// measured on the maintainer's host).
//
// The age purge must never touch it (cachePurgeForbidden names copilot): its files carry the
// release's own 1985 timestamps, so every file in the version a jail is running reads as decades
// old. This class judges whole version directories instead, by version order and by liveness.
//
// THE LIVENESS RULE. A copilot resolves, when it starts, to the newest COMPLETE version in the
// store. So a version V can be running only in a copilot that started before some newer complete
// version N was extracted, and that copilot runs in a jail that started before then too. V goes
// only when a newer complete version was extracted more than CopilotPkgGrace ago AND no running
// jail started before that extraction — the running jails' start times asked of whatever runs the
// jails that use the store (RunningJailStarts for the container store, MacosUserJailStarts for
// macos-user's own). A runtime that cannot be asked declines the whole class (CopilotPkgResult.Declined):
// "no jail is running" and "I could not tell" are not the same answer when the action is deleting a
// program out from under one. The newest CopilotPkgKeep complete versions stay whatever the
// answer, and a version without .extraction-complete (an unpacking in progress, or one that never
// finished) is never counted among them nor as the version a newer one supersedes.
//
// AUTOMATIC, NOT OFFERED (disk-footprint.md, "Copilot's self-update store"): the evidence is the
// runtime's own list of running jails, the same veto that protects live state, and a removed
// version is one release download if Copilot ever wants it again — the bounded regeneration and
// complete evidence BF-P3 asks for.

// CopilotPkgKeep is how many of the newest complete versions per platform are always kept.
const CopilotPkgKeep = 3

// CopilotPkgGrace is how long ago the superseding version must have been extracted.
const CopilotPkgGrace = 7 * 24 * time.Hour

// copilotPkgRel is the store's path under the shared cache root.
const copilotPkgRel = "copilot/pkg"

// copilotPkgComplete is the marker Copilot writes into a version once it is fully unpacked.
const copilotPkgComplete = ".extraction-complete"

// JailStarts is what the container runtime said about the running jails: their start times, or
// why it could not say (Known false).
type JailStarts struct {
	Known  bool
	Why    string
	Starts []time.Time
}

// CopilotPkgCacheMacosUser is the cache root of macos-user's own Copilot store: the run-as-you
// mode's machine tier (paths.MacosUserMachineRootUnder), whose `.cache` every self-mode session's
// HOME links into. NOT the shared cache: that holds the Linux releases container jails on the
// same Mac unpack, and no macos-user session runs them. The account mode's store is in the
// sandbox account's own home, which the invoking user cannot remove from (disk-footprint.md,
// "Copilot's self-update store", names that gap). A Mac that never ran self mode has no such
// directory, and the pass finds nothing. globalStorage is the invoking user's state dir
// (paths.GlobalStorageUnder of their home).
func CopilotPkgCacheMacosUser(globalStorage string) string {
	return filepath.Join(globalStorage, paths.MacosUserMachineLeaf, ".cache")
}

// MacosUserJailStarts is RunningJailStarts for macos-user, which runs no container: its running
// jails are its host-services sessions (runtime.MacosUserSessions), each one's start the earliest
// modification time among its dir, its record and its lock. A session makes all three as it
// starts and writes none of them again (the record once, O_EXCL; the lock is only ever flocked);
// a later file in the dir can only move the dir's own time later, so the earliest is the start or
// before it, the safe direction for a veto. A session whose liveness reads UNKNOWN counts as
// running, as every sweep reading these does; one that reads GONE does not. A session dir that is
// gone by the time it is read ended; any other failure to read one declines, like a listing that
// could not run.
func MacosUserJailStarts(base string) JailStarts {
	sessions, ok := runtime.MacosUserSessions(base)
	if !ok {
		return JailStarts{Why: "the macos-user sessions under " + base + " could not be listed"}
	}
	js := JailStarts{Known: true}
	for _, s := range sessions {
		if s.Liveness == runtime.SessionGone {
			continue
		}
		var start time.Time
		for _, p := range []string{s.Dir, filepath.Join(s.Dir, runtime.SessionRecordName),
			filepath.Join(s.Dir, paths.HostServicesSessionLockName)} {
			st, err := os.Lstat(p)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return JailStarts{Why: "could not read when the macos-user session " + s.Dir + " started: " + err.Error()}
			}
			if start.IsZero() || st.ModTime().Before(start) {
				start = st.ModTime()
			}
		}
		if !start.IsZero() {
			js.Starts = append(js.Starts, start)
		}
	}
	return js
}

// RunningJailStarts asks rt for the start time of every running yolo-* container. Only podman's
// `ps` reports one yolo reads (StartedAt, unix seconds). Apple Container's `container inspect`
// carries a status.startedDate whose encoding nobody has measured, so it declines
// (disk-footprint.md names the gap); macos-user runs no container and is MacosUserJailStarts.
func RunningJailStarts(rt string, run RunFunc) JailStarts {
	if rt != "podman" {
		return JailStarts{Why: "the " + strconv.Quote(rt) + " runtime reports no container start time yolo reads"}
	}
	res := run([]string{rt, "ps", "--format", "{{.Names}} {{.StartedAt}}"}, psTimeout)
	if !res.Ran || res.RC != 0 {
		return JailStarts{Why: "`podman ps` could not list the running jails"}
	}
	js := JailStarts{Known: true}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "yolo-") {
			continue
		}
		var sec int64
		var err error
		if len(f) >= 2 {
			sec, err = strconv.ParseInt(f[1], 10, 64)
		}
		if len(f) < 2 || err != nil || sec <= 0 {
			return JailStarts{Why: "`podman ps` gave no start time for " + f[0]}
		}
		js.Starts = append(js.Starts, time.Unix(sec, 0))
	}
	return js
}

// CopilotPkgVersion is one version directory of the store.
type CopilotPkgVersion struct {
	Platform, Version string
	Bytes             int64
	Err               string // set when its removal failed
}

// Display is platform/version.
func (v CopilotPkgVersion) Display() string { return v.Platform + "/" + v.Version }

// CopilotPkgResult is one pass of PruneCopilotPkgVersions.
type CopilotPkgResult struct {
	Removed, Failed []CopilotPkgVersion
	Bytes           int64
	// Declined is set when some version passed the version and time tests but the running jails
	// could not be asked; nothing was removed, and the caller leaves the class due.
	Declined string
}

// PruneCopilotPkgVersions removes, under cacheRoot/copilot/pkg/<platform>/, every version the
// liveness rule above lets go (apply=false: reports what it would remove). jails is asked only
// when there is a candidate, so a store with nothing to judge costs no runtime call. A version
// whose name is not a dotted release number is never removed: nothing can say where it sorts. A
// tombstone an interrupted removal left is finished whatever its age.
//
// Beneath a root on cacheRoot, like the age purge: a jail writes this tree. Each removal is
// bracketed by guard, whose recheck reads the version again under the lock; the removal renames
// the version to a tombstone first, so copilot never sees a half-removed release.
func PruneCopilotPkgVersions(cacheRoot string, keep int, grace time.Duration, now time.Time, jails func() JailStarts, apply bool, guard Guard) CopilotPkgResult {
	var res CopilotPkgResult
	r, err := os.OpenRoot(cacheRoot)
	if err != nil {
		return res
	}
	defer r.Close()
	plats, err := readDirBeneath(r, copilotPkgRel)
	if err != nil {
		return res
	}

	type ver struct {
		plat, name string
		num        []int
		complete   bool
		extracted  time.Time // the marker's modification time; zero unless complete
	}
	type cand struct {
		ver
		supersededAt time.Time // the earliest extraction of a newer complete version
	}
	var cands []cand
	for _, plat := range plats {
		if !plat.IsDir() {
			continue
		}
		pdir := path.Join(copilotPkgRel, plat.Name())
		ents, err := readDirBeneath(r, pdir)
		if err != nil {
			continue
		}
		var vers []ver
		for _, e := range ents {
			if !e.IsDir() { // a link is never followed or removed
				continue
			}
			if strings.HasPrefix(e.Name(), purgeTombstonePrefix) {
				v := CopilotPkgVersion{Platform: plat.Name(), Version: e.Name()}
				v.Bytes = treeSize(r, path.Join(pdir, e.Name()))
				if !apply {
					res.Removed = append(res.Removed, v)
					res.Bytes += v.Bytes
				} else if err := r.RemoveAll(path.Join(pdir, e.Name())); err != nil {
					v.Err = err.Error()
					res.Failed = append(res.Failed, v)
				} else {
					res.Removed = append(res.Removed, v)
					res.Bytes += v.Bytes
				}
				continue
			}
			num, ok := parseReleaseVersion(e.Name())
			if !ok {
				continue
			}
			v := ver{plat: plat.Name(), name: e.Name(), num: num}
			if st, err := r.Lstat(path.Join(pdir, e.Name(), copilotPkgComplete)); err == nil && st.Mode().IsRegular() {
				v.complete, v.extracted = true, st.ModTime()
			}
			vers = append(vers, v)
		}
		slices.SortFunc(vers, func(a, b ver) int { return slices.Compare(b.num, a.num) })
		kept := 0
		var earliestNewer time.Time // over the complete versions newer than the one in hand
		for _, v := range vers {
			if v.complete && kept < keep {
				kept++
			} else if !earliestNewer.IsZero() && now.Sub(earliestNewer) >= grace {
				cands = append(cands, cand{v, earliestNewer})
			}
			if v.complete && (earliestNewer.IsZero() || v.extracted.Before(earliestNewer)) {
				earliestNewer = v.extracted
			}
		}
	}
	if len(cands) == 0 {
		return res
	}
	js := jails()
	if !js.Known {
		res.Declined = js.Why
		return res
	}
	for _, c := range cands {
		if slices.ContainsFunc(js.Starts, func(s time.Time) bool { return s.Before(c.supersededAt) }) {
			continue // a jail started before the newer version existed: it may be running this one
		}
		rel := path.Join(copilotPkgRel, c.plat, c.name)
		cv := CopilotPkgVersion{Platform: c.plat, Version: c.name, Bytes: treeSize(r, rel)}
		if apply {
			var rmErr error
			ran := guard.Do(func() bool {
				st, err := r.Lstat(rel)
				return err == nil && st.IsDir()
			}, func() {
				tomb := path.Join(copilotPkgRel, c.plat, purgeTombstonePrefix+c.name)
				if rmErr = r.Rename(rel, tomb); rmErr == nil {
					rmErr = r.RemoveAll(tomb)
				}
			})
			if !ran {
				continue
			}
			if rmErr != nil {
				cv.Err = rmErr.Error()
				res.Failed = append(res.Failed, cv)
				continue
			}
		}
		res.Removed = append(res.Removed, cv)
		res.Bytes += cv.Bytes
	}
	return res
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

// treeSize sums the regular files under rel, links not followed.
func treeSize(r *os.Root, rel string) (size int64) {
	_ = fs.WalkDir(r.FS(), rel, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size
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
