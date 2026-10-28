package packload

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packstage"
)

func TestManifestlessHomeFilesSkipGitMetadataLikeStaging(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"home/.git/config", "home/.config/tool/settings", "home/.config/tool/.git/config"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inPlace, problems := LoadDir(root, "fixture")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	dest := filepath.Join(t.TempDir(), "staged")
	if _, err := packstage.Stage(packstage.Spec{Root: root, Dest: dest}); err != nil {
		t.Fatal(err)
	}
	staged, stagedProblems := LoadDirForUse(dest, "fixture")
	if len(stagedProblems) != 0 {
		t.Fatalf("LoadDirForUse(staged): %v", stagedProblems)
	}
	if got, want := inPlace.Decl.Contributions(), staged.Decl.Contributions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("in-place and materialized home contributions differ:\nin-place: %+v\nstaged: %+v", got, want)
	}
	for _, contribution := range inPlace.Decl.Contributions() {
		if strings.Contains(contribution.From, ".git") {
			t.Fatalf("VCS metadata became a home contribution: %+v", contribution)
		}
	}
}

func TestManifestlessHomeFilesUseManifestPathValidationForEveryReader(t *testing.T) {
	for _, tc := range []struct {
		name, destination string
		want              []string
	}{
		{name: "PATH destination", destination: ".local/bin/run", want: []string{"jail's PATH"}},
		{name: "colon in source and destination", destination: ".config/tool/bad:name", want: []string{"contributes[0].from", "contributes[0].into", `must not contain ":"`}},
	} {
		for _, reader := range []struct {
			name string
			load func(string, string) (*Pack, []string)
		}{
			{name: "LoadDir", load: LoadDir},
			{name: "LoadDirForUse", load: LoadDirForUse},
			{name: "LoadDirForLocalUse", load: LoadDirForLocalUse},
			{name: "tolerant LoadDir", load: func(root, name string) (*Pack, []string) {
				restore := OverrideSkewTolerance(true)
				defer restore()
				return LoadDir(root, name)
			}},
		} {
			t.Run(tc.name+"/"+reader.name, func(t *testing.T) {
				root := t.TempDir()
				file := filepath.Join(root, "home", filepath.FromSlash(tc.destination))
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
					t.Fatal(err)
				}
				_, problems := reader.load(root, "fixture")
				joined := strings.Join(problems, "\n")
				for _, want := range tc.want {
					if !strings.Contains(joined, want) {
						t.Fatalf("reader accepted or poorly diagnosed home destination %q (missing %q): %v", tc.destination, want, problems)
					}
				}
			})
		}
	}
}

func TestManifestlessHomeFilesResolveIndividuallyAndCollideByDestination(t *testing.T) {
	makePack := func(name string, files map[string]string) *Pack {
		t.Helper()
		root := t.TempDir()
		for rel, body := range files {
			path := filepath.Join(root, "home", filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		p, problems := LoadDir(root, name)
		if len(problems) != 0 {
			t.Fatalf("LoadDir(%s): %v", name, problems)
		}
		return p
	}
	a := makePack("alpha", map[string]string{".config/tool/a": "a", ".config/tool/run": "#!/bin/sh\n"})
	b := makePack("beta", map[string]string{".config/tool/b": "b"})
	if got := a.Decl.Contributions(); len(got) != 2 {
		t.Fatalf("home files became %d contributions, want one per file: %+v", len(got), got)
	}
	if got := Collisions([]*Pack{a, b}); len(got) != 0 {
		t.Fatalf("disjoint home children collided: %+v", got)
	}
	var executableFrom string
	for _, contribution := range a.Decl.Contributions() {
		if contribution.Into == ".config/tool/run" {
			executableFrom = contribution.From
		}
	}
	if executableFrom != "home/.config/tool/run" {
		t.Fatalf("executable source = %q", executableFrom)
	}
	mode, err := os.Stat(filepath.Join(a.Root, filepath.FromSlash(executableFrom)))
	if err != nil || mode.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable mode was not preserved at source: mode=%v err=%v", mode, err)
	}

	c := makePack("charlie", map[string]string{".config/tool/a": "c"})
	collisions := Collisions([]*Pack{a, c})
	if len(collisions) != 1 || collisions[0].Kind != packdecl.KindFiles || collisions[0].Target != ".config/tool/a" || strings.Join(collisions[0].Packs, ",") != "alpha,charlie" {
		t.Fatalf("same-file collision did not name both packs: %+v", collisions)
	}
}
