// Package worktreereap bounds a tool's managed worktree directory: it removes the worktrees a
// tool made there and left behind, once each is clean, unlocked, unused by any process and
// idle past a floor, through `git worktree remove`, so git's registration goes with the tree
// (docs/reference/disk-footprint.md "Tool-managed worktrees in the durable dir").
//
// IT RUNS IN THE JAIL, NEVER ON THE HOST. Host code never runs git in a workspace and never
// removes anything beneath the durable dir (docs/reference/jail-home.md DS-D3): the
// workspace's `.git/config` is jail-writable and names programs git runs. In the jail, git
// running the jail's own config is the jail running its own code. The caller is the pack that
// pointed the tool at the directory (packs/pi/home/.pi/agent/extensions/yolo-durable-worktrees.js, through
// `yolo-jaild reap-worktrees`); core knows no tool's name, only the directory and the name
// prefix the pack hands it.
//
// IT NEVER DISCARDS WORK. An empty `git status` is not enough, because a tree can be clean and
// still hold the only copy of a commit (an amend in the middle of a rebase leaves a detached
// HEAD no ref names) or an edit status cannot see (a file marked assume-unchanged, or
// skip-worktree and on disk). A tree is removed only when, besides a clean status, no index
// bit hides an edit, it has no submodules, its HEAD is a branch, and no operation (rebase,
// merge, cherry-pick, revert, bisect) is in progress in it; anything else is kept and reported
// (Result.Dirty, Result.Unsaved, Result.Unknown). A tree on a branch needs nothing more: the
// branch and its reflog outlive the removal. The removal is a plain `git worktree remove`,
// which refuses a changed tree again on its own. The tree's branch is deleted only when it
// carries the caller's branch prefix and another local branch or a tag holds its tip; a stash
// or a remote-tracking ref is no holder. Only files the worktree's own ignore rules hide go
// with the tree: build outputs and installed dependencies, the bulk of a worktree's bytes; and
// a synthetic link the tool put there (Options.Synthetic) when it points at the `node_modules`
// of one of the repository's work trees, removed only once every keep check has passed.
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
	// links `<toplevel>/node_modules`). Such a link makes git report the tree untracked (`??
	// node_modules`: an ignore pattern ending in `/` does not match a link), so when status
	// reports nothing else and the link points at `<work tree>/<name>` for one of the
	// repository's other work trees, the tree counts as clean and the link is removed just
	// before the tree. Nothing else at such a name is touched.
	Synthetic []string
	// BranchPrefix, when set, lets a removed tree's branch be deleted too, if its name starts
	// with it and another local branch or a tag holds its tip. Empty keeps every branch.
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
	// Dirty are kept because they hold uncommitted changes or untracked files, or a file whose
	// edits the index hides (Reasons names it): the one list a person may need to act on.
	Dirty []string `json:"kept_dirty,omitempty"`
	// Live are kept because a running process works inside them.
	Live []string `json:"kept_live,omitempty"`
	// Locked are kept because git's lock is on them.
	Locked []string `json:"kept_locked,omitempty"`
	// Young are kept because they were active within the floor.
	Young []string `json:"kept_young,omitempty"`
	// Unsaved are kept, with a clean status, because their HEAD is detached or an operation is
	// in progress: removing them could lose a commit. Reasons says which.
	Unsaved []string `json:"kept_unsaved,omitempty"`
	// Unknown are kept because something about them could not be read or checked.
	Unknown []string `json:"kept_unknown,omitempty"`
	// Emptied are empty prefix-named directories with no `.git`, idle past the floor, removed.
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
			// A directory the tool left with no worktree in it: removed only when empty and idle
			// past the floor, so one the tool is about to fill is not taken from under it.
			fi, err := os.Lstat(wt)
			if err != nil || now.Sub(fi.ModTime()) < o.Floor {
				continue
			}
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
			if why := submodules(wt, w.admin); why != "" {
				keep(&res.Unknown, wt, why)
				continue
			}
			links, clean, err := o.status(wt, w.common)
			if err != nil {
				keep(&res.Unknown, wt, "git status: "+err.Error())
				continue
			}
			if !clean {
				res.Dirty = append(res.Dirty, wt)
				continue
			}
			why, err := o.hidden(wt)
			if err != nil {
				keep(&res.Unknown, wt, "git ls-files -v: "+err.Error())
				continue
			}
			if why != "" {
				keep(&res.Dirty, wt, why)
				continue
			}
			branch, why, err := o.saved(wt, w.admin)
			if err != nil {
				keep(&res.Unknown, wt, err.Error())
				continue
			}
			if why != "" {
				keep(&res.Unsaved, wt, why)
				continue
			}
			// Every keep check has passed: only now go the tool's links, which `git worktree
			// remove` would otherwise refuse as untracked. A failed remove puts them back.
			restore, err := removeLinks(links)
			if err != nil {
				keep(&res.Unknown, wt, err.Error())
				continue
			}
			// No --force: git refuses again if anything changed since the status.
			if _, err := o.Git(w.common, "worktree", "remove", wt); err != nil {
				restore()
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
			branch, why, err := o.saved("", g.admin)
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

// link is a synthetic link the tool made in a worktree, and what it points at as written.
type link struct{ path, target string }

// status reports whether wt's status is empty but for synthetic links the tool made, and
// returns those links. It removes nothing: the caller removes them only once every other keep
// check has passed.
func (o Options) status(wt, common string) ([]link, bool, error) {
	out, err := o.Git(wt, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return nil, false, err
	}
	var tops []string
	var links []link
	for _, l := range nonEmptyLines(out) {
		name, ok := strings.CutPrefix(l, "?? ")
		name = strings.TrimSuffix(name, "/")
		if !ok || !slicesContains(o.Synthetic, name) {
			return nil, false, nil
		}
		p := filepath.Join(wt, name)
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			return nil, false, nil
		}
		target, err := os.Readlink(p)
		if err != nil {
			return nil, false, nil
		}
		if tops == nil {
			tops = o.toplevels(common, wt)
		}
		match := false
		for _, top := range tops {
			if samePath(absFrom(wt, target), filepath.Join(top, name)) {
				match = true
				break
			}
		}
		if !match {
			return nil, false, nil
		}
		links = append(links, link{path: p, target: target})
	}
	return links, true, nil
}

