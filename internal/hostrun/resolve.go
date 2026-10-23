package hostrun

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

// pathEntryProblem says why a PATH entry may supply jail-written code, or "" when it cannot.
// Every entry is checked, searched or not, because the child inherits PATH (HX-D32).
func pathEntryProblem(entry string, stateDirs []string) string {
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
		for _, s := range stateDirs {
			if within(p, s) || (func() bool {
				rs, err := filepath.EvalSymlinks(s)
				return err == nil && within(p, rs)
			})() {
				return "inside yolo's state directory " + s
			}
		}
	}
	return ""
}

// lookProgram resolves argv0 the way exec does, over the given PATH entries, or relative to
// startDir when it holds a slash.
func lookProgram(argv0 string, entries []string, startDir string) (string, error) {
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
	for _, e := range entries {
		if e == "" || !filepath.IsAbs(e) {
			continue
		}
		p := filepath.Join(e, argv0)
		if executable(p) == nil {
			return p, nil
		}
	}
	return "", &fs.PathError{Op: "find", Path: argv0, Err: errNotOnPath}
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
