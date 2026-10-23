package packload

// packtree_test.go pins the pack-set half of a pack-tree program (docs/design/pack-buildable-programs.md
// §6.1, BG-D3): it is listed as a Fork with a Run, never as a fork, and its revision is its source
// directory's digest.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func packTreeContribution() packdecl.Contribution {
	return packdecl.Contribution{
		Kind: packdecl.KindProgram, Bin: "board", Via: packdecl.ViaPack,
		From: "tools/board-cli", Build: "make", Run: "dist/board.js",
	}
}

// TestPackTreeProgramsListsOneAndForksListsNone is the hazard at the pack set: a pack-tree program
// is one Fork value with IsPackTree, no base and the pack's root — and Forks, which the fork pins,
// the fork rewrite and every fork route read, never lists it.
func TestPackTreeProgramsListsOneAndForksListsNone(t *testing.T) {
	p := claimPack(t, "fx", packTreeContribution())
	got := PackTreePrograms([]*Pack{p})
	if len(got) != 1 {
		t.Fatalf("PackTreePrograms = %+v, want one", got)
	}
	f := got[0]
	if !f.IsPackTree() || f.Base != "" || f.Root != p.Root || f.Source != "" {
		t.Errorf("pack-tree Fork = %+v, want IsPackTree, no Base, no Source, Root %s", f, p.Root)
	}
	if f.Key() != "fx/board" {
		t.Errorf("Key = %q, want fx/board", f.Key())
	}
	if f.BuildSource() != "pack:fx/tools/board-cli" {
		t.Errorf("BuildSource = %q, want pack:fx/tools/board-cli", f.BuildSource())
	}
	if f.PackTreeRecipe() != packdecl.PackTreeRecipe("tools/board-cli", "make", "dist/board.js") {
		t.Errorf("PackTreeRecipe = %q, not packdecl's for the declaration", f.PackTreeRecipe())
	}
	if f.SourceDir() != filepath.Join(p.Root, "tools", "board-cli") {
		t.Errorf("SourceDir = %q", f.SourceDir())
	}
	if f.Patched() || f.IsTree() || f.FollowsUpstream() || f.Npm() {
		t.Errorf("a pack-tree program reads as a patched fork, tree or npm source: %+v", f)
	}
	if !strings.Contains(f.Label(), "pack's tree") || strings.HasPrefix(f.Label(), "fork ") {
		t.Errorf("Label = %q, want a pack-tree program's, not a fork's", f.Label())
	}
	if hp := f.HoldPacks(); len(hp) != 1 || hp[0] != "fx" {
		t.Errorf("HoldPacks = %v, want [fx]: it forks no base", hp)
	}
	if forks := Forks([]*Pack{p}); len(forks) != 0 {
		t.Errorf("Forks lists a pack-tree program: %+v — it would take the fork route with no pin", forks)
	}
	// A fork is not a pack-tree program, and reads no pack-tree identity.
	fork := Forks([]*Pack{claimPack(t, "pi-matt", forkContribution("pi", "pi"))})[0]
	if fork.IsPackTree() || fork.BuildSource() != "" || fork.PackTreeRecipe() != "" || fork.SourceDir() != "" {
		t.Errorf("a fork reads as a pack-tree program: %+v", fork)
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPackTreeDigestIsTheSourceDirectorysContent: `.git` is left out, one byte or one exec bit is
// a new revision, an absent directory is "" with no error, and a file in its place is an error.
func TestPackTreeDigestIsTheSourceDirectorysContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src")
	writeTree(t, dir, map[string]string{"VERSION": "1\n", "lib/a.sh": "echo a\n"})
	base, err := PackTreeDigest(dir)
	if err != nil || base == "" {
		t.Fatalf("PackTreeDigest = %q, %v", base, err)
	}

	writeTree(t, dir, map[string]string{".git/HEAD": "ref: refs/heads/main\n"})
	if got, _ := PackTreeDigest(dir); got != base {
		t.Errorf("a .git directory moved the digest: %s → %s", base, got)
	}

	writeTree(t, dir, map[string]string{"VERSION": "2\n"})
	byte1, _ := PackTreeDigest(dir)
	if byte1 == base {
		t.Error("a changed byte did not move the digest")
	}

	if err := os.Chmod(filepath.Join(dir, "lib", "a.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if exec, _ := PackTreeDigest(dir); exec == byte1 {
		t.Error("a changed exec bit did not move the digest")
	}

	if got, err := PackTreeDigest(filepath.Join(t.TempDir(), "absent")); got != "" || err != nil {
		t.Errorf("an absent source directory = %q, %v; want \"\" and no error", got, err)
	}
	file := filepath.Join(t.TempDir(), "file")
	writeTree(t, filepath.Dir(file), map[string]string{"file": "x"})
	if _, err := PackTreeDigest(file); err == nil {
		t.Error("a file in the source directory's place digested without an error")
	}

	// Fork.Digest reads the same directory under the pack's root.
	p := claimPack(t, "fx", packTreeContribution())
	writeTree(t, filepath.Join(p.Root, "tools", "board-cli"), map[string]string{"VERSION": "2\n"})
	f := PackTreePrograms([]*Pack{p})[0]
	want, _ := PackTreeDigest(filepath.Join(p.Root, "tools", "board-cli"))
	if got, err := f.Digest(); err != nil || got != want || got == "" {
		t.Errorf("Fork.Digest = %q, %v; want %q", got, err, want)
	}
}

// TestAPackTreeFootprintNamesItsTreeAndBuild: the claim keeps the program's own name, quotes its
// build line, and is review-worthy only when a build runs or a download is declared.
func TestAPackTreeFootprintNamesItsTreeAndBuild(t *testing.T) {
	built := claimPack(t, "fx", packTreeContribution())
	shipped := packTreeContribution()
	shipped.Build = ""
	plain := claimPack(t, "fy", shipped)
	for _, tc := range []struct {
		p      *Pack
		review bool
		want   string
	}{{built, true, "built by `make`"}, {plain, false, "pack tree: tools/board-cli"}} {
		var found bool
		for _, c := range FootprintOf(tc.p).Claims {
			if c.Kind != packdecl.KindProgram || c.Target != "board" {
				continue
			}
			found = true
			if c.ReviewWorthy != tc.review || !strings.Contains(c.Detail, tc.want) {
				t.Errorf("pack %s claim = %+v, want review %v and %q", tc.p.Name, c, tc.review, tc.want)
			}
		}
		if !found {
			t.Errorf("pack %s has no program claim on board", tc.p.Name)
		}
	}
}
