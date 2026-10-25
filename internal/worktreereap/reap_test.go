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

// A commit only the tree's own branch holds is kept with the tree.
func TestAWorktreeWhoseCommitIsOnNoOtherRefIsKept(t *testing.T) {
	f := newFixture(t)
	wt := f.add(t, "pi-worktree-ahead-0", old)
	f.commit(t, wt, "work")
	f.age(t, wt, "pi-worktree-ahead-0", old)
	keptUnsaved(t, f, f.reapWith(Options{BranchPrefix: "b-"}), wt, "no other ref")
	if !strings.Contains(f.git(t, f.repo, "branch", "--list", "b-pi-worktree-ahead-0"), "b-pi-worktree-ahead-0") {
		t.Fatal("the branch holding the only copy of a commit was deleted")
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
	t.Run("a commit on no other ref", func(t *testing.T) {
		f := newFixture(t)
		wt := f.add(t, "pi-worktree-gone-0", old)
		f.commit(t, wt, "work")
		if err := os.RemoveAll(wt); err != nil {
			t.Fatal(err)
		}
		f.age(t, wt, "pi-worktree-gone-0", old)
		keptUnsaved(t, f, f.reap(), wt, "no other ref")
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

// A prefix-named directory with no worktree in it is removed only when empty.
func TestAnEmptyToolDirectoryIsRemoved(t *testing.T) {
	f := newFixture(t)
	empty := filepath.Join(f.sub, "pi-worktree-empty-0")
	full := filepath.Join(f.sub, "pi-worktree-full-0")
	for _, d := range []string{empty, full} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "notes"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := f.reap()
	if !slices.Equal(res.Emptied, []string{empty}) || exists(empty) {
		t.Fatalf("Emptied = %v, want [%s]", res.Emptied, empty)
	}
	if !exists(filepath.Join(full, "notes")) {
		t.Fatal("a directory holding files was touched")
	}
}
