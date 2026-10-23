// Package nofollow walks a directory tree on the host WITHOUT EVER FOLLOWING A SYMLINK, for trees an
// agent may write: a dev override's checkout (docs/design/pack-buildable-programs.md §5), and every
// source tree yolo stages into a build.
//
// filepath.WalkDir and os.ReadFile classify an entry by its path and then open it by its path again,
// so an entry swapped for a symlink between the two (`src/` → `~/.ssh`) is followed, and the host
// reads bytes outside the tree. This walk opens every component from its parent's own descriptor,
// with O_NOFOLLOW (and O_DIRECTORY for a directory, O_NONBLOCK for a file, so a planted FIFO cannot
// hang it), and classifies what it opened by fstat, not by the path. A component swapped for a link
// after it was listed fails the open (ELOOP) instead of being followed. A race can make a walk fail;
// it cannot make one read anything outside the tree. The same construction as
// internal/jailcontent/workspaceskills.go's confinedTree reads, without its in-tree link resolution:
// here a symlink is never resolved at all, only reported by its target (Readlinkat).
package nofollow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

// Kind is what a walked entry is.
type Kind int

const (
	Dir     Kind = iota // a directory, visited before its children
	File                // a regular file, open for reading in Entry.File
	Symlink             // a symlink, its target in Entry.Target; never followed
	Other               // a device, socket or pipe: never opened
)

// Entry is one walked entry. Rel is its path relative to the root, slash-separated by the
// platform's separator ("." for the root). Perm is its permission bits, from the fstat of what was
// opened (a directory or file) or the lstat of a symlink or special file. File is open only for
// the visit's duration and closed by the walk.
type Entry struct {
	Rel    string
	Kind   Kind
	Perm   os.FileMode
	Target string
	File   *os.File
}

// ErrChanged is returned when an entry changed kind between its listing and its open: a file or
// directory swapped for a symlink, the race this package exists to refuse.
var ErrChanged = errors.New("changed while it was read (an entry was swapped for a symlink or another kind)")

const (
	dirFlags  = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fileFlags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
)

// testHookAfterLstat, when set by a test, runs between an entry's lstat and its open: the window a
// swap to a symlink races.
var testHookAfterLstat func(rel string)

// Walk visits root and everything under it, depth first, each directory's children sorted by name,
// skipping every Rel in skip (and its subtree). The root itself is opened with O_NOFOLLOW too: a
// root that is a symlink is visited as one Symlink entry and nothing under it is read. visit's
// error stops the walk and is returned.
func Walk(root string, skip map[string]bool, visit func(Entry) error) error {
	return walkAt(unix.AT_FDCWD, root, ".", skip, visit)
}

func walkAt(parent int, name, rel string, skip map[string]bool, visit func(Entry) error) error {
	if skip[filepath.ToSlash(rel)] {
		return nil
	}
	var st unix.Stat_t
	if err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &os.PathError{Op: "lstat", Path: rel, Err: err}
	}
	perm := os.FileMode(st.Mode & 0o777)
	if testHookAfterLstat != nil {
		testHookAfterLstat(rel)
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		target, err := readlinkat(parent, name)
		if err != nil {
			return &os.PathError{Op: "readlink", Path: rel, Err: err}
		}
		return visit(Entry{Rel: rel, Kind: Symlink, Perm: perm, Target: target})
	case unix.S_IFDIR:
		fd, err := openat(parent, name, dirFlags)
		if err != nil {
			return openFailure(rel, err)
		}
		d := os.NewFile(uintptr(fd), rel)
		defer d.Close()
		var got unix.Stat_t
		if err := unix.Fstat(fd, &got); err != nil {
			return &os.PathError{Op: "fstat", Path: rel, Err: err}
		}
		if got.Mode&unix.S_IFMT != unix.S_IFDIR || got.Ino != st.Ino || got.Dev != st.Dev {
			return fmt.Errorf("%s: %w", rel, ErrChanged)
		}
		if err := visit(Entry{Rel: rel, Kind: Dir, Perm: os.FileMode(got.Mode & 0o777)}); err != nil {
			return err
		}
		names, err := d.Readdirnames(-1)
		if err != nil {
			return &os.PathError{Op: "readdir", Path: rel, Err: err}
		}
		sort.Strings(names)
		for _, n := range names {
			if err := walkAt(fd, n, filepath.Join(rel, n), skip, visit); err != nil {
				return err
			}
		}
		return nil
	case unix.S_IFREG:
		fd, err := openat(parent, name, fileFlags)
		if err != nil {
			return openFailure(rel, err)
		}
		f := os.NewFile(uintptr(fd), rel)
		defer f.Close()
		var got unix.Stat_t
		if err := unix.Fstat(fd, &got); err != nil {
			return &os.PathError{Op: "fstat", Path: rel, Err: err}
		}
		if got.Mode&unix.S_IFMT != unix.S_IFREG || got.Ino != st.Ino || got.Dev != st.Dev {
			return fmt.Errorf("%s: %w", rel, ErrChanged)
		}
		return visit(Entry{Rel: rel, Kind: File, Perm: os.FileMode(got.Mode & 0o777), File: f})
	}
	return visit(Entry{Rel: rel, Kind: Other, Perm: perm})
}

// openFailure names an open that failed; ELOOP (or EMLINK, FreeBSD's spelling) is a component
// swapped for a symlink since it was listed.
func openFailure(rel string, err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%s: %w", rel, ErrChanged)
	}
	return &os.PathError{Op: "open", Path: rel, Err: err}
}

func openat(dirfd int, name string, flags int) (int, error) {
	for {
		fd, err := unix.Openat(dirfd, name, flags, 0)
		if err == unix.EINTR {
			continue
		}
		return fd, err
	}
}

func readlinkat(dirfd int, name string) (string, error) {
	for size := 256; ; size *= 2 {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(dirfd, name, buf)
		if err != nil {
			return "", err
		}
		if n < size {
			return string(buf[:n]), nil
		}
	}
}
