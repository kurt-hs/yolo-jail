package packload

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

type packTreeOps struct {
	lstat        func(string) (fs.FileInfo, error)
	stat         func(string) (fs.FileInfo, error)
	readDir      func(string) ([]fs.DirEntry, error)
	evalSymlinks func(string) (string, error)
	join         func(...string) string
	within       func(string, string) bool
}

func hostPackTreeOps() packTreeOps {
	return packTreeOps{
		lstat:        func(name string) (fs.FileInfo, error) { return os.Lstat(name) },
		stat:         func(name string) (fs.FileInfo, error) { return os.Stat(name) },
		readDir:      func(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) },
		evalSymlinks: filepath.EvalSymlinks,
		join:         filepath.Join,
		within:       pathWithin,
	}
}

// fsPackTreeOps is only for immutable symlink-free sources such as packreg's embedded FS.
// Path-backed packs use hostPackTreeOps plus the caller's existing whole-root Check.
func fsPackTreeOps(fsys fs.FS) packTreeOps {
	return packTreeOps{
		lstat:        func(name string) (fs.FileInfo, error) { return fs.Stat(fsys, name) },
		stat:         func(name string) (fs.FileInfo, error) { return fs.Stat(fsys, name) },
		readDir:      func(name string) ([]fs.DirEntry, error) { return fs.ReadDir(fsys, name) },
		evalSymlinks: func(name string) (string, error) { return name, nil },
		join:         path.Join,
		within:       pathWithin,
	}
}
