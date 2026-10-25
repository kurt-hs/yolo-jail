// Package worktreereap bounds a tool's managed worktree directory: it removes the worktrees a
// tool made there and left behind, once each is clean, unlocked, unused by any process and
// idle past a floor, through `git worktree remove`, so git's registration goes with the tree
// (docs/reference/disk-footprint.md "Tool-managed worktrees in the durable dir").
//
// IT RUNS IN THE JAIL, NEVER ON THE HOST. Host code never runs git in a workspace and never
// removes anything beneath the durable dir (docs/reference/jail-home.md DS-D3): the
// workspace's `.git/config` is jail-writable and names programs git runs. In the jail, git
// running the jail's own config is the jail running its own code. The caller is the pack that
// pointed the tool at the directory (packs/pi/extensions/yolo-durable-worktrees.js, through
// `yolo-jaild reap-worktrees`); core knows no tool's name, only the directory and the name
// prefix the pack hands it.
//
// IT NEVER DISCARDS WORK. A worktree with any uncommitted change or untracked file is kept and
// reported (Result.Dirty), and the removal itself is a plain `git worktree remove`, which
// refuses such a tree again on its own. The branch is not deleted, so a commit the tree made
// stays reachable. Only files the worktree's own ignore rules hide go with the tree: build
// outputs and installed dependencies, the bulk of a worktree's bytes.
//
// A REAPER THAT CANNOT ASK DECLINES: when the process table cannot be read, no worktree can be
// shown unused, and nothing is removed (Result.Declined).
package worktreereap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultFloor is how long a worktree must have been idle before it is removed: a day, so a
// tree a run just finished, or one a person is still reading, is never taken.
const DefaultFloor = 24 * time.Hour

// gitTimeout bounds each git call, so one wedged repository cannot hold the reaper forever.
const gitTimeout = 2 * time.Minute

// adminFileCap bounds every git link file read (`.git`, `gitdir`, `commondir`): one line each.
const adminFileCap = 4 << 10

// Options says which directory to bound.
type Options struct {
	// Base is the directory the tool makes its worktrees in.
	Base string
	// Prefix is the directory-name prefix of the tool's worktrees. Entries of Base, and of
	// each directory directly below it, whose names start with it are candidates; nothing
	// else under Base is touched.
	Prefix string
	// Repos are repositories (a work tree or a git dir) whose registrations beneath Base are
	// checked for directories that are gone, beside those of the candidates' repositories.
	Repos []string
	// Floor is the idle time a worktree must exceed; zero means DefaultFloor.
	Floor time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// LiveCwds lists every running process's working directory; nil reads the process table
	// (ProcessCwds). An error declines the whole pass.
	LiveCwds func() ([]string, error)
	// Git runs git with args in dir and returns its standard output; nil runs the real one.
	Git func(dir string, args ...string) (string, error)
}

