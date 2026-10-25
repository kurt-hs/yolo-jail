package worktreereap

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixture is a repository and a tool's worktree directory beside it, holding one subdirectory
// per repository as pi-subagents keeps it.
type fixture struct {
	repo, base, sub string
	now             time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{repo: filepath.Join(root, "repo"), base: filepath.Join(root, "base"), now: time.Now()}
	f.sub = filepath.Join(f.base, "repo")
	if err := os.MkdirAll(f.sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f.git(t, root, "init", "-q", "-b", "main", f.repo)
	if err := os.WriteFile(filepath.Join(f.repo, "tracked"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(t, f.repo, "add", ".")
	f.git(t, f.repo, "commit", "-q", "-m", "init")
	return f
}

func (f *fixture) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// add makes a worktree named name under the per-repository subdirectory, idle for age.
func (f *fixture) add(t *testing.T, name string, age time.Duration) string {
	t.Helper()
	wt := filepath.Join(f.sub, name)
	f.git(t, f.repo, "worktree", "add", "-q", "-b", "b-"+name, wt)
	f.age(t, wt, name, age)
	return wt
}

func (f *fixture) age(t *testing.T, wt, name string, age time.Duration) {
	t.Helper()
	when := f.now.Add(-age)
	admin := filepath.Join(f.repo, ".git", "worktrees", name)
	for _, p := range []string{filepath.Join(admin, "HEAD"), filepath.Join(admin, "index"), admin, wt} {
		if err := os.Chtimes(p, when, when); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
}

func (f *fixture) registered(t *testing.T, wt string) bool {
	t.Helper()
	return strings.Contains(f.git(t, f.repo, "worktree", "list", "--porcelain"), "worktree "+wt+"\n")
}

func (f *fixture) reap(cwds ...string) Result {
	return Reap(Options{Base: f.base, Prefix: "pi-worktree-", Repos: []string{f.repo},
		Now:      func() time.Time { return f.now },
		LiveCwds: func() ([]string, error) { return cwds, nil }})
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

const old = 3 * 24 * time.Hour

func TestACleanIdleWorktreeIsRemovedWithItsRegistration(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-clean-0", old)
	// Ignored bytes (installed dependencies, build outputs) are not work, and go with the tree.
	if err := os.MkdirAll(filepath.Join(wt, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.age(t, wt, "pi-worktree-clean-0", old)
	res := f.reap()
	if !slices.Equal(res.Removed, []string{wt}) {
		t.Fatalf("Removed = %v, want [%s]; result %+v", res.Removed, wt, res)
	}
	if exists(wt) {
		t.Error("the worktree's directory is still there")
	}
	if f.registered(t, wt) {
		t.Error("git still registers the removed worktree")
	}
	if !strings.Contains(f.git(t, f.repo, "branch", "--list", "b-pi-worktree-clean-0"), "b-pi-worktree-clean-0") {
		t.Error("the worktree's branch was deleted; a commit it made would be unreachable")
	}
}

func TestADirtyWorktreeIsKeptAndReported(t *testing.T) {
	f := newFixture(t)
	untracked := f.add(t, "pi-worktree-untracked-0", old)
	if err := os.WriteFile(filepath.Join(untracked, "new.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	modified := f.add(t, "pi-worktree-modified-0", old)
	if err := os.WriteFile(filepath.Join(modified, "tracked"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.age(t, untracked, "pi-worktree-untracked-0", old)
	f.age(t, modified, "pi-worktree-modified-0", old)
	res := f.reap()
	if len(res.Removed) != 0 {
		t.Fatalf("removed %v, which hold uncommitted work", res.Removed)
	}
	if !slices.Equal(res.Dirty, []string{modified, untracked}) {
		t.Fatalf("Dirty = %v, want both", res.Dirty)
	}
	for _, wt := range []string{untracked, modified} {
		if !exists(wt) || !f.registered(t, wt) {
			t.Errorf("%s was touched", wt)
		}
	}
}

func TestLiveLockedYoungAndForeignWorktreesAreKept(t *testing.T) {
	f := newFixture(t)
	live := f.add(t, "pi-worktree-live-0", old)
	locked := f.add(t, "pi-worktree-locked-0", old)
	f.git(t, f.repo, "worktree", "lock", locked)
	f.age(t, locked, "pi-worktree-locked-0", old)
	young := f.add(t, "pi-worktree-young-0", time.Hour)
	foreign := filepath.Join(f.base, "task")
	f.git(t, f.repo, "worktree", "add", "-q", "-b", "task", foreign)
	f.age(t, foreign, "task", old)

	res := f.reap(filepath.Join(live, "sub"))
	if len(res.Removed) != 0 {
		t.Fatalf("removed %v", res.Removed)
	}
	if !slices.Equal(res.Live, []string{live}) || !slices.Equal(res.Locked, []string{locked}) ||
		!slices.Equal(res.Young, []string{young}) {
		t.Fatalf("live %v, locked %v, young %v", res.Live, res.Locked, res.Young)
	}
	for _, wt := range []string{live, locked, young, foreign} {
		if !exists(wt) {
			t.Errorf("%s is gone", wt)
		}
	}
}

// The real process table: a process working inside the tree keeps it.
func TestAProcessWorkingInATreeKeepsIt(t *testing.T) {
	if _, err := os.Readlink("/proc/self/cwd"); err != nil {
		t.Skip("no /proc here")
	}
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-busy-0", old)
	cmd := exec.Command("sleep", "60")
	cmd.Dir = wt
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	res := Reap(Options{Base: f.base, Prefix: "pi-worktree-", Now: func() time.Time { return f.now }})
	if !slices.Equal(res.Live, []string{wt}) || !exists(wt) {
		t.Fatalf("a tree a process works in was not kept as live: %+v", res)
	}
}

func TestAReaperThatCannotReadTheProcessTableDeclines(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-clean-0", old)
	res := Reap(Options{Base: f.base, Prefix: "pi-worktree-", Now: func() time.Time { return f.now },
		LiveCwds: func() ([]string, error) { return nil, errors.New("no /proc") }})
	if res.Declined == "" || len(res.Removed) != 0 || !exists(wt) {
		t.Fatalf("a reaper blind to liveness acted: %+v", res)
	}
}

func TestARegistrationWhoseTreeIsGoneIsDropped(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-gone-0", old)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	f.age(t, wt, "pi-worktree-gone-0", old)
	// A gone tree of someone else's, outside the tool's directory, is not the reaper's.
	other := filepath.Join(filepath.Dir(f.repo), "elsewhere")
	f.git(t, f.repo, "worktree", "add", "-q", "-b", "elsewhere", other)
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	f.age(t, other, "elsewhere", old)

	res := f.reap()
	if !slices.Equal(res.Pruned, []string{wt}) {
		t.Fatalf("Pruned = %v, want [%s]; errors %v", res.Pruned, wt, res.Errors)
	}
	if f.registered(t, wt) {
		t.Error("the gone tree is still registered")
	}
	if !f.registered(t, other) {
		t.Error("a registration outside the tool's directory was dropped")
	}
}

// Main writes its Result to the report beside the trees, and a second pass while one holds the
// directory's lock exits without acting.
func TestMainWritesTheReportAndSerializes(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-clean-0", old)
	lock, err := os.OpenFile(filepath.Join(f.base, lockName), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	args := []string{"--base", f.base, "--prefix", "pi-worktree-", "--floor", "1h"}
	if rc := mainTo(args, io.Discard); rc != 0 || !exists(wt) || exists(filepath.Join(f.base, ReportName)) {
		t.Fatalf("a pass ran while another held the lock (rc %d)", rc)
	}
	lock.Close()
	if rc := mainTo(args, io.Discard); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	b, err := os.ReadFile(filepath.Join(f.base, ReportName))
	if err != nil {
		t.Fatal(err)
	}
	var res Result
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Removed, []string{wt}) || exists(wt) {
		t.Fatalf("report %s; tree present %v", b, exists(wt))
	}
	if rc := mainTo([]string{"--base", f.base}, io.Discard); rc != 2 {
		t.Fatalf("a call with no prefix = %d, want usage 2", rc)
	}
}

// gitMay runs git and returns its error rather than failing: a rebase told to stop does.
func (f *fixture) gitMay(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_EDITOR=true",
		"GIT_SEQUENCE_EDITOR=true")
	return cmd.Run()
}

func (f *fixture) commit(t *testing.T, wt, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, msg), []byte(msg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(t, wt, "add", msg)
	f.git(t, wt, "commit", "-q", "-m", msg)
}

func (f *fixture) reapWith(o Options) Result {
	o.Base, o.Prefix, o.Repos = f.base, "pi-worktree-", []string{f.repo}
	o.Now = func() time.Time { return f.now }
	o.LiveCwds = func() ([]string, error) { return nil, nil }
	return Reap(o)
}

func keptUnsaved(t *testing.T, f *fixture, res Result, wt, why string) {
	t.Helper()
	if len(res.Removed) != 0 || len(res.Pruned) != 0 {
		t.Fatalf("removed %v, pruned %v; a commit could be lost", res.Removed, res.Pruned)
	}
	if !slices.Equal(res.Unsaved, []string{wt}) || !strings.Contains(res.Reasons[wt], why) {
		t.Fatalf("Unsaved = %v, reasons %v; want %s kept because %q", res.Unsaved, res.Reasons, wt, why)
	}
	if !f.registered(t, wt) {
		t.Error("the kept tree's registration was dropped")
	}
}

// A clean status is not enough: a detached HEAD may be the only name of a commit.
func TestACleanDetachedWorktreeIsKept(t *testing.T) {
	f := newFixture(t)
	wt := filepath.Join(f.sub, "pi-worktree-detached-0")
	f.git(t, f.repo, "worktree", "add", "-q", "--detach", wt)
	f.age(t, wt, "pi-worktree-detached-0", old)
	keptUnsaved(t, f, f.reap(), wt, "detached")
	if !exists(wt) {
		t.Fatal("the detached tree is gone")
	}
}

// The case that lost data: an amend in the middle of a rebase leaves a clean tree whose new
// commit only its detached HEAD names.
func TestAWorktreeAmendedMidRebaseSurvives(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-rebase-0", old)
	f.commit(t, wt, "one")
	if err := f.gitMay(wt, "rebase", "-q", "--force-rebase", "--exec", "false", "main"); err == nil {
		t.Fatal("the rebase did not stop")
	}
	f.git(t, wt, "commit", "-q", "--amend", "-m", "amended")
	amended := strings.TrimSpace(f.git(t, wt, "rev-parse", "HEAD"))
	if s := f.git(t, wt, "status", "--porcelain"); strings.TrimSpace(s) != "" {
		t.Fatalf("the fixture's tree is not clean: %s", s)
	}
	f.age(t, wt, "pi-worktree-rebase-0", old)
	keptUnsaved(t, f, f.reap(), wt, "in progress")
	if !exists(wt) {
		t.Fatal("the tree mid-rebase is gone")
	}
	if got := strings.TrimSpace(f.git(t, wt, "rev-parse", "HEAD")); got != amended {
		t.Fatalf("HEAD = %s, want the amended commit %s", got, amended)
	}
}

// Every operation git tracks in the admin dir keeps a clean tree on a branch another ref holds.
func TestAWorktreeWithAnOperationInProgressIsKept(t *testing.T) {
	for _, marker := range inProgress {
		t.Run(marker, func(t *testing.T) {
			f := newFixture(t)
			name := "pi-worktree-op-0"
			wt := f.add(t, name, old)
			p := filepath.Join(f.repo, ".git", "worktrees", name, marker)
			var err error
			if strings.HasPrefix(marker, "rebase-") {
				err = os.Mkdir(p, 0o755)
			} else {
				err = os.WriteFile(p, []byte("x\n"), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
			f.age(t, wt, name, old)
			keptUnsaved(t, f, f.reap(), wt, marker)
		})
	}
}

func (f *fixture) hasBranch(t *testing.T, name string) bool {
	t.Helper()
	return strings.Contains(f.git(t, f.repo, "branch", "--list", name), name)
}

// A commit only the tree's own branch holds does not keep the tree: the branch and its reflog
// outlive `git worktree remove`. It does keep the branch.
func TestAWorktreeWhoseCommitIsOnlyOnItsBranchGoesAndTheBranchStays(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-ahead-0", old)
	f.commit(t, wt, "work")
	tip := strings.TrimSpace(f.git(t, wt, "rev-parse", "HEAD"))
	f.age(t, wt, "pi-worktree-ahead-0", old)
	res := f.reapWith(Options{BranchPrefix: "b-"})
	if !slices.Equal(res.Removed, []string{wt}) || exists(wt) {
		t.Fatalf("Removed = %v, want [%s]; result %+v", res.Removed, wt, res)
	}
	if !f.hasBranch(t, "b-pi-worktree-ahead-0") || len(res.Branches) != 0 {
		t.Fatalf("the branch holding the only copy of a commit was deleted (Branches %v)", res.Branches)
	}
	if got := strings.TrimSpace(f.git(t, f.repo, "rev-parse", "b-pi-worktree-ahead-0")); got != tip {
		t.Fatalf("the branch moved: %s, want %s", got, tip)
	}
}

// Only a local branch or a tag holds a tip for the branch's deletion: a stash entry or a
// remote-tracking ref can go away (a `git stash drop`, a fetch), and the deleted branch's
// reflog would go with it.
func TestAStashOrRemoteRefIsNoHolderForTheBranch(t *testing.T) {
	for _, holder := range []string{"refs/stash", "refs/remotes/origin/x"} {
		t.Run(holder, func(t *testing.T) {
			f := newFixture(t)
			wt := filepath.Join(f.sub, "pi-worktree-run-0")
			f.git(t, f.repo, "worktree", "add", "-q", "-b", "pi-subagents/run-0", wt)
			f.commit(t, wt, "work")
			if holder == "refs/stash" {
				// A stash whose base is the tip: its commit contains the tip.
				if err := os.WriteFile(filepath.Join(wt, "tracked"), []byte("stashed\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				f.git(t, wt, "stash", "-q")
			} else {
				f.git(t, f.repo, "update-ref", holder, "pi-subagents/run-0")
			}
			f.age(t, wt, "pi-worktree-run-0", old)
			res := f.reapWith(Options{BranchPrefix: "pi-subagents/"})
			if !slices.Equal(res.Removed, []string{wt}) {
				t.Fatalf("Removed = %v; result %+v", res.Removed, res)
			}
			if !f.hasBranch(t, "pi-subagents/run-0") || len(res.Branches) != 0 {
				t.Fatalf("the branch was deleted with only %s holding its tip", holder)
			}
		})
	}
	t.Run("a tag", func(t *testing.T) {
		f := newFixture(t)
		wt := filepath.Join(f.sub, "pi-worktree-run-0")
		f.git(t, f.repo, "worktree", "add", "-q", "-b", "pi-subagents/run-0", wt)
		f.commit(t, wt, "work")
		f.git(t, f.repo, "tag", "kept", "pi-subagents/run-0")
		f.age(t, wt, "pi-worktree-run-0", old)
		res := f.reapWith(Options{BranchPrefix: "pi-subagents/"})
		if !slices.Equal(res.Branches, []string{"pi-subagents/run-0"}) {
			t.Fatalf("Branches = %v, want the branch a tag holds deleted", res.Branches)
		}
	})
}

// Index bits hide an edit from `git status`, and `git worktree remove` deletes it.
func TestAnEditTheIndexHidesKeepsTheTree(t *testing.T) {
	t.Run("assume-unchanged", func(t *testing.T) {
		f := newFixture(t)
		name := "pi-worktree-au-0"
		wt := f.add(t, name, old)
		f.git(t, wt, "update-index", "--assume-unchanged", "tracked")
		if err := os.WriteFile(filepath.Join(wt, "tracked"), []byte("edited\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, name, old)
		res := f.reap()
		if len(res.Removed) != 0 || !slices.Equal(res.Dirty, []string{wt}) || !strings.Contains(res.Reasons[wt], "assume-unchanged") {
			t.Fatalf("an assume-unchanged edit was not kept: %+v", res)
		}
		if b, _ := os.ReadFile(filepath.Join(wt, "tracked")); string(b) != "edited\n" {
			t.Fatal("the hidden edit is gone")
		}
	})
	t.Run("skip-worktree on disk", func(t *testing.T) {
		f := newFixture(t)
		name := "pi-worktree-sw-0"
		wt := f.add(t, name, old)
		f.git(t, wt, "update-index", "--skip-worktree", "tracked")
		if err := os.WriteFile(filepath.Join(wt, "tracked"), []byte("edited\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, name, old)
		res := f.reap()
		if len(res.Removed) != 0 || !slices.Equal(res.Dirty, []string{wt}) || !strings.Contains(res.Reasons[wt], "skip-worktree") {
			t.Fatalf("a skip-worktree edit was not kept: %+v", res)
		}
	})
	t.Run("skip-worktree absent (a sparse checkout)", func(t *testing.T) {
		f := newFixture(t)
		name := "pi-worktree-sparse-0"
		wt := f.add(t, name, old)
		f.git(t, wt, "update-index", "--skip-worktree", "tracked")
		if err := os.Remove(filepath.Join(wt, "tracked")); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, name, old)
		if res := f.reap(); !slices.Equal(res.Removed, []string{wt}) {
			t.Fatalf("a sparse tree with nothing hidden was kept: %+v", res)
		}
	})
}

// A tree with submodules is kept, saying why: their own work is in repositories the reaper
// does not examine.
func TestATreeWithSubmodulesIsKeptAsUnknown(t *testing.T) {
	f := newFixture(t)
	name := "pi-worktree-sub-0"
	wt := f.add(t, name, old)
	f.commit(t, wt, ".gitmodules")
	f.age(t, wt, name, old)
	res := f.reap()
	if len(res.Removed) != 0 || !slices.Equal(res.Unknown, []string{wt}) || !strings.Contains(res.Reasons[wt], "submodules") {
		t.Fatalf("a tree with submodules was not kept with its reason: %+v", res)
	}
}

// The same checks hold for a registration whose tree is gone: its HEAD lives in the admin dir.
func TestAGoneRegistrationHoldingACommitIsKept(t *testing.T) {
	t.Run("detached", func(t *testing.T) {
		f := newFixture(t)
		wt := filepath.Join(f.sub, "pi-worktree-gone-0")
		f.git(t, f.repo, "worktree", "add", "-q", "--detach", wt)
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, "pi-worktree-gone-0", old)
		keptUnsaved(t, f, f.reap(), wt, "detached")
	})
	t.Run("a commit only its branch holds", func(t *testing.T) {
		f := newFixture(t)
		wt := f.add(t, "pi-worktree-gone-0", old)
		f.commit(t, wt, "work")
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, "pi-worktree-gone-0", old)
		res := f.reapWith(Options{BranchPrefix: "b-"})
		if !slices.Equal(res.Pruned, []string{wt}) || !f.hasBranch(t, "b-pi-worktree-gone-0") {
			t.Fatalf("Pruned = %v, branch kept %v; want the registration dropped and the branch kept",
				res.Pruned, f.hasBranch(t, "b-pi-worktree-gone-0"))
		}
	})
	t.Run("mid-rebase", func(t *testing.T) {
		f := newFixture(t)
		name := "pi-worktree-gone-0"
		wt := f.add(t, name, old)
		if err := os.Mkdir(filepath.Join(f.repo, ".git", "worktrees", name, "rebase-merge"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, name, old)
		keptUnsaved(t, f, f.reap(), wt, "rebase-merge")
	})
}

// pi-subagents links the repository's node_modules into each tree; a `node_modules/` ignore
// pattern does not match a link, so the tree reads untracked. A link to the repository's own
// copy is removed and status asked again; a link anywhere else is the tree's content.
func TestASyntheticLinkToTheRepositoryDoesNotKeepATree(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.repo, "node_modules", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := f.add(t, "pi-worktree-linked-0", old)
	if err := os.Symlink(filepath.Join(f.repo, "node_modules"), filepath.Join(linked, "node_modules")); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	foreign := f.add(t, "pi-worktree-foreign-0", old)
	if err := os.Symlink(elsewhere, filepath.Join(foreign, "node_modules")); err != nil {
		t.Fatal(err)
	}
	f.age(t, linked, "pi-worktree-linked-0", old)
	f.age(t, foreign, "pi-worktree-foreign-0", old)

	res := f.reapWith(Options{Synthetic: []string{"node_modules"}})
	if !slices.Equal(res.Removed, []string{linked}) || exists(linked) {
		t.Fatalf("Removed = %v, want [%s]; result %+v", res.Removed, linked, res)
	}
	if !slices.Equal(res.Dirty, []string{foreign}) || !exists(filepath.Join(foreign, "node_modules")) {
		t.Fatalf("Dirty = %v, want the tree whose link points elsewhere kept whole", res.Dirty)
	}
	if !exists(filepath.Join(f.repo, "node_modules", "dep")) {
		t.Fatal("removing the link touched the repository's own node_modules")
	}
	// Without the name, the same link keeps the tree.
	f2 := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f2.repo, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := f2.add(t, "pi-worktree-linked-0", old)
	if err := os.Symlink(filepath.Join(f2.repo, "node_modules"), filepath.Join(wt, "node_modules")); err != nil {
		t.Fatal(err)
	}
	f2.age(t, wt, "pi-worktree-linked-0", old)
	if res := f2.reap(); !slices.Equal(res.Dirty, []string{wt}) {
		t.Fatalf("with no synthetic name, Dirty = %v, want [%s]", res.Dirty, wt)
	}
}

// pi-subagents links `<toplevel>/node_modules` from whichever tree its lead ran in: a link to
// another work tree of the same repository is the tool's too. And the link goes only once
// every keep check has passed: a tree kept for any reason keeps its link.
func TestASyntheticLinkToAnotherWorkTreeCountsAndStaysWithAKeptTree(t *testing.T) {
	f := newFixture(t)
	lead := filepath.Join(filepath.Dir(f.repo), "lead")
	f.git(t, f.repo, "worktree", "add", "-q", "-b", "lead", lead)
	if err := os.MkdirAll(filepath.Join(lead, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	fromLead := f.add(t, "pi-worktree-lead-0", old)
	if err := os.Symlink(filepath.Join(lead, "node_modules"), filepath.Join(fromLead, "node_modules")); err != nil {
		t.Fatal(err)
	}
	detached := filepath.Join(f.sub, "pi-worktree-detached-0")
	f.git(t, f.repo, "worktree", "add", "-q", "--detach", detached)
	if err := os.Symlink(filepath.Join(lead, "node_modules"), filepath.Join(detached, "node_modules")); err != nil {
		t.Fatal(err)
	}
	f.age(t, fromLead, "pi-worktree-lead-0", old)
	f.age(t, detached, "pi-worktree-detached-0", old)

	res := f.reapWith(Options{Synthetic: []string{"node_modules"}})
	if !slices.Equal(res.Removed, []string{fromLead}) {
		t.Fatalf("Removed = %v, want the tree linked to the lead's node_modules; result %+v", res.Removed, res)
	}
	if !slices.Equal(res.Unsaved, []string{detached}) {
		t.Fatalf("Unsaved = %v, want the detached tree", res.Unsaved)
	}
	if _, err := os.Lstat(filepath.Join(detached, "node_modules")); err != nil {
		t.Fatalf("the kept tree lost its link: %v", err)
	}
	if !exists(filepath.Join(lead, "node_modules")) {
		t.Fatal("the lead's node_modules was touched")
	}
}

// A removed tree's branch goes too when it carries the tool's prefix and another ref holds its
// tip; a branch without the prefix stays.
func TestARemovedTreesPrefixedBranchIsDeleted(t *testing.T) {
	f := newFixture(t)
	tool := filepath.Join(f.sub, "pi-worktree-run-0")
	f.git(t, f.repo, "worktree", "add", "-q", "-b", "pi-subagents/run-0", tool)
	f.age(t, tool, "pi-worktree-run-0", old)
	other := f.add(t, "pi-worktree-other-0", old)

	res := f.reapWith(Options{BranchPrefix: "pi-subagents/"})
	if !slices.Equal(res.Removed, []string{other, tool}) {
		t.Fatalf("Removed = %v; result %+v", res.Removed, res)
	}
	if !slices.Equal(res.Branches, []string{"pi-subagents/run-0"}) {
		t.Fatalf("Branches = %v, want [pi-subagents/run-0]", res.Branches)
	}
	branches := f.git(t, f.repo, "branch", "--list")
	if strings.Contains(branches, "pi-subagents/run-0") {
		t.Error("the tool's branch survived")
	}
	if !strings.Contains(branches, "b-pi-worktree-other-0") {
		t.Error("a branch without the prefix was deleted")
	}
}

// A prefix-named directory with no worktree in it is removed only when empty and idle past the
// floor: one the tool just made is about to be filled.
func TestAnEmptyToolDirectoryIsRemoved(t *testing.T) {
	f := newFixture(t)
	empty := filepath.Join(f.sub, "pi-worktree-empty-0")
	full := filepath.Join(f.sub, "pi-worktree-full-0")
	fresh := filepath.Join(f.sub, "pi-worktree-fresh-0")
	for _, d := range []string{empty, full, fresh} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "notes"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := f.now.Add(-old)
	for _, d := range []string{empty, full} {
		if err := os.Chtimes(d, when, when); err != nil {
			t.Fatal(err)
		}
	}
	res := f.reap()
	if !slices.Equal(res.Emptied, []string{empty}) || exists(empty) {
		t.Fatalf("Emptied = %v, want [%s]", res.Emptied, empty)
	}
	if !exists(filepath.Join(full, "notes")) {
		t.Fatal("a directory holding files was touched")
	}
	if !exists(fresh) {
		t.Fatal("an empty directory younger than the floor was removed")
	}
}
