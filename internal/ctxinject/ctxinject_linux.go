//go:build linux

package ctxinject

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// resolveDest is how every jail-side path is opened: no symlink and no magic link is followed,
// and nothing outside the starting directory is reached (design §5.6). noXDev adds "and no mount
// is crossed", which pins a path to the /ctx tmpfs itself.
func resolveFlags(noXDev bool) uint64 {
	f := uint64(unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_BENEATH)
	if noXDev {
		f |= unix.RESOLVE_NO_XDEV
	}
	return f
}

// openBeneath opens rel below dirfd as a directory, through resolveFlags.
func openBeneath(dirfd int, rel string, noXDev bool) (int, error) {
	return unix.Openat2(dirfd, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: resolveFlags(noXDev),
	})
}

// Attach mounts source at dest inside the mount namespace of the process pid, read-only unless
// rw. source must already be resolved (absolute, symlinks followed, refusals applied) by the
// caller; it is opened in THIS process's namespace before any setns.
func Attach(pid int, source, dest string, rw bool) error {
	rel, err := checkArgs(pid, dest)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(source) {
		return errors.New("source " + quote(source) + " must be an absolute path")
	}
	// Step 1: the detached copy, in the caller's namespace, where the host source is visible.
	// THE SOURCE IS OPENED WITH NO LINK FOLLOWED, then copied through that open file: the host
	// resolved every link before this ran, so a resolved path has none, and a component swapped
	// for a link between that resolution and this open is refused rather than followed.
	src, err := unix.Openat2(unix.AT_FDCWD, source, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return fmt.Errorf("open %s (a directory, reached with no link followed): %w", source, err)
	}
	tree, err := unix.OpenTree(src, "",
		unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC|unix.AT_RECURSIVE|unix.AT_EMPTY_PATH)
	unix.Close(src)
	if err != nil {
		return fmt.Errorf("open_tree(%s): %w", source, err)
	}
	defer unix.Close(tree)
	var srcStat unix.Stat_t
	if err := unix.Fstat(tree, &srcStat); err != nil {
		return fmt.Errorf("stat the copy of %s: %w", source, err)
	}
	// Step 2: read-only before the jail can see it.
	if !rw {
		if err := unix.MountSetattr(tree, "", unix.AT_EMPTY_PATH|unix.AT_RECURSIVE,
			&unix.MountAttr{Attr_set: unix.MOUNT_ATTR_RDONLY}); err != nil {
			return fmt.Errorf("mount_setattr(read-only) on the copy of %s: %w", source, err)
		}
	}
	ns, err := openMountNS(pid)
	if err != nil {
		return err
	}
	defer unix.Close(ns)
	// Steps 3 to 5.
	return inMountNS(ns, func() error { return attachHere(tree, rel, srcStat, rw) })
}

// Detach unmounts dest inside the mount namespace of pid and removes its mountpoint. A dest that
// is already gone is success.
func Detach(pid int, dest string) error {
	rel, err := checkArgs(pid, dest)
	if err != nil {
		return err
	}
	ns, err := openMountNS(pid)
	if err != nil {
		return err
	}
	defer unix.Close(ns)
	return inMountNS(ns, func() error { return detachHere(rel) })
}

func openMountNS(pid int) (int, error) {
	p := fmt.Sprintf("/proc/%d/ns/mnt", pid)
	fd, err := unix.Open(p, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", p, err)
	}
	return fd, nil
}

// inMountNS runs fn on a fresh OS thread that has joined the mount namespace ns. The thread stays
// locked and is never handed back to the scheduler: a goroutine that exits while locked takes its
// thread with it, so no other goroutine ever runs in the jail's namespace.
func inMountNS(ns int, fn func() error) error {
	done := make(chan error, 1)
	go func() {
		goruntime.LockOSThread() // never unlocked, on purpose
		if err := unix.Unshare(unix.CLONE_FS); err != nil {
			done <- fmt.Errorf("unshare(CLONE_FS): %w", err)
			return
		}
		if err := unix.Setns(ns, unix.CLONE_NEWNS); err != nil {
			done <- fmt.Errorf("setns into the jail's mount namespace: %w", err)
			return
		}
		done <- fn()
	}()
	return <-done
}

