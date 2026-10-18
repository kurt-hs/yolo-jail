package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// treecopyreuse.go is the macos-user tree arm's copy (run.TreeBuildRequest.ReuseCopies): a patched
// extension's build copied out of the capture store ONCE per build, under the workspace's stable
// root (paths.TreeCopyRoot), and handed as it is to every later launch handed the same build.
//
// WHY ONLY macos-user. A container mounts its copy for the jail's whole life, so a copy is one
// launch's and goes with its pack tree. macos-user reads its copy once, into the home overlay, and
// the overlay's own reuse keys on the copy's store entry and prefix (run's patchedTreeIdentities),
// so a copy whose bytes are the entry's can be shared by every launch of that build.
//
// THE LAYOUT. <root>/<PatchedCopySlug(key)>/<hash of entry and prefix>/ holds `tree`, the copy, and
// `lease`, an empty file. A copy is made in a `.tmp-` directory beside it, checked against the
// entry's completion marker as every copy is (copyTreeForLaunch), and only then RENAMED into place,
// so a copy at its final name is whole: nothing is ever written into one after the rename.
//
// LIVENESS, NOT AGE. Each launch that hands a copy holds a shared flock on its lease for the rest of
// its process. A launch settling its own copy reaps every other directory of the extension whose
// lease it can lock exclusively — another entry's copy no launch holds, and a `.tmp-` a crashed copy
// left — and keeps one it cannot. Creating a directory with its lease and reaping both happen under
// the extension's own lock (<slug>.lock), so a reaper never sees a directory between its creation
// and its lease, and a launch never takes a lease on a directory being reaped.
//
// THE TRUST is the overlay's: the copy is the host user's, in yolo's host state dir, which the
// sandbox account cannot write; and the overlay that reads it is itself reused on its digest.

const (
	treeCopyTreeLeaf  = "tree"
	treeCopyLeaseLeaf = "lease"
	treeCopyTmpPrefix = ".tmp-"
	treeCopyReapLeaf  = ".reap-"
)

// heldTreeCopies are the leases this process holds, one open descriptor each, never closed: a
// lease ends with the process, which is how a reaper knows the copy is no longer read.
var (
	heldTreeCopiesMu sync.Mutex
	heldTreeCopies   []*os.File
)

func holdTreeCopyLease(f *os.File) {
	heldTreeCopiesMu.Lock()
	heldTreeCopies = append(heldTreeCopies, f)
	heldTreeCopiesMu.Unlock()
}

// treeCopyFormat is folded into every copy's name. Bump it whenever what a copy holds for the same
// entry changes (capture.CopyTree's modes or link handling), so no copy an older yolo made is handed.
const treeCopyFormat = "tree-copy/1"

// treeCopyName is the directory name of the copy of entry key's prefix.
func treeCopyName(key, prefix string) string {
	sum := sha256.Sum256([]byte(treeCopyFormat + "\x00" + key + "\x00" + prefix))
	return hex.EncodeToString(sum[:])[:32]
}

