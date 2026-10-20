package capture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyMaterializedDirectoryBindings(t *testing.T) {
	for _, binding := range []string{".local", ".local/bin", ".local/share/vendor/1.0"} {
		t.Run(binding, func(t *testing.T) {
			for _, mutation := range []string{"none", "stale target", "target mode", "file replaced by link", "captured link replaced by file", "wrong captured link", "directory replaced by file", "dangling directory", "cyclic directory", "extra output"} {
				t.Run(mutation, func(t *testing.T) {
					home := t.TempDir()
					_, entry := entryFixture(t, home, Platform(), nil)
					bound := filepath.Join(t.TempDir(), "bound")
					if err := os.MkdirAll(bound, 0o700); err != nil {
						t.Fatal(err)
					}
					bindingPath := filepath.Join(home, binding)
					if err := os.MkdirAll(filepath.Dir(bindingPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(bound, bindingPath); err != nil {
						t.Fatal(err)
					}
					if _, err := Materialize(MaterializeOptions{Entry: entry, Home: home}); err != nil {
						t.Fatal(err)
					}
					// Verify both ancestor-only closure and selected directory inventories.
					outputs := []string{".local/bin/vendor"}
					if binding != ".local" {
						outputs = append(outputs, binding)
					}
					if err := VerifyMaterialized(entry, home, ".local/bin/vendor", outputs); err != nil {
						t.Fatalf("valid binding rejected: %v", err)
					}
					outputs = append(outputs, ".local")
					if err := VerifyMaterialized(entry, home, ".local/bin/vendor", outputs); err != nil {
						t.Fatalf("nested directory binding rejected: %v", err)
					}
					info, err := os.Stat(bound)
					if err != nil || info.Mode().Perm() != 0o700 {
						t.Fatalf("existing directory mode changed: %v, %v", info, err)
					}
					target := filepath.Join(home, ".local/share/vendor/1.0/vendor")
					link := filepath.Join(home, ".local/bin/vendor")
					replaceFile := func(path, body string, mode os.FileMode) {
						t.Helper()
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte(body), mode); err != nil {
							t.Fatal(err)
						}
					}
					switch mutation {
					case "none":
						return
					case "stale target":
						replaceFile(target, "stale bytes", 0o755)
					case "target mode":
						replaceFile(target, "#!/bin/sh\necho vendor\n", 0o644)
					case "file replaced by link":
						external := filepath.Join(t.TempDir(), "same-bytes")
						if err := os.WriteFile(external, []byte("#!/bin/sh\necho vendor\n"), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.Remove(target); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(external, target); err != nil {
							t.Fatal(err)
						}
					case "captured link replaced by file":
						replaceFile(link, "#!/bin/sh\necho vendor\n", 0o755)
					case "wrong captured link":
						if err := os.Remove(link); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("wrong-target", link); err != nil {
							t.Fatal(err)
						}
					case "directory replaced by file", "dangling directory", "cyclic directory":
						if err := os.Remove(bindingPath); err != nil {
							t.Fatal(err)
						}
						switch mutation {
						case "directory replaced by file":
							if err := os.WriteFile(bindingPath, []byte("not a directory"), 0o644); err != nil {
								t.Fatal(err)
							}
						case "dangling directory":
							if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), bindingPath); err != nil {
								t.Fatal(err)
							}
						case "cyclic directory":
							if err := os.Symlink(filepath.Base(bindingPath), bindingPath); err != nil {
								t.Fatal(err)
							}
						}
					case "extra output":
						if err := os.WriteFile(filepath.Join(bindingPath, "extra"), []byte("extra"), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					if err := VerifyMaterialized(entry, home, ".local/bin/vendor", outputs); err == nil {
						t.Fatalf("accepted %s through directory binding %s", mutation, binding)
					}
				})
			}
		})
	}
}

func TestAdmittedTargetClosureDoesNotTraverseExternalOrCyclicTargets(t *testing.T) {
	entries := map[string]ManifestEntry{
		"bin":          {Path: "bin", Kind: KindDir},
		"bin/external": {Path: "bin/external", Kind: KindSymlink, Target: "/external/program"},
		"bin/cycle":    {Path: "bin/cycle", Kind: KindSymlink, Target: "cycle2"},
		"bin/cycle2":   {Path: "bin/cycle2", Kind: KindSymlink, Target: "cycle"},
		"bin/escape":   {Path: "bin/escape", Kind: KindSymlink, Target: "../../outside"},
	}
	selected := map[string]bool{"bin/external": true, "bin/cycle": true, "bin/escape": true}
	closure, dirs := admittedTargetClosure(entries, selected, "/captured/home")
	if !samePaths(closure, selected) || len(dirs) != 0 {
		t.Fatalf("followed inadmissible targets: closure=%v dirs=%v", closure, dirs)
	}
}
