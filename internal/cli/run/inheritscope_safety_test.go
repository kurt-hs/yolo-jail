package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestAssemblyAccountsForSupportedLegacyJSONDestinations(t *testing.T) {
	for _, backend := range []string{"podman", "container"} {
		for _, linked := range []bool{false, true} {
			name := backend + "/regular-json"
			if linked {
				name = backend + "/linked-json"
			}
			t.Run(name, func(t *testing.T) {
				_, wsState := inheritHome(t, `{"packs":["claude"]}`)
				configDir := filepath.Join(wsState, ".config")
				if backend == "podman" {
					var subtree string
					for _, bind := range podmanEarlyHomeBinds() {
						if bind.HomeRel == ".config" {
							subtree = bind.Subtree
						}
					}
					if subtree == "" {
						t.Fatal("Podman no longer declares its .config home bind")
					}
					configDir = filepath.Join(wsState, subtree)
				}
				configDir = filepath.Join(configDir, "yolo-jail")
				if err := os.MkdirAll(configDir, 0o755); err != nil {
					t.Fatal(err)
				}
				owner := filepath.Join(configDir, paths.UserConfigLegacyJSONName)
				ownerBytes := []byte(`{"packs":["owner"]}` + "\n")
				linkTarget := filepath.Join(t.TempDir(), "owner.json")
				if linked {
					if err := os.WriteFile(linkTarget, ownerBytes, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(linkTarget, owner); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(owner, ownerBytes, 0o640); err != nil {
					t.Fatal(err)
				}

				o := goldenOptions(t.TempDir(), os.Getenv("HOME"))
				in := relocationInput(t, backend, wsState, nil)
				argv := o.assembleRunCmd(in)
				if backend == "podman" && !linked {
					if in.assemblyErr != nil {
						t.Fatalf("regular legacy JSON should keep the compatible legacy route: %v", in.assemblyErr)
					}
					if len(inheritedScopeMounts(argv)) == 0 {
						t.Fatalf("compatible legacy route emitted no inherited scope: %v", argv)
					}
					if _, err := os.Lstat(filepath.Join(wsState, "inherit-preflight.toml")); !os.IsNotExist(err) {
						t.Fatalf("compatible legacy route left a conflicting TOML stage: %v", err)
					}
					if _, err := os.Lstat(filepath.Join(configDir, paths.UserConfigTOMLName)); !os.IsNotExist(err) {
						t.Fatalf("compatible legacy route copied a TOML sibling into HOME: %v", err)
					}
				} else {
					if in.assemblyErr == nil || !strings.Contains(in.assemblyErr.Error(), owner) {
						t.Fatalf("existing JSON destination was not refused before staging: %v", in.assemblyErr)
					}
					if len(inheritedScopeMounts(argv)) != 0 {
						t.Fatalf("inherited scope was mounted despite refusal: %v", inheritedScopeMounts(argv))
					}
					assertNoInheritedScopeStages(t, wsState)
				}
				for _, spec := range inheritedScopeMounts(argv) {
					if strings.HasSuffix(spec, "/config.toml:ro") {
						t.Fatalf("a conflicting TOML sibling was staged or copied: %s", spec)
					}
				}
				if linked {
					got, err := os.Readlink(owner)
					if err != nil || got != linkTarget {
						t.Fatalf("owner link changed: got=%q err=%v want=%q", got, err, linkTarget)
					}
					if got, err := os.ReadFile(linkTarget); err != nil || string(got) != string(ownerBytes) {
						t.Fatalf("link target changed: got=%q err=%v", got, err)
					}
					if info, err := os.Stat(linkTarget); err != nil || info.Mode().Perm() != 0o600 {
						t.Fatalf("link target mode changed: info=%v err=%v", info, err)
					}
				} else {
					got, err := os.ReadFile(owner)
					if err != nil || string(got) != string(ownerBytes) {
						t.Fatalf("owner bytes changed: got=%q err=%v", got, err)
					}
					info, err := os.Stat(owner)
					if err != nil || info.Mode().Perm() != 0o640 {
						t.Fatalf("owner mode changed: mode=%v err=%v", info, err)
					}
				}
			})
		}
	}
}

func TestAssemblyRefusesInheritanceCoveredByLaterPackFilesHomeBind(t *testing.T) {
	_, wsState := inheritHome(t, `{"packs":["claude"]}`)
	workspace := t.TempDir()
	var configSubtree string
	for _, bind := range podmanEarlyHomeBinds() {
		if bind.HomeRel == ".config" {
			configSubtree = bind.Subtree
		}
	}
	if configSubtree == "" {
		t.Fatal("Podman no longer declares its .config home bind")
	}
	ownerDir := filepath.Join(wsState, configSubtree, "yolo-jail")
	if err := os.MkdirAll(ownerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(ownerDir, "config.jsonc")
	ownerBytes := []byte(`{"resources":{"memory":"8g"}}` + "\n")
	if err := os.WriteFile(owner, ownerBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	o := goldenOptions(workspace, os.Getenv("HOME"))
	pack := jailPack(t, "config-owner", map[string]string{"files/config.jsonc": `{"packs":["owner"]}`},
		packdecl.Contribution{Kind: packdecl.KindFiles, From: "files", Into: ".config/yolo-jail"})
	in := relocationInput(t, "podman", wsState, nil)
	in.packs = append(in.packs, pack)
	argv := o.assembleRunCmd(in)
	if in.assemblyErr == nil || !strings.Contains(in.assemblyErr.Error(), ".config/yolo-jail/config.jsonc") {
		t.Fatalf("assembly failed to account for the later specific home bind: %v", in.assemblyErr)
	}
	if len(inheritedScopeMounts(argv)) != 0 {
		t.Fatalf("inheritance was mounted despite unprovable actual destination mapping: %v", inheritedScopeMounts(argv))
	}
	assertNoInheritedScopeStages(t, wsState)
	gotOwner, err := os.ReadFile(owner)
	if err != nil || string(gotOwner) != string(ownerBytes) {
		t.Fatalf("owner's backing config changed (err=%v): %q", err, gotOwner)
	}
	if info, err := os.Stat(owner); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("owner config mode changed: info=%v err=%v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(ownerDir, paths.UserConfigTOMLName)); !os.IsNotExist(err) {
		t.Fatalf("refusal left a conflicting TOML home file: %v", err)
	}
	packSource := filepath.Join(pack.Root, "files", "config.jsonc")
	packBytes, err := os.ReadFile(packSource)
	if err != nil || string(packBytes) != `{"packs":["owner"]}` {
		t.Fatalf("pack's source file changed (err=%v): %q", err, packBytes)
	}
	foundPackMount := false
	for _, spec := range mountSpecs(argv) {
		if strings.Contains(spec, ":/home/agent/.config/yolo-jail:ro") {
			foundPackMount = true
		}
	}
	if !foundPackMount {
		t.Fatalf("test did not exercise the actual pack-files mount emitter: %v", argv)
	}
}
