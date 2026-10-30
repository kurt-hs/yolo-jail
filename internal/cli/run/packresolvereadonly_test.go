package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// packresolvereadonly_test.go pins that the LAZY pack resolver (resolveConfiguredPacks, behind
// loopholes.PackModules and PackSupersessions) WRITES NOTHING into the pack store. It runs
// behind read-only surfaces — `yolo check`, `yolo loopholes list`, config validation — and
// docs/reference/pack-system.md ("What never fetches") rules that those check nothing out: a
// checkout of the partial mirror can fetch. Measured before the fix: `yolo check` checked out
// a commit the mirror held but had not checked out, through this resolver, and the Packs
// section's own read-only test passed only while a process-lifetime memo kept the resolver
// from running under that test's HOME.

// runGit runs git in dir with a clean environment and a fixed identity, returning stdout.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())), "GIT_AUTHOR_NAME=t",
		"GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// treesOf lists the pack store's checkouts of commit sha (a subdirectory pack's tree is
// trees/<sha>-<key>, so a prefix match is the only honest one).
func treesOf(t *testing.T, sha string) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(paths.PacksDir(), "trees"))
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), sha) {
			out = append(out, e.Name())
		}
	}
	return out
}

func configuredPackNamed(name string) bool {
	for _, p := range resolveConfiguredPacks() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// A COMMIT THE MIRROR HOLDS BUT HAS NOT CHECKED OUT stays un-checked-out under the lazy
// resolver, and the pack contributes nothing until the launch's refresh checks it out. The
// control half is the launch's order — refresh, then resolve — and pins that the launch path
// still sees the pack, so the read-only posture costs a launch nothing.
func TestResolveConfiguredPacksChecksNothingOut(t *testing.T) {
	home := hostRefreshEnv(t)
	repo := refreshGitRepo(t)

	// The store holds main, fetched and checked out by an ordinary refresh.
	writeUserPacks(t, home, `[{"name": "gp", "source": "git+file://`+repo+`?ref=main"}]`)
	RefreshConfiguredPacks(func(string) {}, func(string) {})
	if !configuredPackNamed("gp") {
		t.Fatal("fixture: the checked-out main must resolve, or the half below measures nothing")
	}

	// v2 reaches the mirror without a checkout, as a fetch by another pack of the same
	// repository leaves it.
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "v2")
	runGit(t, repo, "tag", "v2")
	v2 := runGit(t, repo, "rev-parse", "v2^{commit}")
	mirrors, err := os.ReadDir(filepath.Join(paths.PacksDir(), "mirrors"))
	if err != nil || len(mirrors) != 1 {
		t.Fatalf("fixture: want one mirror: %v %v", mirrors, err)
	}
	runGit(t, filepath.Join(paths.PacksDir(), "mirrors", mirrors[0].Name()),
		"fetch", "-q", "origin", "+refs/tags/*:refs/tags/*")
	writeUserPacks(t, home, `[{"name": "gp", "source": "git+file://`+repo+`?ref=v2"}]`)

	if configuredPackNamed("gp") {
		t.Error("the lazy resolver read a commit the store has not checked out")
	}
	if got := treesOf(t, v2); len(got) != 0 {
		t.Fatalf("the lazy resolver behind `yolo check` checked v2 out into the pack store: %v", got)
	}

	// The launch's order: its refresh checks the commit out, then validation resolves it.
	RefreshConfiguredPacks(func(string) {}, func(string) {})
	if len(treesOf(t, v2)) == 0 {
		t.Fatal("fixture: the launch's refresh did not check v2 out")
	}
	if !configuredPackNamed("gp") {
		t.Error("after the launch's refresh the lazy resolver must see the pack, as validation on the launch path does")
	}
}
