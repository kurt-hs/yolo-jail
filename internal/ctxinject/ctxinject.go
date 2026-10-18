// Package ctxinject adds a context mount to a RUNNING jail, and takes one away, without a restart
// (docs/design/live-config-apply.md §5, "Mount injection per backend").
//
// The INJECTOR (the design's word for this package's caller, the short-lived
// `yolo internal ctx-attach`) runs in a namespace that sees the host source and does five things
// in order (§5.1):
//
//  1. open_tree(source, OPEN_TREE_CLONE|AT_RECURSIVE): a detached copy of the source mount;
//  2. mount_setattr(AT_RECURSIVE, MOUNT_ATTR_RDONLY) on that copy, unless the mount is read-write,
//     before the jail can see it;
//  3. setns into the jail's mount namespace, from a fresh OS thread locked for the rest of its
//     life (a Go process is multi-threaded: CLONE_FS is unshared first, or setns is EINVAL);
//  4. the mountpoint is made through a PRIVATE writable clone of /ctx: open_tree(/ctx,
//     OPEN_TREE_CLONE), RDONLY cleared on that clone only, mkdirat. The jail's own /ctx is never
//     writable. /ctx must be the tmpfs a live-mounts launch makes, or nothing is attempted;
//  5. move_mount attaches the copy at the new mountpoint, which is opened through the jail's own
//     /ctx with no symlink or magic-link following and without crossing a mount, so the copy lands
//     on that tmpfs and nowhere else.
//
// Removal is setns, umount2(MNT_DETACH|UMOUNT_NOFOLLOW), then rmdir through the clone. A mountpoint
// this package made for a move that then failed is removed again, best effort.
//
// Host paths are resolved by the caller before this package runs, never after setns (§5.6): the
// only jail-side input is the destination.
package ctxinject

import (
	"errors"
	"path"
	"strings"
)

// Root is the context dir in a container jail (paths.ContainerContextDir), spelled here too so
// this package imports nothing of yolo's.
const Root = "/ctx"

// ProcessIdentity is a launch-local pin for the target PID and mount namespace. It is captured
// immediately after runtime inspection and revalidated under a pidfd before any namespace change.
type ProcessIdentity struct {
	StartTime     uint64
	MountNSDevice uint64
	MountNSInode  uint64
}

func (p ProcessIdentity) Valid() bool {
	return p.StartTime != 0 && p.MountNSInode != 0
}

// ErrNotTmpfs is the refusal when the jail's /ctx is not a tmpfs: a jail launched before live
// mounts existed has /ctx on its read-only image rootfs, which the injector cannot add to.
var ErrNotTmpfs = errors.New("/ctx in this jail is not the tmpfs a live-mounts launch makes " +
	"(the jail started before live mounts existed); restart the jail to mount there")

// RelDest returns dest relative to Root, refusing anything that is not a clean path strictly
// below it: no "..", no ".", no empty or trailing component.
func RelDest(dest string) (string, error) {
	rel, ok := strings.CutPrefix(dest, Root+"/")
	if !ok || rel == "" || path.Clean(dest) != dest {
		return "", errors.New("destination " + quote(dest) + " is not a clean path under " + Root + "/")
	}
	for _, c := range strings.Split(rel, "/") {
		if c == "" || c == "." || c == ".." {
			return "", errors.New("destination " + quote(dest) + " is not a clean path under " + Root + "/")
		}
	}
	return rel, nil
}

func quote(s string) string { return `"` + s + `"` }

// checkArgs is the argument validation both verbs share, before any syscall.
func checkArgs(pid int, dest string) (string, error) {
	if pid <= 0 {
		return "", errors.New("the jail's pid must be a positive number")
	}
	return RelDest(dest)
}
