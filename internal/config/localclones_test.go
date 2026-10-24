package config

// localclones_test.go pins the gathering half of the LOCAL CLONE MAP (localclones.go, PF-D100): a path the user
// config reads a pack from registers the clone it lands in, `local_clones` registers a clone explicitly, a
// workspace may not name clones, and an entry that registers nothing is a fatal config error.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

func lcGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

func lcCommit(t *testing.T, dir, file, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lcGit(t, dir, "add", "-A")
	lcGit(t, dir, "commit", "-qm", file)
	return lcGit(t, dir, "rev-parse", "HEAD")
}

// lcFixture is an upstream, a bare published copy of it holding branch mine, and "machine", the user's clone,
// whose remote is written as remoteURL (rewritten to the bare copy by the clone's own insteadOf) and whose
// local mine is one commit ahead of the published one. It returns the root, the clone and the local tip.
func lcFixture(t *testing.T, remoteURL string) (root, upstream, machine, localTip string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	upstream, machine = filepath.Join(root, "upstream"), filepath.Join(root, "machine")
	published := filepath.Join(root, "published.git")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	lcGit(t, upstream, "init", "-q", "-b", "main")
	lcCommit(t, upstream, "f.txt", "base\n")
	lcGit(t, root, "clone", "-q", "--bare", upstream, published)
	lcGit(t, root, "clone", "-q", published, machine)
	lcGit(t, machine, "checkout", "-q", "-b", "mine")
	lcCommit(t, machine, "carry.txt", "pushed\n")
	lcGit(t, machine, "push", "-q", "origin", "mine")
	localTip = lcCommit(t, machine, "carry.txt", "unpushed\n")
	lcGit(t, machine, "checkout", "-q", "main")
	lcGit(t, machine, "remote", "set-url", "origin", remoteURL)
	lcGit(t, machine, "config", "url."+published+".insteadOf", remoteURL)
	return root, upstream, machine, localTip
}

func lcUserConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	write(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), body)
	return home
}

func lcRefs(t *testing.T, repo string) string {
	t.Helper()
	refs := lcGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	st, err := os.Stat(filepath.Join(repo, ".git", "FETCH_HEAD"))
	mt := "none"
	if err == nil {
		mt = st.ModTime().String()
	}
	return refs + "\nFETCH_HEAD " + mt
}

// A pack fetched by URL names its patch branch by the published URL; the user named a DIFFERENT pack by a path
// inside their clone of that repository. The clone's newer local branch builds, its refs and FETCH_HEAD are
// untouched, and the provenance line names the clone and the pack whose path registered it.
func TestAPathPackRegistersItsCloneForAnotherPacksPublishedURL(t *testing.T) {
	_, upstream, machine, localTip := lcFixture(t, "https://example.com/o/r.git")
	packDir := filepath.Join(machine, "packs", "other")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lcUserConfig(t, `{"packs": [{"source": "`+packDir+`", "name": "other"}]}`)
	before := lcRefs(t, machine)

	store := &packsrc.Store{Dir: t.TempDir()}
	want := packsrc.PatchedWant{Owner: "fetched/tool", Source: "git+file://" + upstream + "?ref=main",
		PatchBranch: "git+ssh://git@example.com/o/r?ref=mine"} // the scp/ssh spelling still matches the https remote
	now := time.Unix(1_800_000_000, 0)
	res := store.CheckPatched(want, packsrc.CheckOptions{RefreshPatchBranch: true, Now: func() time.Time { return now }})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	pb := res.Record.PatchBranch
	if pb == nil {
		t.Fatalf("no resolution; check %+v", res.Record.Check)
	}
	if pb.Commit != localTip || pb.Copy != "local" || pb.Checkout != machine {
		t.Fatalf("built %s from %q in %q, want the clone's local branch %s", pb.Commit, pb.Copy, pb.Checkout, localTip)
	}
	line := pb.Line(now)
	for _, part := range []string{"1 unpushed", "local clone " + machine, "the path of pack other"} {
		if !strings.Contains(line, part) {
			t.Errorf("the provenance line %q lacks %q", line, part)
		}
	}
	if after := lcRefs(t, machine); after != before {
		t.Errorf("the check wrote to the user's clone:\nbefore %s\nafter  %s", before, after)
	}
}

// A plain-files pack in no repository registers nothing, and is no error.
func TestAPlainFilesPackRegistersNothing(t *testing.T) {
	plain := t.TempDir()
	lcUserConfig(t, `{"packs": [{"source": "`+plain+`", "name": "plain"}]}`)
	if got := LocalClones(); len(got) != 0 {
		t.Fatalf("a plain-files pack registered %+v", got)
	}
	errs, _ := ValidateConfig(decode(t, `{}`), t.TempDir(), nil)
	for _, e := range errs {
		if strings.Contains(e, "local_clones") {
			t.Errorf("a plain-files pack produced %q", e)
		}
	}
}

// An explicit entry registers a clone holding no pack, before any implicit one, with `~` expanded.
func TestLocalClonesEntryRegistersAClone(t *testing.T) {
	root, _, machine, _ := lcFixture(t, "git@example.com:o/r.git")
	home := lcUserConfig(t, `{"local_clones": ["~/m"]}`)
	if err := os.Symlink(machine, filepath.Join(home, "m")); err != nil {
		t.Fatal(err)
	}
	_ = root
	got := LocalClones()
	if len(got) != 1 || !got[0].Explicit || got[0].From != "~/m" {
		t.Fatalf("LocalClones = %+v, want the one explicit entry", got)
	}
	if len(got[0].Remotes) != 1 || got[0].Remotes[0].URL != "git@example.com:o/r.git" {
		t.Errorf("remotes = %+v, want origin as the configuration writes it", got[0].Remotes)
	}
}

func TestValidateConfigRefusesWorkspaceLocalClones(t *testing.T) {
	ws := t.TempDir()
	lcUserConfig(t, `{}`)
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"local_clones": ["/srv/x"]}`)
	errs, _ := ValidateConfig(decode(t, `{"local_clones": ["/srv/x"]}`), ws, nil)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"local_clones: user-scope only", "must not point yolo at host paths", "config.jsonc"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors = %v, want one containing %q", errs, want)
		}
	}
}

// An entry that does not exist, is in no git clone, or whose clone has no remote is a FATAL error naming the
// entry and the next step.
func TestValidateConfigRefusesALocalClonesEntryThatRegistersNothing(t *testing.T) {
	notGit := t.TempDir()
	noRemote := t.TempDir()
	lcGit(t, noRemote, "init", "-q")
	missing := filepath.Join(t.TempDir(), "gone")
	body := `{"local_clones": ["` + missing + `", "` + notGit + `", "` + noRemote + `"]}`
	lcUserConfig(t, body)
	errs, _ := ValidateConfig(decode(t, body), t.TempDir(), nil)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{missing + "' does not exist", notGit + "' is not inside a git clone",
		"which has no remote", "fix the path, clone the repository there, or remove the entry from"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors = %v, want one containing %q", errs, want)
		}
	}
	if _, known := knownTopLevelConfigKeys["local_clones"]; !known {
		t.Error("local_clones is not in knownTopLevelConfigKeys")
	}
}
