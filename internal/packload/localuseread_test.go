package packload

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packstage"
)

func TestLoadDirForLocalUseFollowsHomeDirectoryAndFileLinks(t *testing.T) {
	root := t.TempDir()
	dotfiles := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "home", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "home", ".git", "config"), []byte("metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkedHome := filepath.Join(dotfiles, "managed-home")
	if err := os.MkdirAll(filepath.Join(linkedHome, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedHome, ".git", "config"), []byte("linked metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "home", ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(linkedHome, ".config", "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(linkedHome, ".config", "tool", "run")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedHome, filepath.Join(root, "home", "managed")); err != nil {
		t.Fatal(err)
	}
	aliasTarget := filepath.Join(dotfiles, "standalone")
	if err := os.WriteFile(aliasTarget, []byte("linked file"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(aliasTarget, filepath.Join(root, "home", ".config", "direct")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("local fixture\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, problems := LoadDirForLocalUse(root, "fixture")
	if len(problems) != 0 {
		t.Fatalf("LoadDirForLocalUse: %v", problems)
	}
	if p.Local || p.Official {
		t.Fatalf("content reader inferred origin authority: Local=%v Official=%v", p.Local, p.Official)
	}
	if p.Decl.Description != "local fixture" {
		t.Fatalf("README provenance description = %q", p.Decl.Description)
	}
	got := map[string]bool{}
	for _, contribution := range p.Decl.Contributions() {
		got[contribution.From+"=>"+contribution.Into] = true
	}
	for _, want := range []string{
		"home/managed/.config/tool/run=>managed/.config/tool/run",
		"home/.config/direct=>.config/direct",
	} {
		if !got[want] {
			t.Errorf("missing alias-relative contribution %q; got %v", want, got)
		}
	}
	mode, err := os.Stat(filepath.Join(p.Root, "home/managed/.config/tool/run"))
	if err != nil || mode.Mode().Perm()&0o111 == 0 {
		t.Fatalf("linked executable mode not preserved: mode=%v err=%v", mode, err)
	}
	body, err := os.ReadFile(filepath.Join(p.Root, "home/.config/direct"))
	if err != nil || string(body) != "linked file" {
		t.Fatalf("linked file content = %q, err=%v", body, err)
	}

	dest := filepath.Join(t.TempDir(), "staged")
	if _, err := packstage.Stage(packstage.Spec{Root: root, Dest: dest, FollowSymlinks: true}); err != nil {
		t.Fatalf("stage local fixture: %v", err)
	}
	staged, stagedProblems := LoadDirForUse(dest, "fixture")
	if len(stagedProblems) != 0 {
		t.Fatalf("LoadDirForUse(staged local fixture): %v", stagedProblems)
	}
	if got := staged.Decl.Contributions(); !reflect.DeepEqual(got, p.Decl.Contributions()) {
		t.Fatalf("in-place and materialized home contributions differ:\nin-place: %+v\nstaged: %+v", p.Decl.Contributions(), got)
	}
}

func TestLoadDirForLocalUseRefusesDanglingHomeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "home")); err != nil {
		t.Fatal(err)
	}
	_, problems := LoadDirForLocalUse(root, "fixture")
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "home/") || !(strings.Contains(joined, "resolve") || strings.Contains(joined, "does not exist")) {
		t.Fatalf("dangling local home root was treated as absent or diagnosed poorly: %v", problems)
	}
}

func TestLoadDirForLocalUseRefusesUnresolvableAndNonFileHomeLinks(t *testing.T) {
	for _, kind := range []string{"dangling", "cycle", "directory-cycle", "special"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "dangling":
				if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(home, "bad")); err != nil {
					t.Fatal(err)
				}
			case "cycle":
				if err := os.Symlink("loop", filepath.Join(home, "loop")); err != nil {
					t.Fatal(err)
				}
			case "directory-cycle":
				if err := os.Symlink(home, filepath.Join(home, "cycle-dir")); err != nil {
					t.Fatal(err)
				}
			case "special":
				if err := os.Symlink("/dev/null", filepath.Join(home, "special")); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
			}
			_, problems := LoadDirForLocalUse(root, "fixture")
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, "home/") || !(strings.Contains(joined, "symlink") || strings.Contains(joined, "regular file")) {
				t.Fatalf("invalid %s link did not produce an actionable home/ refusal: %v", kind, problems)
			}
		})
	}
}

