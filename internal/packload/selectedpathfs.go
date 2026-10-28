package packload

import (
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"
)

// selectedPathFS is the read-only view of packstage.Check's staged paths. It exposes only
// files the filtered no-write walk would deliver while opening their original source paths.
type selectedPathFS struct {
	root  string
	files map[string]bool
	dirs  map[string]map[string]bool
}

func newSelectedPathFS(root string, staged []string) *selectedPathFS {
	view := &selectedPathFS{root: root, files: make(map[string]bool, len(staged)), dirs: map[string]map[string]bool{".": {}}}
	for _, file := range staged {
		file = path.Clean(file)
		if file == "." || file == ".." || path.IsAbs(file) {
			continue
		}
		view.files[file] = true
		parent := path.Dir(file)
		for parent != "." {
			if _, exists := view.dirs[parent]; !exists {
				view.dirs[parent] = make(map[string]bool)
			}
			parent = path.Dir(parent)
		}
		parent = path.Dir(file)
		view.dirs[parent][path.Base(file)] = false
		for parent != "." {
			grand := path.Dir(parent)
			view.dirs[grand][path.Base(parent)] = true
			parent = grand
		}
	}
	return view
}

func (f *selectedPathFS) Open(name string) (fs.File, error) {
	clean, err := cleanSelectedPath(name)
	if err != nil {
		return nil, pathError("open", name, err)
	}
	if f.files[clean] {
		file, err := os.Open(f.physical(clean))
		if err != nil {
			return nil, pathError("open", name, err)
		}
		return &selectedPathFile{File: file, name: clean}, nil
	}
	if _, ok := f.dirs[clean]; ok {
		file, err := os.Open(f.physical(clean))
		if err != nil {
			return nil, pathError("open", name, err)
		}
		entries, err := f.ReadDir(clean)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return &selectedPathDir{selectedPathFile: selectedPathFile{File: file, name: clean}, entries: entries}, nil
	}
	return nil, pathError("open", name, fs.ErrNotExist)
}

func (f *selectedPathFS) Stat(name string) (fs.FileInfo, error) {
	clean, err := cleanSelectedPath(name)
	if err != nil {
		return nil, pathError("stat", name, err)
	}
	if f.files[clean] {
		info, err := os.Stat(f.physical(clean))
		if err != nil {
			return nil, pathError("stat", name, err)
		}
		return renamedPathInfo{name: path.Base(clean), FileInfo: info}, nil
	}
	if _, ok := f.dirs[clean]; ok {
		info, err := os.Stat(f.physical(clean))
		if err == nil && info.IsDir() {
			return renamedPathInfo{name: path.Base(clean), FileInfo: info}, nil
		}
		if clean == "." {
			return nil, pathError("stat", name, fs.ErrNotExist)
		}
		return selectedDirInfo(path.Base(clean)), nil
	}
	return nil, pathError("stat", name, fs.ErrNotExist)
}

func (f *selectedPathFS) ReadDir(name string) ([]fs.DirEntry, error) {
	clean, err := cleanSelectedPath(name)
	if err != nil {
		return nil, pathError("readdir", name, err)
	}
	children, ok := f.dirs[clean]
	if !ok {
		return nil, pathError("readdir", name, fs.ErrNotExist)
	}
	names := make([]string, 0, len(children))
	for child := range children {
		names = append(names, child)
	}
	sort.Strings(names)
	entries := make([]fs.DirEntry, 0, len(names))
	for _, child := range names {
		childPath := path.Join(clean, child)
		var info fs.FileInfo
		if children[child] {
			info, err = f.Stat(childPath)
		} else {
			info, err = os.Stat(f.physical(childPath))
			if err == nil {
				info = renamedPathInfo{name: child, FileInfo: info}
			}
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, selectedPathEntry{name: child, info: info})
	}
	return entries, nil
}

func (f *selectedPathFS) physical(name string) string {
	if name == "." {
		return f.root
	}
	return filepath.Join(f.root, filepath.FromSlash(name))
}

func cleanSelectedPath(name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", fs.ErrInvalid
	}
	return path.Clean(name), nil
}

func pathError(op, name string, err error) error {
	return &fs.PathError{Op: op, Path: name, Err: err}
}

type selectedPathEntry struct {
	name string
	info fs.FileInfo
}

func (e selectedPathEntry) Name() string      { return e.name }
func (e selectedPathEntry) IsDir() bool       { return e.info.IsDir() }
func (e selectedPathEntry) Type() fs.FileMode { return e.info.Mode().Type() }
func (e selectedPathEntry) Info() (fs.FileInfo, error) {
	return renamedPathInfo{name: e.name, FileInfo: e.info}, nil
}

type renamedPathInfo struct {
	name string
	fs.FileInfo
}

func (i renamedPathInfo) Name() string { return i.name }

func selectedDirInfo(name string) fs.FileInfo { return selectedPathInfo{name: name} }

type selectedPathInfo struct{ name string }

func (i selectedPathInfo) Name() string       { return i.name }
func (i selectedPathInfo) Size() int64        { return 0 }
func (i selectedPathInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (i selectedPathInfo) ModTime() time.Time { return time.Time{} }
func (i selectedPathInfo) IsDir() bool        { return true }
func (i selectedPathInfo) Sys() any           { return nil }

type selectedPathFile struct {
	fs.File
	name string
}

func (f *selectedPathFile) Name() string { return f.name }
func (f *selectedPathFile) Stat() (fs.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return renamedPathInfo{name: path.Base(f.name), FileInfo: info}, nil
}

type selectedPathDir struct {
	selectedPathFile
	entries []fs.DirEntry
	index   int
}

func (d *selectedPathDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.index >= len(d.entries) && n > 0 {
		return nil, io.EOF
	}
	end := len(d.entries)
	if n > 0 && d.index+n < end {
		end = d.index + n
	}
	out := append([]fs.DirEntry(nil), d.entries[d.index:end]...)
	d.index = end
	return out, nil
}
