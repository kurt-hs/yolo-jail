package packsrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE RELATIVE LOCATOR's grammar (relative.go): a path starting `.`, with a mandatory ?ref= whose `..` is
// still refused, and no //subdirectory; a host name with no scheme is never read as a directory.
func TestParseFromReadsTheRelativeLocatorsGrammar(t *testing.T) {
	a, err := ParseFrom("../..?ref=carry/pi-subagents", "")
	if err != nil || a.Kind != KindGit || a.Ref != "carry/pi-subagents" || a.Repo != "" {
		t.Fatalf("ParseFrom of a relative locator with no directory = %+v, %v", a, err)
	}
	for raw, want := range map[string]string{
		"../..":                   "missing required ?ref=",
		"../..?ref=a..b":          "must not contain '..'",
		"../..//sub?ref=main":     "no //subdirectory",
		"github.com/you/pi?ref=x": "relative to the pack",
		"file:///srv/pi?ref=mine": "git+file:///srv/pi?ref=mine",
	} {
		if _, err := ParseFrom(raw, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseFrom(%q) = %v, want an error containing %q", raw, err, want)
		}
	}
	if _, err := Parse("../..?ref=main"); err == nil {
		t.Error("Parse, which reads `packs` entries, accepted a relative locator")
	}
}

// THE BOUND holds through a link: a path inside the pack's repository that is a link to a repository above
// it is refused, naming the link and the bound; the repository's own root resolves.
func TestParseFromBoundsALinkOutOfThePacksRepository(t *testing.T) {
	root := t.TempDir()
	repo, other := filepath.Join(root, "repo"), filepath.Join(root, "other")
	pack := filepath.Join(repo, "packs", "p")
	for _, d := range []string{filepath.Join(repo, ".git"), filepath.Join(other, ".git"), pack} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(other, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	realRepo, _ := filepath.EvalSymlinks(repo)
	a, err := ParseFrom("../..?ref=mine", pack)
	if err != nil || a.Repo != "file://"+filepath.ToSlash(realRepo) || a.Ref != "mine" || a.Raw != "../..?ref=mine" {
		t.Fatalf("ParseFrom of the pack's own repository = %+v, %v", a, err)
	}
	_, err = ParseFrom("../../link?ref=mine", pack)
	if err == nil || !strings.Contains(err.Error(), "a link to") || !strings.Contains(err.Error(), realRepo) {
		t.Errorf("ParseFrom of a link out of the repository = %v, want a refusal naming the link and the bound", err)
	}
}

func TestRelativeRepositoryIdentityMapsBindPathAliasesAndSeparatesClones(t *testing.T) {
	makeRepo := func() (string, string) {
		t.Helper()
		root := t.TempDir()
		pack := filepath.Join(root, "repo", "packs", "p")
		if err := os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(pack, 0o755); err != nil {
			t.Fatal(err)
		}
		return root, pack
	}
	rootA, packA := makeRepo()
	_, packB := makeRepo()
	alias := filepath.Join(t.TempDir(), "mapped-pack")
	if err := os.Symlink(packA, alias); err != nil {
		t.Fatal(err)
	}
	idA, err := RelativeRepositoryIdentity("../..?ref=mine", packA)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := RelativeRepositoryIdentity("../..?ref=mine", alias)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := RelativeRepositoryIdentity("../..?ref=mine", packB)
	if err != nil {
		t.Fatal(err)
	}
	if idA != mapped {
		t.Errorf("same declaring repository at %s and mapped path %s has identities %q / %q", rootA, alias, idA, mapped)
	}
	if idA == idB {
		t.Errorf("independent declaring repositories share identity %q", idA)
	}
}

// A CHECK HANDED A RELATIVE PATCH BRANCH WITH NO PACK DIRECTORY refuses before any fetch, rather than fetching
// a repository named "".
func TestACheckOfARelativePatchBranchWithNoDirectoryRefuses(t *testing.T) {
	w := PatchedWant{Owner: "p/b", Source: "git+https://example.invalid/up?ref=main", PatchBranch: "../..?ref=mine"}
	if _, _, _, err := w.Inputs(); err == nil || !strings.Contains(err.Error(), "a yolo bug") {
		t.Errorf("Inputs of a relative patch branch with no directory = %v, want a refusal", err)
	}
}
