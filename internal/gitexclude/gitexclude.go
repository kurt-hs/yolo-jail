// Package gitexclude asks git whether a path is ignored and adds lines to a clone's own
// info/exclude file, the one ignore file that is local to the clone: a project's tracked
// .gitignore is the project's, and nothing here writes it.
package gitexclude

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status is what git says about one path.
type Status uint8

const (
	// Unknown: git could not answer — git is missing, or the path is in no work tree.
	Unknown Status = iota
	// Ignored: an ignore rule keeps the path out of `git add`.
	Ignored
	// NotIgnored: `git add -A` would pick the path up.
	NotIgnored
)

// Check asks git whether path is ignored, running in path's directory. A tracked path is
// reported not ignored, as `git check-ignore` reports it: git keeps committing it whatever the
// rules say. The path need not exist.
func Check(path string) Status {
	cmd := exec.Command("git", "-C", filepath.Dir(path), "check-ignore", "-q", "--", filepath.Base(path))
	err := cmd.Run()
	if err == nil {
		return Ignored
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return NotIgnored
	}
	return Unknown
}

// File returns the info/exclude file of the clone holding dir (a linked worktree shares its main
// clone's), or "" when dir is in no work tree or git is missing. The file need not exist yet.
func File(dir string) string {
	raw, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(raw))
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path
}

// Line is the info/exclude line that matches exactly path: its name relative to the top of its
// work tree, anchored with a leading slash. ok is false when git cannot name the work tree or path
// is outside it.
func Line(path string) (line string, ok bool) {
	raw, err := exec.Command("git", "-C", filepath.Dir(path), "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	top := strings.TrimSpace(string(raw))
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	dir := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	rel, err := filepath.Rel(top, filepath.Join(dir, filepath.Base(path)))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return "/" + filepath.ToSlash(rel), true
}

// Append adds line to the exclude file at path, creating it and its directory as needed, and
// starting a new line first when the file does not end in one.
func Append(path, line string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := line + "\n"
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		entry = "\n" + entry
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(entry); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
