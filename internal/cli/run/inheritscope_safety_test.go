package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
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

func TestAssemblyRefusesNestedJSONDestinationThatStrictUserReaderCannotMerge(t *testing.T) {
	for _, rt := range []string{"podman", "container"} {
		t.Run(rt, func(t *testing.T) {
			_, wsState := inheritHome(t, `{"packages":["strace"]}`)
			configDir := filepath.Join(wsState, ".config")
			if rt == "podman" {
				for _, bind := range podmanEarlyHomeBinds() {
					if bind.HomeRel == ".config" {
						configDir = filepath.Join(wsState, bind.Subtree)
					}
				}
			}
			nestedDir := filepath.Join(configDir, "yolo-jail")
			if err := os.MkdirAll(nestedDir, 0o755); err != nil {
				t.Fatal(err)
			}
			owner := filepath.Join(nestedDir, "inherited-launch.json")
			ownerBytes := []byte(`{"packages":["legacy-owner"]}` + "\n")
			if err := os.WriteFile(owner, ownerBytes, 0o640); err != nil {
				t.Fatal(err)
			}

			o := goldenOptions(t.TempDir(), os.Getenv("HOME"))
			effective, _ := o.effectiveConfigForInherit()
			nestedValues, _, err := config.FilterInheritErr(effective, config.InheritNested)
			if err != nil || nestedValues.Len() == 0 {
				t.Fatalf("fixture does not provide nonempty nested inheritance: values=%#v err=%v", nestedValues, err)
			}
			in := relocationInput(t, rt, wsState, nil)
			argv := o.assembleRunCmd(in)
			var generatedBody string
			var nestedMounts []string
			for _, spec := range mountSpecs(argv) {
				if strings.Contains(spec, ":/home/agent/.config/yolo-jail/inherited-launch.") {
					nestedMounts = append(nestedMounts, spec)
					if strings.HasSuffix(spec, "/inherited-launch.jsonc:ro") {
						body, err := os.ReadFile(strings.TrimSuffix(spec, ":/home/agent/.config/yolo-jail/inherited-launch.jsonc:ro"))
						if err != nil {
							t.Fatal(err)
						}
						generatedBody = string(body)
					}
				}
			}

			jailHome := t.TempDir()
			jailDir := filepath.Join(jailHome, ".config", "yolo-jail")
			if err := os.MkdirAll(jailDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(jailDir, paths.UserConfigLegacyJSONCName), []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(jailDir, "inherited-launch.json"), ownerBytes, 0o640); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", jailHome)
			t.Setenv("YOLO_VERSION", "9.9.9-test")
			t.Setenv(config.UserLayerEnv, "")
			if rt == "podman" && in.assemblyErr == nil {
				if generatedBody == "" || len(nestedMounts) == 0 {
					t.Fatalf("Podman assembly did not produce the nested JSONC sibling: mounts=%v argv=%v", nestedMounts, argv)
				}
				if err := os.WriteFile(filepath.Join(jailDir, "inherited-launch.jsonc"), []byte(generatedBody), 0o644); err != nil {
					t.Fatal(err)
				}
				if _, err := config.LoadReadableUserScopeConfig(true, func(string) {}); err == nil || !strings.Contains(err.Error(), "inherited-launch") {
					t.Fatalf("the actual assembled User graph unexpectedly passed its strict reader: %v", err)
				}
				t.Fatalf("assembly reported success although the public strict User reader rejects its generated nested JSONC beside %s", owner)
			}
			if rt == "podman" {
				if in.assemblyErr == nil || !strings.Contains(in.assemblyErr.Error(), owner) {
					t.Fatalf("assembly did not refuse the owner JSON before staging: %v", in.assemblyErr)
				}
				if len(nestedMounts) != 0 {
					t.Fatalf("refusal still emitted nested inherited mounts: %v", nestedMounts)
				}
				assertNoInheritedScopeStages(t, wsState)
				for _, name := range []string{"inherited-launch.jsonc", "inherited-launch.toml"} {
					if _, err := os.Lstat(filepath.Join(nestedDir, name)); !os.IsNotExist(err) {
						t.Fatalf("refusal left generated nested destination %s: %v", name, err)
					}
				}
			} else if in.assemblyErr != nil || len(nestedMounts) != 0 {
				t.Fatalf("non-nesting backend unexpectedly delivered or refused a nested file: err=%v mounts=%v", in.assemblyErr, nestedMounts)
			}

			gotOwner, err := os.ReadFile(owner)
			if err != nil || string(gotOwner) != string(ownerBytes) {
				t.Fatalf("owner JSON bytes changed: got=%q err=%v", gotOwner, err)
			}
			if info, err := os.Stat(owner); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("owner JSON mode changed: info=%v err=%v", info, err)
			}
			if _, err := config.LoadReadableUserScopeConfig(true, func(string) {}); err != nil {
				t.Fatalf("the preserved single JSON owner file must remain readable by the public User reader: %v", err)
			}
		})
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