// toplevels are the work trees of common's repository other than wt: the main worktree and every
// linked one. pi-subagents links `<toplevel>/node_modules` from whichever tree its lead ran in, so
// a synthetic link to any of them is the tool's.
func (o Options) toplevels(common, wt string) []string {
	var out []string
	if filepath.Base(common) == ".git" {
		out = append(out, filepath.Dir(common))
	}
	list, err := o.Git(common, "worktree", "list", "--porcelain")
	if err != nil {
		return out
	}
	for _, l := range nonEmptyLines(list) {
		if p, ok := strings.CutPrefix(l, "worktree "); ok && !samePath(p, wt) {
			out = append(out, p)
		}
	}
	return out
}

// removeLinks removes the tool's links and returns a function that puts them back.
func removeLinks(links []link) (func(), error) {
	var done []link
	restore := func() {
		for _, l := range done {
			_ = os.Symlink(l.target, l.path)
		}
	}
	for _, l := range links {
		if err := os.Remove(l.path); err != nil {
			restore()
			return func() {}, fmt.Errorf("removing the tool's link %s: %v", l.path, err)
		}
		done = append(done, l)
	}
	return restore, nil
}

// hidden is a reason to keep wt when its index hides edits from `git status`: a file marked
// assume-unchanged (a lowercase `git ls-files -v` tag), or one marked skip-worktree (`S`) that
// exists on disk. `git worktree remove` deletes either with the tree. A skip-worktree file that is
// absent is a sparse checkout doing its job, and holds nothing.
func (o Options) hidden(wt string) (string, error) {
	out, err := o.Git(wt, "ls-files", "-v", "-z")
	if err != nil {
		return "", err
	}
	for _, e := range strings.Split(out, "\x00") {
		if len(e) < 3 || e[1] != ' ' {
			continue
		}
		tag, path := e[0], e[2:]
		switch {
		case tag >= 'a' && tag <= 'z':
			return "a file is marked assume-unchanged (" + path + "), so git status cannot show an edit to it", nil
		case tag == 'S':
			if _, err := os.Lstat(filepath.Join(wt, path)); err == nil {
				return "a file is marked skip-worktree (" + path + ") and is on disk, so git status cannot show an edit to it", nil
			}
		}
	}
	return "", nil
}

// submodules is a reason to keep wt when it has submodules: their own uncommitted work and
// unpushed commits live in repositories this reaper does not examine.
func submodules(wt, admin string) string {
	for _, p := range []string{filepath.Join(wt, ".gitmodules"), filepath.Join(admin, "modules")} {
		if _, err := os.Lstat(p); err == nil {
			return "it has submodules, whose own changes and commits this reaper does not check; remove it yourself once they are saved"
		}
	}
	return ""
}

// inProgress are the admin-dir entries git keeps while an operation is under way in a
// worktree: its state lives there, not in the tree, so a clean status says nothing about it.
var inProgress = []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"}

// saved decides whether a clean worktree's commits are safe without it. It returns the tree's
// branch (refs/heads/…), and a reason to keep it when they may not be: a detached HEAD, whose
// commit only the tree's own reflog may name, or an operation in progress, whose state lives in
// the admin dir that the removal deletes. A tree on a branch needs nothing more: the branch and
// its reflog outlive `git worktree remove`. wt is "" for a registration whose tree is gone;
// HEAD is then read from the registration.
func (o Options) saved(wt, admin string) (branch, why string, err error) {
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
	return strings.TrimSpace(ref), "", nil
}

// deleteBranch deletes ref when it carries the branch prefix and another local branch or a tag
// holds its tip. Only those count: a stash entry or a remote-tracking ref is no holder, since a
// `git stash drop` or a fetch can take it away, and the deleted branch's reflog goes with it.
func (o Options) deleteBranch(res *Result, common, ref string) {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	if o.BranchPrefix == "" || !ok || !strings.HasPrefix(name, o.BranchPrefix) {
		return
	}
	tip, err := o.Git(common, "rev-parse", "--verify", "-q", ref+"^{commit}")
	if err != nil {
		return
	}
	refs, err := o.Git(common, "for-each-ref", "--contains", strings.TrimSpace(tip), "--format=%(refname)",
		"refs/heads/", "refs/tags/")
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
