//go:build linux

package capture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// artifactWalk pins every ancestor with O_NOFOLLOW and passes already-open files
// to the visitor. Neither classification nor the transfer reopens a source path.
// SurfaceRoot is deliberately unused: it is an optimization of the legacy move,
// not authority to bypass the artifact home's no-follow walk.
func (d *driver) artifactWalk(visit func(string, *os.File, os.FileInfo, string) error) error {
	home := int(d.artifactHome.Fd())
	var walk func(int, string, string) error
	walk = func(parent int, name, rel string) error {
		var st unix.Stat_t
		if err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT == unix.S_IFLNK {
			buf := make([]byte, 4096)
			n, err := unix.Readlinkat(parent, name, buf)
			if err != nil {
				return err
			}
			if n == len(buf) {
				return fmt.Errorf("program capture: overlong link %q — fix the recipe", rel)
			}
			return visit(rel, nil, nil, string(buf[:n]))
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR && st.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("program capture: special output %q — fix the selected recipe", rel)
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(parent, name, flags, 0)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), rel)
		defer file.Close()
		var pinned unix.Stat_t
		if err := unix.Fstat(fd, &pinned); err != nil {
			return err
		}
		if st.Dev != pinned.Dev || st.Ino != pinned.Ino || st.Mode != pinned.Mode {
			return fmt.Errorf("program capture: output %q changed while opening — retry the sealed install", rel)
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || info.Mode().Perm()&0002 != 0 {
			return fmt.Errorf("program capture: unsafe permissions on %q — fix the selected recipe", rel)
		}
		if err := visit(rel, file, info, ""); err != nil {
			return err
		}
		if info.IsDir() {
			names, err := file.Readdirnames(-1)
			if err != nil {
				return err
			}
			for _, name := range names {
				if err := walk(fd, name, path.Join(rel, name)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, root := range d.surfaceRels() {
		parent, err := unix.Dup(home)
		if err != nil {
			return err
		}
		parts := strings.Split(root, "/")
		missing := false
		for i, name := range parts[:len(parts)-1] {
			next, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if errors.Is(err, unix.ENOENT) {
				missing = true
				break
			}
			if err != nil {
				unix.Close(parent)
				return fmt.Errorf("program capture: parent of %q is not a no-follow directory — fix the selected recipe: %w", root, err)
			}
			// Ancestors appear in the manifest too; derive their modes from pinned fds.
			borrowed, err := unix.Dup(next)
			if err != nil {
				unix.Close(next)
				unix.Close(parent)
				return err
			}
			f := os.NewFile(uintptr(borrowed), name)
			info, err := f.Stat()
			if err == nil {
				err = visit(strings.Join(parts[:i+1], "/"), f, info, "")
			}
			f.Close()
			if err != nil {
				unix.Close(next)
				unix.Close(parent)
				return err
			}
			unix.Close(parent)
			parent = next
		}
		if !missing {
			err = walk(parent, parts[len(parts)-1], root)
			if errors.Is(err, unix.ENOENT) {
				err = nil
			}
		}
		unix.Close(parent)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *driver) artifactSnapshot() (snapshot, error) {
	snap := snapshot{}
	err := d.artifactWalk(func(rel string, f *os.File, info os.FileInfo, target string) error {
		if f == nil {
			snap[rel] = node{kind: KindSymlink}
			return nil
		}
		snap[rel] = node{kind: kindOf(info.Mode().Type()), perm: info.Mode().Perm(), size: info.Size(), mtime: info.ModTime().UnixNano()}
		return nil
	})
	return snap, err
}

func (d *driver) transferArtifacts(baseline snapshot, res *Result) error {
	return d.artifactWalk(func(rel string, f *os.File, info os.FileInfo, target string) error {
		dst := filepath.Join(d.tree, filepath.FromSlash(rel))
		if f == nil {
			if err := checkArtifactTarget(d.home, rel, target, d.surfaceRels()); err != nil {
				return err
			}
			if err := os.Symlink(target, dst); err != nil {
				return err
			}
			res.Copied++
			return nil
		}
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		now := node{kind: KindFile, perm: info.Mode().Perm(), size: info.Size(), mtime: info.ModTime().UnixNano()}
		if was, ok := baseline[rel]; ok && was == now {
			return nil
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, f)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		res.Copied++
		return nil
	})
}

func (d *driver) pinArtifactHome() error {
	fd, err := unix.Open(d.home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("program capture: cannot pin capture home — retry in the supported sealed Linux runtime: %w", err)
	}
	d.artifactHome = os.NewFile(uintptr(fd), "capture-home")
	return nil
}