// openCtx opens the jail's /ctx and checks it is a tmpfs, through the open file so the check and
// every later use are the same directory.
func openCtx() (int, error) {
	fd, err := unix.Open(Root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open %s in the jail: %w", Root, err)
	}
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("statfs %s in the jail: %w", Root, err)
	}
	if st.Type != unix.TMPFS_MAGIC {
		unix.Close(fd)
		return -1, ErrNotTmpfs
	}
	return fd, nil
}

// writableClone is step 4's private clone of /ctx with RDONLY cleared on it alone. It is a
// detached mount of the same tmpfs: a directory made through it appears in the jail's /ctx, and
// the jail's own mount of /ctx stays read-only.
func writableClone() (int, error) {
	clone, err := unix.OpenTree(unix.AT_FDCWD, Root, unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC)
	if err != nil {
		return -1, fmt.Errorf("open_tree(%s) in the jail: %w", Root, err)
	}
	if err := unix.MountSetattr(clone, "", unix.AT_EMPTY_PATH,
		&unix.MountAttr{Attr_clr: unix.MOUNT_ATTR_RDONLY}); err != nil {
		unix.Close(clone)
		return -1, fmt.Errorf("mount_setattr(clear read-only) on a private clone of %s: %w", Root, err)
	}
	return clone, nil
}

// attachHere is steps 4 and 5, run inside the jail's mount namespace. src is the copy's own stat
// and rw its mode: a destination that already holds a mount of that same directory in that same
// mode is success with nothing done, so an add is idempotent (a step cut off after its mount
// landed is recorded by the next apply instead of refused).
func attachHere(tree int, rel string, src unix.Stat_t, rw bool) error {
	ctx, err := openCtx()
	if err != nil {
		return err
	}
	defer unix.Close(ctx)
	if sameMountAt(ctx, rel, src, rw) {
		return nil
	}
	clone, err := writableClone()
	if err != nil {
		return err
	}
	defer unix.Close(clone)
	created, err := mkdirsBeneath(clone, rel)
	if err != nil {
		removeCreated(clone, created)
		return err
	}
	// The target through the jail's own /ctx, never crossing a mount: a destination that is
	// already a mountpoint, or sits under one, is refused rather than mounted over.
	target, err := openBeneath(ctx, rel, true)
	if err != nil {
		removeCreated(clone, created)
		if errors.Is(err, unix.EXDEV) {
			return fmt.Errorf("%s/%s is, or is under, a mount already; remove that first", Root, rel)
		}
		return fmt.Errorf("open %s/%s in the jail: %w", Root, rel, err)
	}
	defer unix.Close(target)
	if err := unix.MoveMount(tree, "", target, "",
		unix.MOVE_MOUNT_F_EMPTY_PATH|unix.MOVE_MOUNT_T_EMPTY_PATH); err != nil {
		removeCreated(clone, created)
		return fmt.Errorf("move_mount onto %s/%s: %w", Root, rel, err)
	}
	return nil
}

