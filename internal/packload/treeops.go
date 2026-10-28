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

// fsPackTreeOps reads a source view that has already enforced path checks or is intrinsically
// immutable and symlink-free, such as packreg's embedded FS.
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
