package brokerscope

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// resolvedTempDir resolves t.TempDir() where it is MINTED, so a darwin /var → /private/var
// symlink cannot make a fixture path and a resolved one disagree (AGENTS.md, Testing).
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sampleConfig = `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:o/r.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "fork"]
	url = "https://github.com/me/r-fork" ; my fork
[remote "upstream"]
	url = ssh://git@github.com/O/R.git
[remote "elsewhere"]
	url = https://gitlab.com/x/y.git
[remote "rewritten"]
	url = gh:x/y
[include]
	path = /etc/evil.gitconfig
[remote.legacy]
	url = https://github.com/leg/acy
`

func TestReadRemotesTakesEveryGitHubRemote(t *testing.T) {
	ws := resolvedTempDir(t)
	writeFile(t, filepath.Join(ws, ".git", "config"), sampleConfig)
	r := ReadRemotes(ws, "github.com")
	if r.Problem != "" || r.GitConfig != filepath.Join(ws, ".git", "config") {
		t.Fatalf("read %+v", r)
	}
	want := []Remote{
		{Name: "legacy", Repo: "leg/acy"},
		{Name: "fork", Repo: "me/r-fork"},
		{Name: "origin", Repo: "o/r"},
		{Name: "upstream", Repo: "O/R"},
	}
	if !reflect.DeepEqual(r.Remotes, want) {
		t.Fatalf("remotes %+v, want %+v", r.Remotes, want)
	}
	if got := r.Repos(); !reflect.DeepEqual(got, []string{"leg/acy", "me/r-fork", "o/r"}) {
		t.Fatalf("repos %v", got)
	}
}

func TestReadRemotesWithNoGitIsEmptyWithoutAProblem(t *testing.T) {
	r := ReadRemotes(resolvedTempDir(t), "github.com")
	if r.Problem != "" || len(r.Remotes) != 0 || r.GitConfig != "" {
		t.Fatalf("read %+v", r)
	}
}

func TestReadRemotesFollowsNoSymlink(t *testing.T) {
	ws := resolvedTempDir(t)
	other := resolvedTempDir(t)
	writeFile(t, filepath.Join(other, "config"), sampleConfig)
	if err := os.MkdirAll(filepath.Join(ws, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "config"), filepath.Join(ws, ".git", "config")); err != nil {
		t.Fatal(err)
	}
	r := ReadRemotes(ws, "github.com")
	if len(r.Remotes) != 0 || !strings.Contains(r.Problem, "symlink") {
		t.Fatalf("a symlinked config was followed: %+v", r)
	}
	ws2 := resolvedTempDir(t)
	if err := os.Symlink(filepath.Join(ws, ".git"), filepath.Join(ws2, ".git")); err != nil {
		t.Fatal(err)
	}
	if r := ReadRemotes(ws2, "github.com"); len(r.Remotes) != 0 || !strings.Contains(r.Problem, "symlink") {
		t.Fatalf("a symlinked .git was followed: %+v", r)
	}
}

// worktree lays out a git worktree the way git does: the main checkout's .git holds
// worktrees/<name>, whose gitdir points back at the worktree's .git file.
func worktree(t *testing.T) (main, wt string) {
	t.Helper()
	main, wt = resolvedTempDir(t), resolvedTempDir(t)
	writeFile(t, filepath.Join(main, ".git", "config"), sampleConfig)
	g := filepath.Join(main, ".git", "worktrees", "feature")
	writeFile(t, filepath.Join(g, "gitdir"), filepath.Join(wt, ".git")+"\n")
	writeFile(t, filepath.Join(g, "commondir"), "../..\n")
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: "+g+"\n")
	return main, wt
}

func TestReadRemotesFollowsAWellShapedWorktree(t *testing.T) {
	main, wt := worktree(t)
	r := ReadRemotes(wt, "github.com")
	if r.Problem != "" || r.GitConfig != filepath.Join(main, ".git", "config") || len(r.Remotes) != 4 {
		t.Fatalf("read %+v", r)
	}
}