func TestLoadDirForLocalUseRefusesUnresolvableAndNonregularOverlayLinks(t *testing.T) {
	for _, kind := range []string{"dangling", "cycle", "special"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "overlays")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "bad.jsonc")
			switch kind {
			case "dangling":
				if err := os.Symlink(filepath.Join(root, "missing"), target); err != nil {
					t.Fatal(err)
				}
			case "cycle":
				if err := os.Symlink("bad.jsonc", target); err != nil {
					t.Fatal(err)
				}
			case "special":
				if err := os.Symlink("/dev/null", target); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
			}
			_, problems := LoadDirForLocalUse(root, "fixture")
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, "overlays/bad.jsonc") || !(strings.Contains(joined, "symlink") || strings.Contains(joined, "regular file")) {
				t.Fatalf("invalid %s overlay link did not produce an actionable refusal: %v", kind, problems)
			}
		})
	}
}

func TestOrdinaryReadersAcceptInPackHomeFileAliases(t *testing.T) {
	for _, reader := range []struct {
		name string
		load func(string, string) (*Pack, []string)
	}{
		{name: "authoring", load: LoadDir},
		{name: "use", load: LoadDirForUse},
		{name: "tolerant", load: func(root, name string) (*Pack, []string) {
			restore := OverrideSkewTolerance(true)
			defer restore()
			return LoadDir(root, name)
		}},
	} {
		t.Run(reader.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "home"), 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "home", "actual")
			if err := os.WriteFile(target, []byte("inside"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("actual", filepath.Join(root, "home", "alias")); err != nil {
				t.Fatal(err)
			}
			p, problems := reader.load(root, "fixture")
			if len(problems) != 0 {
				t.Fatalf("reader refused in-pack file alias: %v", problems)
			}
			found := false
			for _, contribution := range p.Decl.Contributions() {
				if contribution.From == "home/alias" && contribution.Into == "alias" {
					found = true
				}
			}
			if !found {
				t.Fatalf("in-pack alias was not emitted under its alias-relative name: %+v", p.Decl.Contributions())
			}
		})
	}
}

func TestOrdinaryReadersRefuseInvalidHomeFileLinks(t *testing.T) {
	for _, kind := range []string{"dangling", "cycle", "directory", "special", "escaping"} {
		for _, reader := range []struct {
			name string
			load func(string, string) (*Pack, []string)
		}{
			{name: "authoring", load: LoadDir},
			{name: "use", load: LoadDirForUse},
			{name: "tolerant", load: func(root, name string) (*Pack, []string) {
				restore := OverrideSkewTolerance(true)
				defer restore()
				return LoadDir(root, name)
			}},
		} {
			t.Run(kind+"/"+reader.name, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				if err := os.MkdirAll(home, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(home, "bad")
				switch kind {
				case "dangling":
					if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
						t.Fatal(err)
					}
				case "cycle":
					if err := os.Symlink("bad", link); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(filepath.Join(root, "target"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Join(root, "target"), link); err != nil {
						t.Fatal(err)
					}
				case "special":
					if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
						t.Skipf("FIFO fixture unavailable: %v", err)
					}
					if err := os.Symlink(filepath.Join(root, "fifo"), link); err != nil {
						t.Fatal(err)
					}
				case "escaping":
					if err := os.Symlink(outside, link); err != nil {
						t.Fatal(err)
					}
				}
				p, problems := reader.load(root, "fixture")
				if p == nil || !strings.Contains(strings.Join(problems, "\n"), "home/bad") {
					t.Fatalf("reader accepted invalid %s link: pack=%v problems=%v", kind, p, problems)
				}
				for _, contribution := range p.Decl.Contributions() {
					if contribution.From == "home/bad" {
						t.Fatalf("invalid target was emitted: %+v", contribution)
					}
				}
			})
		}
	}
}

func TestOrdinaryReadersDoNotFollowLinkedHomeDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "secret"), []byte("not delivered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "home", "outside")); err != nil {
		t.Fatal(err)
	}
	for _, load := range []struct {
		name string
		fn   func(string, string) (*Pack, []string)
	}{{"authoring", LoadDir}, {"use", LoadDirForUse}} {
		t.Run(load.name, func(t *testing.T) {
			p, problems := load.fn(root, "fetched")
			if p == nil || !strings.Contains(strings.Join(problems, "\n"), "home/") {
				t.Fatalf("ordinary reader followed a home directory link: pack=%v problems=%v", p, problems)
			}
			for _, contribution := range p.Decl.Contributions() {
				if strings.Contains(contribution.From, "secret") {
					t.Fatalf("ordinary reader discovered outside file: %+v", contribution)
				}
			}
		})
	}
}
