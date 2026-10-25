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
// IT NEVER DISCARDS WORK. An empty `git status` is not enough, because a tree can be clean and
// still hold the only copy of a commit (an amend in the middle of a rebase leaves a detached
// HEAD no ref names). A tree is removed only when, besides a clean status, its HEAD is a
// branch, no operation (rebase, merge, cherry-pick, revert, bisect) is in progress in it, and
// its HEAD commit is reachable from some ref other than its own branch; anything else is kept
// and reported (Result.Dirty, Result.Unsaved). The removal is a plain `git worktree remove`,
// which refuses a changed tree again on its own. The tree's branch is deleted only when it
// carries the caller's branch prefix and its tip is reachable from another ref. Only files the
// worktree's own ignore rules hide go with the tree: build outputs and installed dependencies,
// the bulk of a worktree's bytes; and a synthetic link the tool put there (Options.Synthetic)
// when it points at the repository's own copy.
//
// A REAPER THAT CANNOT ASK DECLINES: when the process table cannot be read, no worktree can be
// shown unused, and nothing is removed (Result.Declined). The table is the jail's own: a
// process of another jail or of the host working in the tree is not seen, which is why the idle
// floor applies as well.
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
	// Synthetic are names the tool links into each worktree from the repository (pi-subagents
	// links `node_modules`). A link at one of them that points at `<repository>/<name>` makes
	// git report the tree untracked (`?? node_modules`: an ignore pattern ending in `/` does not
	// match a link), so when it is the only thing status reports, the link is removed and status
	// asked again. Nothing else at such a name is touched.
	Synthetic []string
	// BranchPrefix, when set, lets a removed tree's branch be deleted too, if its name starts
	// with it and its tip is reachable from another ref. Empty keeps every branch.
	BranchPrefix string
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
	// Unsaved are kept, with a clean status, because their HEAD is detached, an operation is in
	// progress, or their HEAD commit is on no other ref: removing them could lose a commit.
	// Reasons says which.
	Unsaved []string `json:"kept_unsaved,omitempty"`
	// Unknown are kept because something about them could not be read or checked.
	Unknown []string `json:"kept_unknown,omitempty"`
	// Emptied are empty prefix-named directories with no `.git`, removed.
	Emptied []string `json:"removed_empty,omitempty"`
	// Branches are the branches deleted after their trees were removed.
	Branches []string `json:"deleted_branches,omitempty"`
	// Reasons maps a kept tree to why, where its list alone does not say.
	Reasons map[string]string `json:"reasons,omitempty"`
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

	keep := func(list *[]string, wt, reason string) {
		*list = append(*list, wt)
		if reason != "" {
			if res.Reasons == nil {
				res.Reasons = map[string]string{}
			}
			res.Reasons[wt] = reason
		}
	}
	commons := map[string]bool{}
	for _, wt := range candidates(o.Base, o.Prefix) {
		if _, err := os.Lstat(filepath.Join(wt, ".git")); errors.Is(err, fs.ErrNotExist) {
			// A directory the tool left with no worktree in it: removed only when empty.
			if os.Remove(wt) == nil {
				res.Emptied = append(res.Emptied, wt)
			}
			continue
		}
		w, err := readWorktree(wt)
		if err != nil {
			keep(&res.Unknown, wt, err.Error())
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
			clean, err := o.clean(wt, w)
			if err != nil {
				keep(&res.Unknown, wt, "git status: "+err.Error())
				continue
			}
			if !clean {
				res.Dirty = append(res.Dirty, wt)
				continue
			}
			branch, why, err := o.saved(wt, w.admin, w.common)
			if err != nil {
				keep(&res.Unknown, wt, err.Error())
				continue
			}
			if why != "" {
				keep(&res.Unsaved, wt, why)
				continue
			}
			// No --force: git refuses again if anything changed since the status.
			if _, err := o.Git(w.common, "worktree", "remove", wt); err != nil {
				keep(&res.Unknown, wt, "git worktree remove: "+err.Error())
				continue
			}
			res.Removed = append(res.Removed, wt)
			o.deleteBranch(&res, w.common, branch)
		}
	}

	for _, r := range o.Repos {
		if c, ok := commonDirOf(r); ok {
			commons[c] = true
		}
	}
	for _, c := range sortedKeys(commons) {
		for _, g := range goneRegistrations(c, bases, o.Prefix, now, o.Floor) {
			branch, why, err := o.saved("", g.admin, c)
			if err != nil {
				keep(&res.Unknown, g.path, err.Error())
				continue
			}
			if why != "" {
				keep(&res.Unsaved, g.path, why)
				continue
			}
			if _, err := o.Git(c, "worktree", "remove", g.path); err != nil {
				keep(&res.Unknown, g.path, "git worktree remove (directory gone): "+err.Error())
				continue
			}
			res.Pruned = append(res.Pruned, g.path)
			o.deleteBranch(&res, c, branch)
		}
	}
	for _, l := range [][]string{res.Removed, res.Pruned, res.Dirty, res.Live, res.Locked, res.Young,
		res.Unsaved, res.Unknown, res.Emptied, res.Branches} {
		sort.Strings(l)
	}
	return res
}