// reusableTreeCopy is copyTreeForLaunch under a stable root: the copy an earlier launch made of
// this entry when there is one, a new one otherwise, and every copy of f no launch holds reaped.
// errTreeEntryGone when the entry is no longer in the store, whether or not a copy of it exists,
// so the caller re-reads the record as it does for a copy cut short.
func reusableTreeCopy(f packload.Fork, key, root string, errw io.Writer) (string, error) {
	store := &capture.Store{Dir: paths.CapturesDir()}
	prefix := packdecl.TreeReservedDir(f.Bin)
	slugDir := filepath.Join(root, run.PatchedCopySlug(f.Key()))
	name := treeCopyName(key, prefix)
	final := filepath.Join(slugDir, name)
	entry, err := store.Resolve(key)
	if err != nil {
		return "", errTreeEntryGone
	}
	if err := os.MkdirAll(slugDir, 0o700); err != nil {
		return "", err
	}

	// UNDER THE EXTENSION'S LOCK: take the existing copy's lease, or make the directory a new copy
	// goes into with its lease held; then reap what no launch holds.
	var tmp string
	var lease *os.File
	var reaped []string
	err = withTreeCopyLock(slugDir, func() error {
		keep := name
		if l, ok := leaseTreeCopy(final); ok {
			lease = l
		} else {
			// Anything at the final name that is not a whole, leasable copy is reaped like a stray.
			keep = ""
			d, err := os.MkdirTemp(slugDir, treeCopyTmpPrefix)
			if err != nil {
				return err
			}
			l, err := createTreeCopyLease(d)
			if err != nil {
				_ = os.RemoveAll(d)
				return err
			}
			tmp, lease = d, l
		}
		reaped = reapTreeCopies(slugDir, keep, tmp)
		return nil
	})
	// THE REMOVALS RUN OUTSIDE THE LOCK: each reaped copy was renamed aside under it, so no launch can
	// take it, and a large tree's removal must not hold up another launch of the extension.
	for _, p := range reaped {
		_ = removeTreeCopy(p)
	}
	if err != nil {
		return "", err
	}
	if tmp == "" {
		holdTreeCopyLease(lease)
		return filepath.Join(final, treeCopyTreeLeaf), nil
	}

	// A NEW COPY, made outside the lock (it is the slow part) into the leased directory.
	_, cerr := capture.CopyTree(capture.CopyTreeOptions{Entry: entry, Prefix: prefix,
		Dest: filepath.Join(tmp, treeCopyTreeLeaf), Stderr: errw})
	treeCopied(key)
	if _, err := store.Resolve(key); err != nil {
		_ = lease.Close()
		_ = os.RemoveAll(tmp)
		return "", errTreeEntryGone
	}
	if cerr != nil {
		_ = lease.Close()
		_ = os.RemoveAll(tmp)
		return "", cerr
	}
	var dir string
	err = withTreeCopyLock(slugDir, func() error {
		if err := os.Rename(tmp, final); err == nil {
			dir = final
			return nil
		}
		// ANOTHER LAUNCH NAMED ITS COPY OF THIS ENTRY FIRST: hand that one, and drop this.
		if l, ok := leaseTreeCopy(final); ok {
			_ = lease.Close()
			lease, dir = l, final
			_ = os.RemoveAll(tmp)
			return nil
		}
		return fmt.Errorf("could not move the copy of %s into place at %s", key, final)
	})
	if err != nil {
		_ = lease.Close()
		_ = os.RemoveAll(tmp)
		return "", err
	}
	holdTreeCopyLease(lease)
	return filepath.Join(dir, treeCopyTreeLeaf), nil
}

// withTreeCopyLock runs fn holding the exclusive flock of slugDir's lock file.
func withTreeCopyLock(slugDir string, fn func() error) error {
	lf, err := os.OpenFile(slugDir+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// createTreeCopyLease creates dir's lease and holds it shared.
func createTreeCopyLease(dir string) (*os.File, error) {
	l, err := os.OpenFile(filepath.Join(dir, treeCopyLeaseLeaf), os.O_RDONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(l.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// leaseTreeCopy holds the lease of the copy at dir, shared, when dir is a real directory holding a
// copy and a lease. Called under the extension's lock, so no reaper holds the lease exclusively.
func leaseTreeCopy(dir string) (*os.File, bool) {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil, false
	}
	if fi, err := os.Lstat(filepath.Join(dir, treeCopyTreeLeaf)); err != nil || !fi.IsDir() {
		return nil, false
	}
	l, err := os.OpenFile(filepath.Join(dir, treeCopyLeaseLeaf), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(l.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		_ = l.Close()
		return nil, false
	}
	return l, true
}

// reapTreeCopies takes every directory under slugDir other than keep and tmp that no launch
// holds: one whose lease this process can lock exclusively, or one with no lease at all (a copy
// can only lack one if its launch died between making the directory and its lease, both under the
// lock this runs under). Each is renamed aside, so nothing is left at a name a launch would take,
// and returned for the caller to remove once it has let go of the lock; a `.reap-` directory an
// earlier removal left is returned as it is. Best-effort: a copy left behind costs disk, never a
// launch.
func reapTreeCopies(slugDir, keep, tmp string) (aside []string) {
	ents, err := os.ReadDir(slugDir)
	if err != nil {
		return nil
	}
	for _, e := range ents {
		name := e.Name()
		p := filepath.Join(slugDir, name)
		if name == keep || p == tmp || !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(name, treeCopyReapLeaf) {
			l, err := os.OpenFile(filepath.Join(p, treeCopyLeaseLeaf), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
			if err == nil {
				locked := syscall.Flock(int(l.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
				_ = l.Close()
				if !locked {
					continue
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				continue
			}
			dir, err := os.MkdirTemp(slugDir, treeCopyReapLeaf)
			if err != nil {
				continue
			}
			if os.Rename(p, filepath.Join(dir, "x")) != nil {
				_ = os.Remove(dir)
				continue
			}
			p = dir
		}
		aside = append(aside, p)
	}
	return aside
}

// removeTreeCopy removes a copy, giving its directories their owner write bit first should a
// copy have kept a directory without it.
func removeTreeCopy(p string) error {
	if err := os.RemoveAll(p); err == nil {
		return nil
	}
	_ = filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			if fi, err := d.Info(); err == nil {
				_ = os.Chmod(q, fi.Mode().Perm()|0o700)
			}
		}
		return nil
	})
	return os.RemoveAll(p)
}