// Result is one pass, each list holding worktree paths, sorted.
type Result struct {
	At time.Time `json:"at"`
	// Removed are the worktrees removed, registration and directory.
	Removed []string `json:"removed"`
	// Pruned are registrations beneath Base whose directory was already gone, now dropped.
	Pruned []string `json:"pruned,omitempty"`
	// Dirty are kept because they hold uncommitted changes or untracked files: the one list
	// a person may need to act on.
	Dirty []string `json:"kept_dirty,omitempty"`
	// Live are kept because a running process works inside them.
	Live []string `json:"kept_live,omitempty"`
	// Locked are kept because git's lock is on them.
	Locked []string `json:"kept_locked,omitempty"`
	// Young are kept because they were active within the floor.
	Young []string `json:"kept_young,omitempty"`
	// Unknown are kept because something about them could not be read or checked.
	Unknown []string `json:"kept_unknown,omitempty"`
	// Declined is why nothing was judged at all, when nothing was.
	Declined string   `json:"declined,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

// Reap runs one pass over o.Base.
func Reap(o Options) Result {
	if o.Floor <= 0 {
		o.Floor = DefaultFloor
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.LiveCwds == nil {
		o.LiveCwds = ProcessCwds
	}
	if o.Git == nil {
		o.Git = runGit
	}
	now := o.Now()
	res := Result{At: now}
	if o.Base == "" || o.Prefix == "" {
		res.Declined = "no worktree directory or name prefix was given"
		return res
	}
	if fi, err := os.Lstat(o.Base); err != nil || !isDir(fi) {
		// No directory (or a link where one should be): nothing of the tool's to bound.
		return res
	}
	cwds, err := o.LiveCwds()
	if err != nil {
		res.Declined = "the process table could not be read (" + err.Error() + "), so no worktree can be shown unused"
		return res
	}
	bases := []string{filepath.Clean(o.Base)}
	if real, err := filepath.EvalSymlinks(o.Base); err == nil && real != bases[0] {
		bases = append(bases, real)
	}

	commons := map[string]bool{}
	for _, wt := range candidates(o.Base, o.Prefix) {
		w, err := readWorktree(wt)
		if err != nil {
			res.Unknown = append(res.Unknown, wt)
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		commons[w.common] = true
		switch {
		case w.locked:
			res.Locked = append(res.Locked, wt)
		case now.Sub(w.lastActive) < o.Floor:
			res.Young = append(res.Young, wt)
		case inUse(wt, cwds):
			res.Live = append(res.Live, wt)
		default:
			status, err := o.Git(wt, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
			switch {
			case err != nil:
				res.Unknown = append(res.Unknown, wt)
				res.Errors = append(res.Errors, fmt.Sprintf("%s: git status: %v", wt, err))
			case strings.TrimSpace(status) != "":
				res.Dirty = append(res.Dirty, wt)
			default:
				// No --force: git refuses again if anything changed since the status.
				if _, err := o.Git(w.common, "worktree", "remove", wt); err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: git worktree remove: %v", wt, err))
					res.Unknown = append(res.Unknown, wt)
				} else {
					res.Removed = append(res.Removed, wt)
				}
			}
		}
	}

	for _, r := range o.Repos {
		if c, ok := commonDirOf(r); ok {
			commons[c] = true
		}
	}
	for _, c := range sortedKeys(commons) {
		for _, gone := range goneRegistrations(c, bases, o.Prefix, now, o.Floor) {
			if _, err := o.Git(c, "worktree", "remove", gone); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: git worktree remove (directory gone): %v", gone, err))
				continue
			}
			res.Pruned = append(res.Pruned, gone)
		}
	}
	for _, l := range [][]string{res.Removed, res.Pruned, res.Dirty, res.Live, res.Locked, res.Young, res.Unknown} {
		sort.Strings(l)
	}
	return res
}

// candidates are the prefix-named real directories directly in base and one level below it
// (tools that keep one subdirectory per repository, as pi-subagents does).
func candidates(base, prefix string) []string {
	var out []string
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p := filepath.Join(base, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || !isDir(fi) || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, p)
			continue
		}
		sub, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, s := range sub {
			if !strings.HasPrefix(s.Name(), prefix) {
				continue
			}
			q := filepath.Join(p, s.Name())
			if fi, err := os.Lstat(q); err == nil && isDir(fi) {
				out = append(out, q)
			}
		}
	}
	sort.Strings(out)
	return out
}

type worktree struct {
	common     string
	locked     bool
	lastActive time.Time
}

// readWorktree reads a linked worktree's link files and checks that git's registration points
// back at it, so a directory that merely claims a repository is never acted on.
func readWorktree(wt string) (worktree, error) {
	var w worktree
	line, err := readLine(filepath.Join(wt, ".git"))
	if err != nil {
		return w, fmt.Errorf("%s: not a linked worktree (%v)", wt, err)
	}
	admin, ok := strings.CutPrefix(line, "gitdir: ")
	if !ok {
		return w, fmt.Errorf("%s: .git names no gitdir", wt)
	}
	admin = absFrom(wt, admin)
	back, err := readLine(filepath.Join(admin, "gitdir"))
	if err != nil {
		return w, fmt.Errorf("%s: its registration %s cannot be read (%v)", wt, admin, err)
	}
	if !samePath(absFrom(admin, back), filepath.Join(wt, ".git")) {
		return w, fmt.Errorf("%s: its registration %s points elsewhere (%s)", wt, admin, back)
	}
	common := filepath.Join(admin, "..", "..")
	if c, err := readLine(filepath.Join(admin, "commondir")); err == nil {
		common = absFrom(admin, c)
	}
	w.common = filepath.Clean(common)
	if _, err := os.Lstat(filepath.Join(admin, "locked")); err == nil {
		w.locked = true
	}
	for _, p := range []string{wt, admin, filepath.Join(admin, "HEAD"), filepath.Join(admin, "index")} {
		if fi, err := os.Lstat(p); err == nil && fi.ModTime().After(w.lastActive) {
			w.lastActive = fi.ModTime()
		}
	}
	return w, nil
}

// goneRegistrations are common's unlocked registrations of prefix-named worktrees beneath one
// of bases whose directory no longer exists and whose registration is older than floor.
func goneRegistrations(common string, bases []string, prefix string, now time.Time, floor time.Duration) []string {
	dir := filepath.Join(common, "worktrees")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		admin := filepath.Join(dir, e.Name())
		if _, err := os.Lstat(filepath.Join(admin, "locked")); err == nil {
			continue
		}
		gitdir, err := readLine(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		wt := filepath.Dir(absFrom(admin, gitdir))
		if !strings.HasPrefix(filepath.Base(wt), prefix) || !beneathAny(bases, wt) {
			continue
		}
		if _, err := os.Lstat(wt); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if fi, err := os.Lstat(admin); err != nil || now.Sub(fi.ModTime()) < floor {
			continue
		}
		out = append(out, wt)
	}
	sort.Strings(out)
	return out
}

// commonDirOf is the git dir of r, a work tree or a git dir.
func commonDirOf(r string) (string, bool) {
	if fi, err := os.Lstat(filepath.Join(r, ".git")); err == nil && isDir(fi) {
		return filepath.Join(r, ".git"), true
	}
	if fi, err := os.Lstat(filepath.Join(r, "worktrees")); err == nil && isDir(fi) {
		return filepath.Clean(r), true
	}
	return "", false
}

// inUse reports whether any cwd is wt or beneath it.
func inUse(wt string, cwds []string) bool {
	wt = filepath.Clean(wt)
	real, _ := filepath.EvalSymlinks(wt)
	for _, c := range cwds {
		c = filepath.Clean(strings.TrimSuffix(c, " (deleted)"))
		for _, w := range []string{wt, real} {
			if w != "" && (c == w || strings.HasPrefix(c, w+string(filepath.Separator))) {
				return true
			}
		}
	}
	return false
}

// ProcessCwds lists every running process's working directory: from /proc where there is
// one, else from `lsof` (darwin). Processes it may not inspect are skipped; a table it cannot
// read at all is an error.
func ProcessCwds() ([]string, error) {
	if _, err := os.Readlink("/proc/self/cwd"); err == nil {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return nil, err
		}
		var out []string
		for _, e := range entries {
			if _, err := strconv.Atoi(e.Name()); err != nil {
				continue
			}
			if c, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd")); err == nil {
				out = append(out, c)
			}
		}
		return out, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, "lsof", "-w", "-a", "-d", "cwd", "-Fn").Output()
	if err != nil && len(b) == 0 {
		return nil, fmt.Errorf("no /proc, and lsof: %w", err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if p, ok := strings.CutPrefix(l, "n"); ok && p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// runGit runs git in dir (`-C`), with no repository chosen by the environment and no optional
// locks, so a status never contends with the tool's own git calls.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY":
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return string(out), fmt.Errorf("%w: %s", err, msg)
		}
		return string(out), err
	}
	return string(out), nil
}

// readLine reads the first line of a regular file (never a link), at most adminFileCap bytes.
func readLine(p string) (string, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, adminFileCap))
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line), nil
}

func absFrom(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(dir, p))
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func beneathAny(dirs []string, p string) bool {
	for _, d := range dirs {
		rel, err := filepath.Rel(d, p)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func isDir(fi os.FileInfo) bool { return fi.Mode()&fs.ModeSymlink == 0 && fi.IsDir() }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