// clean reports whether wt's status is empty, after removing a synthetic link the tool made
// when that link is the only thing status reports.
func (o Options) clean(wt string, w worktree) (bool, error) {
	status, err := o.Git(wt, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return false, err
	}
	lines := nonEmptyLines(status)
	if len(lines) == 0 {
		return true, nil
	}
	repoTree := ""
	if filepath.Base(w.common) == ".git" {
		repoTree = filepath.Dir(w.common)
	}
	var links []string
	for _, l := range lines {
		name, ok := strings.CutPrefix(l, "?? ")
		name = strings.TrimSuffix(name, "/")
		if !ok || repoTree == "" || !slicesContains(o.Synthetic, name) {
			return false, nil
		}
		p := filepath.Join(wt, name)
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			return false, nil
		}
		target, err := os.Readlink(p)
		if err != nil || !samePath(absFrom(wt, target), filepath.Join(repoTree, name)) {
			return false, nil
		}
		links = append(links, p)
	}
	for _, p := range links {
		if err := os.Remove(p); err != nil {
			return false, err
		}
	}
	status, err = o.Git(wt, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return false, err
	}
	return len(nonEmptyLines(status)) == 0, nil
}

// inProgress are the admin-dir entries git keeps while an operation is under way in a
// worktree: its state lives there, not in the tree, so a clean status says nothing about it.
var inProgress = []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"}

// saved decides whether a clean worktree's commits are safe without it. It returns the tree's
// branch (refs/heads/…), and a reason to keep it when they may not be: a detached HEAD, an
// operation in progress, or a HEAD commit no other ref reaches. wt is "" for a registration
// whose tree is gone; HEAD is then read from the registration.
func (o Options) saved(wt, admin, common string) (branch, why string, err error) {
	for _, name := range inProgress {
		if _, err := os.Lstat(filepath.Join(admin, name)); err == nil {
			return "", "an operation is in progress (" + name + ")", nil
		}
	}
	head, err := readLine(filepath.Join(admin, "HEAD"))
	if err != nil {
		return "", "", fmt.Errorf("its HEAD cannot be read: %v", err)
	}
	ref, ok := strings.CutPrefix(head, "ref: ")
	if !ok {
		return "", "its HEAD is detached", nil
	}
	if wt != "" {
		// git's own answer, beside the admin file's.
		if _, err := o.Git(wt, "symbolic-ref", "-q", "HEAD"); err != nil {
			return "", "its HEAD is detached", nil
		}
	}
	ref = strings.TrimSpace(ref)
	tip, err := o.Git(common, "rev-parse", "--verify", "-q", ref+"^{commit}")
	if err != nil {
		return "", "its branch " + ref + " does not resolve to a commit", nil
	}
	tip = strings.TrimSpace(tip)
	refs, err := o.Git(common, "for-each-ref", "--contains", tip, "--format=%(refname)")
	if err != nil {
		return "", "", fmt.Errorf("git for-each-ref --contains: %v", err)
	}
	for _, r := range nonEmptyLines(refs) {
		if r != ref {
			return ref, "", nil
		}
	}
	return "", "its HEAD commit is on no other ref", nil
}

// deleteBranch deletes ref when it carries the branch prefix and its tip is still reachable
// from another ref.
func (o Options) deleteBranch(res *Result, common, ref string) {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	if o.BranchPrefix == "" || !ok || !strings.HasPrefix(name, o.BranchPrefix) {
		return
	}
	tip, err := o.Git(common, "rev-parse", "--verify", "-q", ref+"^{commit}")
	if err != nil {
		return
	}
	refs, err := o.Git(common, "for-each-ref", "--contains", strings.TrimSpace(tip), "--format=%(refname)")
	if err != nil {
		return
	}
	for _, r := range nonEmptyLines(refs) {
		if r != ref {
			if _, err := o.Git(common, "branch", "-D", name); err == nil {
				res.Branches = append(res.Branches, name)
			} else {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: git branch -D: %v", name, err))
			}
			return
		}
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, "\r"))
		}
	}
	return out
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
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
	admin      string
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
	w.admin = admin
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
// goneReg is one such registration: the tree it names and its admin directory.
type goneReg struct{ path, admin string }

func goneRegistrations(common string, bases []string, prefix string, now time.Time, floor time.Duration) []goneReg {
	dir := filepath.Join(common, "worktrees")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []goneReg
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
		out = append(out, goneReg{path: wt, admin: admin})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
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
