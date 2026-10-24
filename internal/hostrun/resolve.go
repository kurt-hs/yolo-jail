package hostrun

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/hostwrap"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// yoloAncestor returns the nearest directory at or above dir that holds a `.yolo`, or "".
// A `.yolo` marks a workspace a jail can write, so anything beneath its holder is
// agent-writable (HX-D32, HX-D33).
func yoloAncestor(dir string) string {
	for d := filepath.Clean(dir); ; {
		if _, err := os.Lstat(filepath.Join(d, ".yolo")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// defaultStateDirs are yolo's own state directories, which a jail-facing process may write.
func defaultStateDirs() []string {
	return []string{paths.GlobalStorage(), filepath.Dir(paths.UserConfigPath())}
}

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

// pathEntryProblem says why a PATH entry may supply jail-written code, or "" when it cannot.
// Every entry is checked, searched or not, because the child inherits PATH (HX-D32).
//
// managedDirs are yolo's host-managed directories (hostpath.ManagedDirs: the generated bin tree,
// which holds the host launch wrappers a user is asked to PREPEND to PATH). They sit inside the
// state directory, but no backend mounts them into a jail, so an entry in one is not refused for
// being in the state directory. Each spelling of the entry is judged on its own: a managed name
// that resolves elsewhere in the state directory is still refused, and the .yolo check applies
// to every entry regardless.
func pathEntryProblem(entry string, stateDirs, managedDirs []string) string {
	if entry == "" {
		return "an empty entry, which means the current directory"
	}
	if !filepath.IsAbs(entry) {
		return "a relative entry, which follows the current directory"
	}
	forms := []string{filepath.Clean(entry)}
	if r, err := filepath.EvalSymlinks(entry); err == nil && r != forms[0] {
		forms = append(forms, r)
	}
	for _, p := range forms {
		if h := yoloAncestor(p); h != "" {
			return "inside " + h + ", which holds a .yolo"
		}
		if withinAny(p, managedDirs) {
			continue
		}
		for _, s := range stateDirs {
			if withinAny(p, []string{s}) {
				return "inside yolo's state directory " + s
			}
		}
	}
	return ""
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

// agentWritableArgs returns the argv words that name an existing path inside a directory
// holding `.yolo`, relative words read against startDir (HX-D33).
func agentWritableArgs(words []string, startDir string) []string {
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
		if yoloAncestor(p) != "" {
			out = append(out, w)
		}
	}
	return out
}