// sameMountAt reports whether rel below the jail's /ctx is the root of a mount of the directory
// src describes (same device and inode), read-only exactly when rw is false. It opens rel with no
// link followed and without leaving /ctx, crossing mounts as it must to reach the mount's root.
func sameMountAt(ctx int, rel string, src unix.Stat_t, rw bool) bool {
	fd, err := unix.Openat2(ctx, rel, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: resolveFlags(false),
	})
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var stx unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_INO, &stx); err != nil ||
		stx.Attributes_mask&unix.STATX_ATTR_MOUNT_ROOT == 0 || stx.Attributes&unix.STATX_ATTR_MOUNT_ROOT == 0 {
		return false
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Dev != src.Dev || st.Ino != src.Ino {
		return false
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil {
		return false
	}
	return (fs.Flags&unix.ST_RDONLY != 0) == !rw
}

// mkdirsBeneath makes every missing component of rel below dirfd, mode 0755, and returns the
// ones it made, outermost first. An existing component must be a directory reached without
// following a link.
func mkdirsBeneath(dirfd int, rel string) ([]string, error) {
	var created []string
	parts := strings.Split(rel, "/")
	for i := range parts {
		sub := strings.Join(parts[:i+1], "/")
		fd, err := openBeneath(dirfd, sub, true)
		if err == nil {
			unix.Close(fd)
			continue
		}
		if !errors.Is(err, unix.ENOENT) {
			return created, fmt.Errorf("open %s/%s in the jail: %w", Root, sub, err)
		}
		parent, err := openBeneath(dirfd, parentOf(sub), true)
		if err != nil {
			return created, fmt.Errorf("open %s/%s in the jail: %w", Root, parentOf(sub), err)
		}
		err = unix.Mkdirat(parent, parts[i], 0o755)
		unix.Close(parent)
		if err != nil {
			return created, fmt.Errorf("mkdir %s/%s through a private clone of %s: %w", Root, sub, Root, err)
		}
		created = append(created, sub)
	}
	return created, nil
}

func parentOf(rel string) string {
	if d := path.Dir(rel); d != "" {
		return d
	}
	return "."
}

// removeCreated removes, innermost first, the directories mkdirsBeneath made: best effort, for a
// move that failed.
func removeCreated(clone int, created []string) {
	for i := len(created) - 1; i >= 0; i-- {
		_ = rmdirBeneath(clone, created[i])
	}
}

// rmdirBeneath removes the empty directory rel below dirfd. The kernel refuses (EBUSY) one that
// is a mountpoint in this namespace, so a live mount is never detached by it.
func rmdirBeneath(dirfd int, rel string) error {
	parent, err := openBeneath(dirfd, parentOf(rel), true)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	return unix.Unlinkat(parent, path.Base(rel), unix.AT_REMOVEDIR)
}

// detachHere is the removal, run inside the jail's mount namespace.
func detachHere(rel string) error {
	ctx, err := openCtx()
	if err != nil {
		return err
	}
	defer unix.Close(ctx)
	parentRel := parentOf(rel)
	// The parent pinned to the tmpfs, no links: the unmount names the leaf relative to it, and
	// UMOUNT_NOFOLLOW keeps the leaf itself from being a link.
	parent, err := openBeneath(ctx, parentRel, true)
	if errors.Is(err, unix.ENOENT) {
		return nil // already gone
	}
	if err != nil {
		return fmt.Errorf("open %s/%s in the jail: %w", Root, parentRel, err)
	}
	defer unix.Close(parent)
	// This thread unshared CLONE_FS, so its cwd is its own: a relative umount2 path resolves
	// against the parent opened above, with no /proc in the jail's pid namespace involved.
	if err := unix.Fchdir(parent); err != nil {
		return fmt.Errorf("chdir to %s/%s in the jail: %w", Root, parentRel, err)
	}
	leaf := path.Base(rel)
	switch err := unix.Unmount(leaf, unix.MNT_DETACH|unix.UMOUNT_NOFOLLOW); {
	case err == nil, errors.Is(err, unix.EINVAL):
		// EINVAL: nothing mounted there (an earlier removal unmounted it); the empty mountpoint
		// left behind is removed below all the same.
	case errors.Is(err, unix.ENOENT):
		return nil
	default:
		return fmt.Errorf("umount %s/%s in the jail: %w", Root, rel, err)
	}
	clone, err := writableClone()
	if err != nil {
		return err
	}
	defer unix.Close(clone)
	if err := rmdirBeneath(clone, rel); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("rmdir %s/%s in the jail: %w", Root, rel, err)
	}
	return nil
}
