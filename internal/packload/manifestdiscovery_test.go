package packload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func TestLoadDirUsesREADMEFirstLineForManifestlessPack(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("Portable shell helpers\n\nDetails.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "home", ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, problems := LoadDir(root, "shell-tools")
	if len(problems) != 0 || p == nil {
		t.Fatalf("manifest-less pack load: pack=%#v problems=%v", p, problems)
	}
	if p.Decl.Description != "Portable shell helpers" {
		t.Fatalf("description = %q, want README first line", p.Decl.Description)
	}
	if p.Decl.Origin.Format != 0 || len(p.Decl.Contributions()) != 0 {
		t.Fatalf("conventional content selected or acquired a manifest: %#v", p.Decl)
	}
}

func TestLoadDirFallsBackPastDanglingLegacyJSONCSymlink(t *testing.T) {
	root := t.TempDir()
	jsonc := filepath.Join(root, packdecl.ManifestLegacyJSONCName)
	if err := os.Symlink(filepath.Join(root, "missing.jsonc"), jsonc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestLegacyJSONName), []byte(`{"description":"json fallback"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, problems := LoadDir(root, "legacy")
	if len(problems) != 0 || p == nil || p.Decl.Description != "json fallback" {
		t.Fatalf("legacy fallback changed: pack=%#v problems=%v", p, problems)
	}
}

func TestLoadDirRefusesTOMLWithDanglingLegacySibling(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte("description = \"new\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, packdecl.ManifestLegacyJSONCName)
	if err := os.Symlink(filepath.Join(root, "missing.jsonc"), legacy); err != nil {
		t.Fatal(err)
	}
	p, problems := LoadDir(root, "ambiguous")
	joined := strings.Join(problems, "\n")
	if p != nil || !strings.Contains(joined, packdecl.ManifestTOMLName) || !strings.Contains(joined, packdecl.ManifestLegacyJSONCName) || !strings.Contains(joined, "remove") {
		t.Fatalf("dangling legacy sibling did not trigger actionable dual-format refusal: pack=%#v problems=%v", p, problems)
	}
}

func TestLoadDirReadsLegacyManifestWhenItIsTheOnlySibling(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestLegacyJSONName), []byte(`{"description":"legacy"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, problems := LoadDir(root, "legacy")
	if len(problems) != 0 || p == nil || p.Decl.Origin.Format != packdecl.ManifestFormatJSON {
		t.Fatalf("legacy reader changed: pack=%#v problems=%v", p, problems)
	}
}

func TestLoadDirDiscoversGroupedPackTOML(t *testing.T) {
	root := t.TempDir()
	body := "description = \"A TOML pack\"\n\n[[program]]\nbin = \"ripgrep\"\nvia = \"npm\"\npackage = \"ripgrep\"\n"
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, problems := LoadDir(root, "ripgrep-pack")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	if p.Decl.Origin.Format != packdecl.ManifestFormatTOML || p.Decl.Description != "A TOML pack" {
		t.Fatalf("manifest origin/description = %#v / %q", p.Decl.Origin, p.Decl.Description)
	}
	if got := p.Decl.InstallContributions(); len(got) != 1 || got[0].Bin != "ripgrep" {
		t.Fatalf("program contributions = %#v", got)
	}
}

func TestLoadDirRefusesLegacyAndTOMLManifestSiblings(t *testing.T) {
	for _, legacy := range []string{packdecl.ManifestLegacyJSONCName, packdecl.ManifestLegacyJSONName} {
		t.Run(legacy, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{packdecl.ManifestTOMLName: "description = \"new\"\n", legacy: `{"description":"old"}`} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			p, problems := LoadDir(root, "ambiguous")
			if p != nil || len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), packdecl.ManifestTOMLName) || !strings.Contains(strings.Join(problems, "\n"), legacy) || !strings.Contains(strings.Join(problems, "\n"), "remove") {
				t.Fatalf("LoadDir accepted or gave no actionable collision: pack=%#v problems=%v", p, problems)
			}
		})
	}
}

func TestLoadDirTOMLUsesStrictUseAndJailReaders(t *testing.T) {
	root := t.TempDir()
	// `future_field` was added after some supported jail readers. The use and jail
	// readers skip it with a note; authoring remains strict.
	body := "[[program]]\nbin = \"acme\"\nvia = \"npm\"\npackage = \"acme\"\nfuture_field = true\n"
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, problems := LoadDir(root, "acme"); p == nil || len(problems) == 0 {
		t.Fatalf("strict authoring read should reject unknown field: pack=%#v problems=%v", p, problems)
	}
	if p, problems := LoadDirForUse(root, "acme"); p == nil || len(problems) != 0 || len(p.SkewNotes) == 0 {
		t.Fatalf("host use read should retain and disclose supported fields: pack=%#v problems=%v", p, problems)
	}
	restore := OverrideSkewTolerance(true)
	defer restore()
	if p, problems := LoadDir(root, "acme"); p == nil || len(problems) != 0 || len(p.SkewNotes) == 0 {
		t.Fatalf("jail tolerant read should retain and disclose supported fields: pack=%#v problems=%v", p, problems)
	}
}