// §12 done criterion 9's last sentence: a worktree .git file pointing at another project
// reads as an empty scope and says why.
func TestReadRemotesRefusesAWorktreePointerAtAnotherProject(t *testing.T) {
	_, wt := worktree(t)
	victim := resolvedTempDir(t)
	writeFile(t, filepath.Join(victim, ".git", "config"), `[remote "origin"]
	url = git@github.com:secret/private.git
`)
	// Point the worktree at the victim's .git directory directly: no back-pointer.
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.Join(victim, ".git")+"\n")
	r := ReadRemotes(wt, "github.com")
	if len(r.Remotes) != 0 || !strings.Contains(r.Problem, "wrong shape") {
		t.Fatalf("followed an agent-written pointer: %+v", r)
	}

	// A back-pointer naming another checkout is refused too.
	main, wt2 := worktree(t)
	g := filepath.Join(main, ".git", "worktrees", "feature")
	writeFile(t, filepath.Join(g, "gitdir"), filepath.Join(victim, ".git")+"\n")
	if r := ReadRemotes(wt2, "github.com"); len(r.Remotes) != 0 || !strings.Contains(r.Problem, "back-pointer") {
		t.Fatalf("a foreign back-pointer was accepted: %+v", r)
	}

	// And a gitdir outside the common directory's worktrees/.
	_, wt3 := worktree(t)
	rogue := filepath.Join(resolvedTempDir(t), "rogue")
	writeFile(t, filepath.Join(rogue, "gitdir"), filepath.Join(wt3, ".git")+"\n")
	writeFile(t, filepath.Join(rogue, "commondir"), filepath.Join(victim, ".git")+"\n")
	writeFile(t, filepath.Join(wt3, ".git"), "gitdir: "+rogue+"\n")
	if r := ReadRemotes(wt3, "github.com"); len(r.Remotes) != 0 || !strings.Contains(r.Problem, "worktrees/") {
		t.Fatalf("a gitdir outside worktrees/ was accepted: %+v", r)
	}
}

// A scope entry may hold a whole-segment wildcard; a repository that NAMES one, a remote, and
// every partial glob may not (docs/design/workspace-widening.md, WW-D31).
func TestValidPatternAndAdmits(t *testing.T) {
	for pattern, ok := range map[string]bool{
		"o/r":       true,
		"a.b/c_d-e": true,
		"*/*":       true,
		"o/*":       true,
		"*/r":       true,
		"O/R":       true,
		// Not a pattern: the wildcard is the segment alone, and there are exactly two segments.
		"*":      false,
		"*/*/*":  false,
		"o/r/x":  false,
		"org*/*": false,
		"o/r*":   false,
		"*/r*":   false,
		"o/:":    false,
		"o/..":   false,
		"o/.":    false,
		"/r":     false,
		"o/":     false,
		"o/r ":   false,
		" ":      false,
	} {
		if got := ValidPattern(pattern); got != ok {
			t.Errorf("ValidPattern(%q) = %v, want %v", pattern, got, ok)
		}
	}
	// ValidRepo refuses every wildcard, so a remote can never reduce to one.
	for _, s := range []string{"*/*", "o/*", "*/r", "*"} {
		if ValidRepo(s) {
			t.Errorf("ValidRepo(%q) = true, want false", s)
		}
	}
	for _, tc := range []struct {
		pattern, repo string
		want          bool
	}{
		{"o/r", "o/r", true},
		{"o/r", "O/R", true},
		{"*/*", "o/r", true},
		{"*/*", "anyone/anything-else", true},
		{"o/*", "o/r", true},
		{"o/*", "o/other", true},
		{"o/*", "p/r", false},
		{"*/r", "o/r", true},
		{"*/r", "o/other", false},
		{"o/r", "o/other", false},
		{"o/r", "other/r", false},
		// A malformed side admits nothing, and is admitted by nothing.
		{"o/r", "o/r/x", false},
		{"o/r/x", "o/r", false},
		{"o/*", "*/*", false},
		{"*/*", "/", false},
		{"*/*", "o/bad repo", false},
		{"o/*", "o/bad repo", false},
		{"a b/c", "a b/c", false},
		{"/", "/", false},
	} {
		if got := Admits(tc.pattern, tc.repo); got != tc.want {
			t.Errorf("Admits(%q, %q) = %v, want %v", tc.pattern, tc.repo, got, tc.want)
		}
	}
	for pattern, want := range map[string]string{
		"*/*": "every repository on github.com",
		"o/*": "every repository under o on github.com",
		"*/r": "every repository named r, under any owner on github.com",
		"o/r": "",
		"*":   "",
		// A partial glob is not a pattern, so it gets no note and is no wildcard.
		"org*/*": "",
	} {
		if got := WildcardNote(pattern, "github.com"); got != want {
			t.Errorf("WildcardNote(%q) = %q, want %q", pattern, got, want)
		}
	}
	if got := WildcardNote("*/*", ""); got != "every repository" {
		t.Errorf("WildcardNote with no host = %q", got)
	}
}

func TestRepoFromRemoteURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/o/r":              "o/r",
		"https://github.com/o/r.git":          "o/r",
		"https://token@github.com/o/r.git":    "o/r",
		"ssh://git@github.com/o/r.git":        "o/r",
		"ssh://git@github.com:22/o/r":         "o/r",
		"git@github.com:o/r.git":              "o/r",
		"github.com:o/r":                      "o/r",
		"https://GitHub.com/o/r/":             "o/r",
		"https://github.com/o":                "",
		"https://github.com/o/r/tree/main":    "",
		"https://github.com.evil.example/o/r": "",
		"git://github.com/o/r":                "",
		"http://github.com/o/r":               "",
		"https://github.com/../r":             "",
		"/srv/git/r.git":                      "",
		"file:///srv/r":                       "",
	} {
		if got := RepoFromRemoteURL(raw, "github.com"); got != want {
			t.Errorf("RepoFromRemoteURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// ValidateCurrent admits a repository under an approved wildcard entry, and fails closed on a
// scope file whose entry is malformed (the write-grant gate's read).
func TestValidateCurrentAdmitsUnderAWildcardAndFailsClosedOnAMalformedEntry(t *testing.T) {
	home := resolvedTempDir(t)
	t.Setenv("HOME", home)
	id, err := NewLaunchID()
	if err != nil {
		t.Fatal(err)
	}
	f := File{Source: "github", LaunchID: id, Workspace: "/w", Container: "c", PID: os.Getpid(),
		Repos: []string{"*/*"}}
	path, err := Write(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(path, f, "anything/at-all"); err != nil {
		t.Fatalf("an approved wildcard scope did not admit a repository: %v", err)
	}
	bad := File{Source: "github", LaunchID: "bad", Workspace: "/w", Container: "c", PID: os.Getpid(),
		Repos: []string{"org*/*"}}
	badPath, err := Write(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrent(badPath, bad, "o/r"); err == nil {
		t.Fatal("a malformed scope entry admitted a repository")
	}
}

func TestLiveScopeTransactionsHonorContextWhileSourceLockIsHeld(t *testing.T) {
	home := resolvedTempDir(t)
	t.Setenv("HOME", home)
	id, err := NewLaunchID()
	if err != nil {
		t.Fatal(err)
	}
	file := File{Source: "github", LaunchID: id, Workspace: "/w", Container: "c", PID: os.Getpid(),
		BrokerPID: os.Getpid(), BrokerStart: "start", LiveScope: true, Repos: []string{"o/r"}}
	path, err := Write(file)
	if err != nil {
		t.Fatal(err)
	}

	held, release := make(chan struct{}), make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- brokerlock.WithTransaction(paths.BrokerSourceDir("github"), func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer func() {
		close(release)
		if err := <-lockDone; err != nil {
			t.Errorf("release source transaction: %v", err)
		}
	}()

	for _, tc := range []struct {
		name string
		call func(context.Context) error
	}{
		{"discovery", func(ctx context.Context) error {
			_, err := LiveFilesContext(ctx, "github", "c", "/w")
			return err
		}},
		{"replacement", func(ctx context.Context) error {
			return ReplaceReposContext(ctx, path, file, []string{"o/new"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			started := time.Now()
			err := tc.call(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("call error = %v, want context deadline while the source lock is held", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("context-bounded transaction took %s", elapsed)
			}
		})
	}
}

func TestScopeFileRoundTripAndSweep(t *testing.T) {
	home := resolvedTempDir(t)
	t.Setenv("HOME", home)
	id, err := NewLaunchID()
	if err != nil {
		t.Fatal(err)
	}
	path, err := Write(File{Source: "github", LaunchID: id, PID: os.Getpid(), Repos: []string{"o/r"}})
	if err != nil {
		t.Fatal(err)
	}
	if path != paths.BrokerScopeFile("github", id) {
		t.Fatalf("path %s", path)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("scope file mode %o", fi.Mode().Perm())
	}
	for _, d := range []string{paths.BrokerDir(), filepath.Dir(path)} {
		if fi, _ := os.Stat(d); fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %o", d, fi.Mode().Perm())
		}
	}
	got, err := ReadFile(path)
	if err != nil || !reflect.DeepEqual(got.Repos, []string{"o/r"}) {
		t.Fatalf("read back %+v, %v", got, err)
	}

	// A launch known gone is collected; a live one is not.
	gone := 1 << 30 // no such pid
	deadPath, err := Write(File{Source: "github", LaunchID: "dead", PID: gone})
	if err != nil {
		t.Fatal(err)
	}
	removed := Sweep("github")
	if len(removed) != 1 || removed[0] != deadPath {
		t.Fatalf("swept %v", removed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a live launch's scope file was collected: %v", err)
	}
}

func TestFenceReachesInsideContainsAndSymlinks(t *testing.T) {
	home := resolvedTempDir(t)
	t.Setenv("HOME", home)
	getenv := func(k string) string {
		return map[string]string{"GH_CONFIG_DIR": filepath.Join(home, "ghcfg")}[k]
	}
	fenced := FencedPaths([]string{"$GH_CONFIG_DIR", "$UNSET_VAR/gh", "~/.config/gh"}, home, getenv)
	want := []string{paths.BrokerDir(), filepath.Join(home, "ghcfg"), filepath.Join(home, ".config", "gh")}
	if !reflect.DeepEqual(fenced, want) {
		t.Fatalf("fenced %v, want %v", fenced, want)
	}
	link := filepath.Join(home, "innocent")
	if err := os.MkdirAll(filepath.Join(home, ".config", "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".config"), link); err != nil {
		t.Fatal(err)
	}
	for src, want := range map[string]string{
		paths.GlobalStorage():                  paths.BrokerDir(), // contains the store
		filepath.Join(paths.BrokerDir(), "x"):  paths.BrokerDir(), // inside it
		filepath.Join(home, ".config", "gh"):   filepath.Join(home, ".config", "gh"),
		filepath.Join(link, "gh", "hosts.yml"): filepath.Join(home, ".config", "gh"),
		filepath.Join(home, "code"):            "",
		home:                                   paths.BrokerDir(),
	} {
		if got := Reaches(src, fenced); got != want {
			t.Errorf("Reaches(%s) = %q, want %q", src, got, want)
		}
	}
}

// liveFixture writes a scope file and binds a broker lifetime to it, as a launch and its
// daemon do.
func liveFixture(t *testing.T, launchID, container string, pid int) (string, File) {
	t.Helper()
	f := File{Source: "github", LaunchID: launchID, Workspace: "/w", Container: container, PID: pid,
		Repos: []string{"o/r"}, ConfigFile: "yolo-jail.jsonc", LocalFile: "yolo-jail.local.jsonc"}
	path, err := Write(f)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindBroker(path, f, "start-"+launchID, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return path, bound
}

func TestBindBrokerWritesTheLiveScopeCapability(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	path, _ := liveFixture(t, "one", "c", os.Getpid())
	got, err := ReadFile(path)
	if err != nil || !got.LiveScope || got.BrokerStart != "start-one" || got.BrokerPID != os.Getpid() {
		t.Fatalf("bound file %+v, %v", got, err)
	}
}

func TestReplaceReposKeepsEveryOtherField(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	path, bound := liveFixture(t, "one", "c", os.Getpid())
	if err := ReplaceRepos(path, bound, []string{"o/r", "x/*"}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := bound
	want.Version = FileVersion
	want.Repos = []string{"o/r", "x/*"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rewritten file\n got %+v\nwant %+v", got, want)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("rewritten file mode %v, %v", fi.Mode(), err)
	}
}

func TestReplaceReposRefusesAChangedIdentityABadEntryAndASymlink(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	path, bound := liveFixture(t, "one", "c", os.Getpid())
	for name, mutate := range map[string]func(*File){
		"container": func(f *File) { f.Container = "other" },
		"workspace": func(f *File) { f.Workspace = "/elsewhere" },
		"pid":       func(f *File) { f.PID++ },
	} {
		expect := bound
		mutate(&expect)
		if err := ReplaceRepos(path, expect, []string{"o/new"}); err == nil {
			t.Errorf("%s: a changed identity was rewritten", name)
		}
	}
	if err := ReplaceRepos(path, bound, []string{"org*/*"}); err == nil {
		t.Error("a malformed entry was written")
	}
	if got, _ := ReadFile(path); !reflect.DeepEqual(got.Repos, []string{"o/r"}) {
		t.Fatalf("a refused rewrite changed the file: %v", got.Repos)
	}
	real := path + ".real"
	if err := os.Rename(path, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceRepos(path, bound, []string{"o/new"}); err == nil {
		t.Error("a symlinked scope file was rewritten")
	}
}

func TestLiveFilesSkipsADeadLaunchAndAnotherContainer(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	livePath, _ := liveFixture(t, "live", "c", os.Getpid())
	liveFixture(t, "dead", "c", 1<<30)
	liveFixture(t, "other", "elsewhere", os.Getpid())
	// Written but never bound: no broker serves it.
	if _, err := Write(File{Source: "github", LaunchID: "unbound", Workspace: "/w", Container: "c", PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	got, err := LiveFiles("github", "c", "/w")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != livePath || got[0].File.LaunchID != "live" || !got[0].File.LiveScope {
		t.Fatalf("live files %+v, want only %s", got, livePath)
	}
	if got, err := LiveFiles("github", "c", "/other-workspace"); err != nil || len(got) != 0 {
		t.Fatalf("another workspace: %+v, %v", got, err)
	}
}
