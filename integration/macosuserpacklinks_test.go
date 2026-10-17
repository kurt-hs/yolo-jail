package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// macosuserpacklinks_test.go is the macos-user cell for a fetched pack's SYMBOLIC LINKS
// (docs/reference/macos-user-nix-and-features.md, CD-1): the host user copies a pack's content into
// the sandbox home, so a copy that read through a link would hand the sandbox account a host file.
// A fetched pack's link to a file outside its
// own tree refuses the launch before any copy, naming the link; a link inside the tree arrives as
// the file it names. Unit-pinned on Linux through Run's macos-user arm
// (run.TestMacosUserRefusesAFetchedPacksLinkToAHostFile, run.TestMacosUserDeliversAFetchedPacksInTreeLink);
// only a Mac runs the real staging, the `cp -R` into the staged root and the sandbox's read.
//
// HERMETIC: the pack is a local git repository fetched over git+file:// at launch, and the session
// runs bash only.

// macLinkSecret is the body of the host file the escaping link names.
const macLinkSecret = "MAC-HOST-SECRET-7d21"

// macLinkPackSource commits a pack delivering `tools` at ~/.acme-links/tools, holding real.txt,
// alias.txt -> real.txt and, when secret is non-empty, leak -> secret, and returns its git source.
func macLinkPackSource(t *testing.T, secret string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := resolvedTempDir(t)
	for rel, body := range map[string]string{
		"pack.json":      `{"name": "acme-links", "contributes": [{"kind": "files", "from": "tools", "into": ".acme-links/tools"}]}`,
		"tools/real.txt": "real",
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{"tools/alias.txt": "real.txt"}
	if secret != "" {
		links["tools/leak"] = secret
	}
	for rel, target := range links {
		if err := os.Symlink(target, filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-qm", "pack"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())), "GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return "git+file://" + repo + "?ref=main"
}

const macLinkProbe = `d="$HOME/.acme-links/tools"
echo "ALIAS=$(cat "$d/alias.txt" 2>/dev/null)"
if [ -e "$d/leak" ] || [ -L "$d/leak" ]; then echo LEAK_PRESENT; else echo LEAK_ABSENT; fi
echo PROBE_DONE`

// TestMacosUserRefusesAFetchedPacksLinkToAHostFile: the escaping link refuses the launch, named,
// and the secret's bytes reach neither the session's output nor the sandbox.
func TestMacosUserRefusesAFetchedPacksLinkToAHostFile(t *testing.T) {
	requireMacosUser(t)
	secret := filepath.Join(resolvedTempDir(t), "id_ed25519")
	if err := os.WriteFile(secret, []byte(macLinkSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	packHome(t, `{"packs": [{"name": "acme-links", "source": "`+macLinkPackSource(t, secret)+`"}]}`)
	ws := macosUserWorkspace(t, `{}`)

	r := runMacosUser(t, ws, macLinkProbe, withoutFixtureProgramInstall(), withHostSemantics())
	if r.rc == 0 || strings.Contains(r.stdout, "PROBE_DONE") {
		t.Fatalf("a fetched pack linking a host file launched (rc %d):\n%s", r.rc, r.combined())
	}
	if !strings.Contains(r.combined(), "symlink pointing outside the pack") || !strings.Contains(r.combined(), "tools/leak") {
		t.Errorf("the refusal does not name the escaping link tools/leak:\n%s", r.combined())
	}
	if strings.Contains(r.combined(), macLinkSecret) {
		t.Errorf("the host file's bytes appeared in the launch's output:\n%s", r.combined())
	}
}

// TestMacosUserDeliversAFetchedPacksInTreeLink: the same pack without the escaping link launches,
// and the in-tree link reads as the file it names inside the sandbox.
func TestMacosUserDeliversAFetchedPacksInTreeLink(t *testing.T) {
	requireMacosUser(t)
	packHome(t, `{"packs": [{"name": "acme-links", "source": "`+macLinkPackSource(t, "")+`"}]}`)
	ws := macosUserWorkspace(t, `{}`)

	r := runMacosUser(t, ws, macLinkProbe, withoutFixtureProgramInstall(), withHostSemantics())
	if r.rc != 0 || !strings.Contains(r.stdout, "PROBE_DONE") {
		t.Fatalf("the launch did not run its probe (rc %d):\n%s", r.rc, r.combined())
	}
	for _, want := range []string{"ALIAS=real", "LEAK_ABSENT"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("the sandbox did not report %s:\n%s", want, r.stdout)
		}
	}
}
