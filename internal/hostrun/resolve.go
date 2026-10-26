package hostrun

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/hostwrap"
)

func within(p, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// withinAny reports whether p is inside one of dirs, as spelled or with its symlinks resolved.
func withinAny(p string, dirs []string) bool {
	for _, d := range dirs {
		if within(p, d) {
			return true
		}
		if rd, err := filepath.EvalSymlinks(d); err == nil && within(p, rd) {
			return true
		}
	}
	return false
}

// lookProgram resolves argv0 over pathVal the way a host launch resolves its target
// (hostwrap.LookPathSkipping, HE-D5): skipping yolo's host-managed directories, so a name a host
// launch wrapper shadows resolves to the real program rather than the wrapper. One holding a
// slash is read relative to startDir.
func lookProgram(argv0, pathVal, startDir string, managedDirs []string) (string, error) {
	if strings.Contains(argv0, "/") {
		p := argv0
		if !filepath.IsAbs(p) {
			p = filepath.Join(startDir, p)
		}
		if err := executable(p); err != nil {
			return "", err
		}
		return p, nil
	}
	p, err := hostwrap.LookPathSkipping(pathVal, argv0, managedDirs)
	if err != nil {
		return "", &fs.PathError{Op: "find", Path: argv0, Err: errNotOnPath}
	}
	return p, nil
}

var errNotOnPath = errors.New("not found on PATH")

func executable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() || fi.Mode()&0o111 == 0 {
		return &fs.PathError{Op: "exec", Path: p, Err: fs.ErrPermission}
	}
	return nil
}

// riskyEnv returns the NAME=value entries of environ the disclosure's `env:` line names
// (HX-D32): variables that change what a program loads or reads before it runs.
func riskyEnv(environ []string) []string {
	var out []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case name == "LD_PRELOAD", name == "LD_LIBRARY_PATH", name == "BASH_ENV", name == "ENV",
			strings.HasPrefix(name, "DYLD_"), strings.HasPrefix(name, "GIT_CONFIG_"):
			out = append(out, kv)
		}
	}
	return out
}

// lookupEnv is os.LookupEnv over an environ slice; the last entry wins, as exec's does.
func lookupEnv(environ []string, name string) (string, bool) {
	val, ok := "", false
	for _, kv := range environ {
		if k, v, found := strings.Cut(kv, "="); found && k == name {
			val, ok = v, true
		}
	}
	return val, ok
}

// agentWritableArgs returns the argv words that name an existing path inside the workspace,
// relative words read against startDir (HX-D33). The workspace is the tree a jail can write,
// so it is the note's subject; a `.yolo` marker somewhere above a path is not evidence of one.
func agentWritableArgs(words []string, startDir, workspace string) []string {
	var out []string
	for _, w := range words {
		if w == "" {
			continue
		}
		p := w
		if !filepath.IsAbs(p) {
			p = filepath.Join(startDir, p)
		}
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if workspace != "" && withinAny(p, []string{workspace}) {
			out = append(out, w)
		}
	}
	return out
}
