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
