package packload

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mschulkind-oss/yolo-jail/packs"
)

// embed.FS cannot carry a mode, so packs/executables.txt records the source tree's exec bits
// beside the bytes. It is a FACT ABOUT THE TREE: this fails when a file's on-disk mode and
// the list disagree, in either direction, so `chmod +x` on a shipped pack's script is caught
// at the next `go test` rather than as a script that lands non-executable in every jail.
func TestEmbeddedExecutablesMatchTheTree(t *testing.T) {
	listed, err := embeddedExecutables(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	repoPacks := filepath.Join(findRepoRoot(t), "packs")
	onDisk := map[string]bool{}
	if err := fs.WalkDir(packs.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(p, "/") {
			return err // top-level files (the list itself, embed.go) are no pack's
		}
		fi, serr := os.Stat(filepath.Join(repoPacks, filepath.FromSlash(p)))
		if serr != nil {
			return serr
		}
		if fi.Mode().Perm()&0o111 != 0 {
			onDisk[p] = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for p := range onDisk {
		if !listed[p] {
			t.Errorf("packs/%s is executable on disk but not listed in packs/%s — add the line %q",
				p, EmbeddedExecutablesFile, p)
		}
	}
	for p := range listed {
		if !onDisk[p] {
			t.Errorf("packs/%s lists %s, which is not executable on disk — drop the line or `chmod +x` it",
				EmbeddedExecutablesFile, p)
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("no executable embedded file at all — the shipped packs carry several, so the walk is broken")
	}
}

// The real cache tree every launch and `yolo host apply` read a shipped pack from carries the
// exec bit, sealed read-only: 0o555 for a listed file, 0o444 for the rest.
func TestEmbeddedCacheTreeCarriesTheExecBit(t *testing.T) {
	withEmbeddedFS(t, packs.FS)
	var root string
	for _, p := range Embedded() {
		if p.Name == "muse-planner" {
			root = p.Root
		}
	}
	if root == "" {
		t.Fatalf("muse-planner not embedded: %v", EmbeddedProblems())
	}
	for rel, want := range map[string]os.FileMode{
		"files/scripts/muse-planner-exec.sh": 0o555,
		"files/agents/muse-planner.md":       0o444,
	} {
		fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s sealed at %04o, want %04o", rel, fi.Mode().Perm(), want)
		}
	}
}

func execSampleFS() fstest.MapFS {
	f := sampleFS()
	f["alpha/bin/run.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\n")}
	f[EmbeddedExecutablesFile] = &fstest.MapFile{Data: []byte("# comment\n\nalpha/bin/run.sh\n")}
	return f
}

// A tree on disk that lost the exec bit is not this build's tree: adoption must refuse it and
// repopulate, or a script once stripped would stay stripped for every later process.
func TestAdoptionRefusesATreeWithoutTheExecBit(t *testing.T) {
	base, _ := withEmbeddedFS(t, execSampleFS())
	sum, _ := EmbeddedHash()
	if len(Embedded()) != 2 {
		t.Fatalf("populate failed: %v", EmbeddedProblems())
	}
	script := filepath.Join(base, sum, "alpha", "bin", "run.sh")
	if fi, err := os.Stat(script); err != nil || fi.Mode().Perm() != 0o555 {
		t.Fatalf("populated %s: %v %v, want 0555", script, fi, err)
	}
	ReleaseEmbedded()
	if err := os.Chmod(script, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := verifyEmbeddedTree(execSampleFS(), filepath.Join(base, sum)); err == nil {
		t.Fatal("verify accepted a tree whose listed script lost its exec bit")
	}
	if len(Embedded()) != 2 {
		t.Fatalf("repopulate failed: %v", EmbeddedProblems())
	}
	root, _ := EmbeddedLocation()
	if fi, err := os.Stat(filepath.Join(root, "alpha", "bin", "run.sh")); err != nil || fi.Mode().Perm() != 0o555 {
		t.Fatalf("after re-adopt the script is %v %v, want 0555", fi, err)
	}
}

// A list naming a file the FS does not have stops the populate with a problem that names the
// line, rather than delivering the script it meant without its bit.
func TestAStaleExecutablesListIsAProblem(t *testing.T) {
	f := sampleFS()
	f[EmbeddedExecutablesFile] = &fstest.MapFile{Data: []byte("alpha/bin/gone.sh\n")}
	withEmbeddedFS(t, f)
	if got := Embedded(); len(got) != 0 {
		t.Fatalf("Embedded() = %d packs from a stale list, want none", len(got))
	}
	if probs := strings.Join(EmbeddedProblems(), "\n"); !strings.Contains(probs, "alpha/bin/gone.sh") {
		t.Fatalf("problems %q do not name the stale line", probs)
	}
	if _, probs := MaterializeEmbedded(f, t.TempDir()); len(probs) == 0 {
		t.Fatal("MaterializeEmbedded accepted a stale list")
	}
}
