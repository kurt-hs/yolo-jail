package entrypoint

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateGraphicsDriversUsesProfileMetadataAndSplitArchitectures(t *testing.T) {
	tmp := t.TempDir()
	graphicsTestDirs(t, tmp, "mesa64")
	lib64 := filepath.Join(tmp, "separate-lib-output", "lib")
	share64 := filepath.Join(tmp, "separate-share-output", "share")
	for _, path := range []string{lib64, share64} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lib32, share32 := graphicsTestDirs(t, tmp, "mesa32")
	profile := filepath.Join(tmp, "profile")
	writeGraphicsTestManifest(t, filepath.Join(profile, graphicsProfileManifestRel), graphicsManifest{Drivers: []graphicsDriver{
		{Name: "mesa", Version: "26.2.5", Arch: "64", Lib: lib64, Share: share64},
		{Name: "mesa", Version: "26.2.5", Arch: "32", Lib: lib32, Share: share32},
		{Name: "mesa", Version: "other", Arch: "64", Lib: lib64, Share: share64},
	}})
	baked := filepath.Join(tmp, "baked.json")
	writeGraphicsTestManifest(t, baked, graphicsManifest{Drivers: []graphicsDriver{
		{Name: "mesa", Version: "stale-baked", Arch: "64", Lib: lib64, Share: share64},
	}})
	paths := graphicsTestPaths(tmp)
	e := NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1", StoreProfilesEnv: profile})
	if err := generateGraphicsDriversIn(e, paths, baked); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{filepath.Join(paths.Root64, "lib"), lib64},
		{filepath.Join(paths.Root64, "share"), share64},
		{filepath.Join(paths.Root32, "lib"), lib32},
		{filepath.Join(paths.Root32, "share"), share32},
	} {
		if got, err := os.Readlink(pair[0]); err != nil || got != pair[1] {
			t.Errorf("symlink %s = %q, %v; want %q", pair[0], got, err, pair[1])
		}
	}
	got := readGraphicsTestManifest(t, paths.RuntimeManifest)
	if len(got.Drivers) != 2 || got.Drivers[0].Version != "26.2.5" || got.Drivers[0].Preserved == nil || *got.Drivers[0].Preserved {
		t.Fatalf("runtime provenance = %#v, want first selected per-arch profile records with preserved=false", got)
	}
}

func TestGenerateGraphicsDriversPreservesIncompleteAndDanglingTrees(t *testing.T) {
	for _, kind := range []string{"incomplete directory", "dangling symlink", "plain file"} {
		t.Run(kind, func(t *testing.T) {
			tmp := t.TempDir()
			lib, share := graphicsTestDirs(t, tmp, "mesa")
			paths := graphicsTestPaths(tmp)
			if err := os.MkdirAll(filepath.Dir(paths.Root64), 0o755); err != nil {
				t.Fatal(err)
			}
			var marker string
			switch kind {
			case "incomplete directory":
				if err := os.Mkdir(paths.Root64, 0o755); err != nil {
					t.Fatal(err)
				}
				marker = filepath.Join(paths.Root64, "user-marker")
				if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "dangling symlink":
				marker = filepath.Join(tmp, "absent")
				if err := os.Symlink(marker, paths.Root64); err != nil {
					t.Fatal(err)
				}
			case "plain file":
				marker = paths.Root64
				if err := os.WriteFile(marker, []byte("user bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			baked := filepath.Join(tmp, "baked.json")
			writeGraphicsTestManifest(t, baked, graphicsManifest{Drivers: []graphicsDriver{{Name: "mesa", Version: "26.2.5", Arch: "64", Lib: lib, Share: share}}})
			if err := generateGraphicsDriversIn(NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1"}), paths, baked); err != nil {
				t.Fatal(err)
			}
			if kind == "incomplete directory" {
				if b, err := os.ReadFile(marker); err != nil || string(b) != "keep" {
					t.Fatalf("existing user content changed: %q %v", b, err)
				}
			} else if kind == "dangling symlink" {
				if got, err := os.Readlink(paths.Root64); err != nil || got != marker {
					t.Fatalf("dangling user symlink changed: %q %v", got, err)
				}
			} else if b, err := os.ReadFile(paths.Root64); err != nil || string(b) != "user bytes" {
				t.Fatalf("existing user file changed: %q %v", b, err)
			}
			got := readGraphicsTestManifest(t, paths.RuntimeManifest)
			if len(got.Drivers) != 1 || got.Drivers[0].Preserved == nil || !*got.Drivers[0].Preserved {
				t.Fatalf("existing tree not marked preserved: %#v", got)
			}
		})
	}
}

func TestGenerateGraphicsDriversDisabledOrWithoutDriverIsNoop(t *testing.T) {
	tmp := t.TempDir()
	paths := graphicsTestPaths(tmp)
	baked := filepath.Join(tmp, "baked.json")
	writeGraphicsTestManifest(t, baked, graphicsManifest{Drivers: []graphicsDriver{}})
	for _, env := range []map[string]string{
		{GraphicsGPUEnabledEnv: "0"},
		{GraphicsGPUEnabledEnv: "1"},
	} {
		if err := generateGraphicsDriversIn(NewEnv(env), paths, baked); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{paths.Root64, paths.Root32, paths.RuntimeManifest} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("no-op wrote %s: %v", path, err)
			}
		}
	}
}

func TestGenerateGraphicsDriversStoreModeNeverFallsBackToBaked(t *testing.T) {
	tmp := t.TempDir()
	lib, share := graphicsTestDirs(t, tmp, "mesa")
	baked := filepath.Join(tmp, "baked.json")
	writeGraphicsTestManifest(t, baked, graphicsManifest{Drivers: []graphicsDriver{{Name: "mesa", Version: "baked", Arch: "64", Lib: lib, Share: share}}})
	paths := graphicsTestPaths(tmp)
	e := NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1", StoreProfilesEnv: filepath.Join(tmp, "missing-profile")})
	err := generateGraphicsDriversIn(e, paths, baked)
	if err == nil || !strings.Contains(err.Error(), "store-delivered packages") {
		t.Fatalf("missing store manifest error = %v, want profile rebuild guidance", err)
	}
	if _, err := os.Lstat(paths.Root64); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale baked tree used in store mode: %v", err)
	}
}

func TestGenerateGraphicsDriversFHSOnlyStoreModeDoesNotSelectImageDrivers(t *testing.T) {
	tmp := t.TempDir()
	lib, share := graphicsTestDirs(t, tmp, "mesa")
	baked := filepath.Join(tmp, "baked.json")
	writeGraphicsTestManifest(t, baked, graphicsManifest{Drivers: []graphicsDriver{{Name: "mesa", Version: "baked", Arch: "64", Lib: lib, Share: share}}})
	paths := graphicsTestPaths(tmp)
	e := NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1", StoreFHSProfilesEnv: filepath.Join(tmp, "chromium-profile")})
	if err := generateGraphicsDriversIn(e, paths, baked); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Root64, paths.Root32, paths.RuntimeManifest} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("FHS-only store profile selected a stale or dependency driver at %s: %v", path, err)
		}
	}
}

func TestGenerateGraphicsDriversRollsBackBothArchitecturesOnFailure(t *testing.T) {
	tmp := t.TempDir()
	lib64, share64 := graphicsTestDirs(t, tmp, "mesa64")
	lib32, share32 := graphicsTestDirs(t, tmp, "mesa32")
	paths := graphicsTestPaths(tmp)
	baked := filepath.Join(tmp, "baked.json")
	writeGraphicsTestManifest(t, baked, graphicsManifest{Drivers: []graphicsDriver{
		{Name: "mesa", Version: "26.2.5", Arch: "64", Lib: lib64, Share: share64},
		{Name: "mesa", Version: "26.2.5", Arch: "32", Lib: lib32, Share: share32},
	}})
	// Provenance publication failure occurs after both roots are populated.
	blocker := filepath.Join(tmp, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths.RuntimeManifest = filepath.Join(blocker, "graphics-drivers.json")
	err := generateGraphicsDriversIn(NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1"}), paths, baked)
	if err == nil {
		t.Fatal("expected runtime provenance publication failure")
	}
	if !strings.Contains(err.Error(), paths.RuntimeManifest) || !graphicsErrorHasRemedy(err.Error()) {
		t.Fatalf("provenance error lacks path/actionable remedy: %v", err)
	}
	for _, root := range []string{paths.Root64, paths.Root32} {
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("failed two-architecture attempt left new tree %s: %v", root, err)
		}
	}
	if b, err := os.ReadFile(blocker); err != nil || string(b) != "keep" {
		t.Fatalf("rollback changed unrelated user data: %q %v", b, err)
	}
}

func TestEnsureGraphicsTreesRollsBackPartialPublication(t *testing.T) {
	tmp := t.TempDir()
	lib64, share64 := graphicsTestDirs(t, tmp, "mesa64")
	lib32, share32 := graphicsTestDirs(t, tmp, "mesa32")
	paths := graphicsTestPaths(tmp)
	targets := []graphicsTreeTarget{
		{root: paths.Root64, driver: graphicsDriver{Name: "mesa", Version: "v", Arch: "64", Lib: lib64, Share: share64}},
		{root: paths.Root32, driver: graphicsDriver{Name: "mesa", Version: "v", Arch: "32", Lib: lib32, Share: share32}},
	}
	injected := errors.New("injected publication failure")
	renamed := 0
	_, _, err := ensureGraphicsTreesWithRename(targets, func(old, new string) error {
		if new == filepath.Join(paths.Root32, "share") {
			return injected
		}
		renamed++
		return os.Rename(old, new)
	})
	if err == nil || renamed == 0 {
		t.Fatalf("partial publish result err=%v renamed=%d", err, renamed)
	}
	if !strings.Contains(err.Error(), paths.Root32) || !errors.Is(err, injected) || !graphicsErrorHasRemedy(err.Error()) {
		t.Fatalf("publication error lacks path, cause or operational next step: %v", err)
	}
	for _, root := range []string{paths.Root64, paths.Root32} {
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("partial publish rollback left %s: %v", root, err)
		}
	}
}

func TestGraphicsDriverOperationalFailuresNamePathCauseAndRepair(t *testing.T) {
	tmp := t.TempDir()
	lib64, share64 := graphicsTestDirs(t, tmp, "mesa64")
	lib32, share32 := graphicsTestDirs(t, tmp, "mesa32")
	paths := graphicsTestPaths(tmp)
	targets := []graphicsTreeTarget{
		{root: paths.Root64, driver: graphicsDriver{Name: "mesa", Version: "v", Arch: "64", Lib: lib64, Share: share64}},
		{root: paths.Root32, driver: graphicsDriver{Name: "mesa", Version: "v", Arch: "32", Lib: lib32, Share: share32}},
	}
	injected := errors.New("injected filesystem failure")

	t.Run("inspection", func(t *testing.T) {
		ops := systemGraphicsFSOps()
		ops.lstat = func(path string) (os.FileInfo, error) {
			if path == paths.Root32 {
				return nil, injected
			}
			return os.Lstat(path)
		}
		_, _, err := ensureGraphicsTreesWithOps(targets, ops)
		if err == nil || !strings.Contains(err.Error(), paths.Root32) || !errors.Is(err, injected) || !graphicsErrorHasRemedy(err.Error()) {
			t.Fatalf("inspection error lacks path, cause or actionable remedy: %v", err)
		}
		if _, err := os.Lstat(paths.Root64); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspection failure left the earlier staged root: %v", err)
		}
	})
	t.Run("staging", func(t *testing.T) {
		ops := systemGraphicsFSOps()
		attempts := 0
		ops.mkdirTemp = func(dir, pattern string) (string, error) {
			attempts++
			if attempts == 2 {
				return "", injected
			}
			return os.MkdirTemp(dir, pattern)
		}
		_, _, err := ensureGraphicsTreesWithOps(targets, ops)
		if err == nil || !strings.Contains(err.Error(), paths.Root32) || !errors.Is(err, injected) || !graphicsErrorHasRemedy(err.Error()) {
			t.Fatalf("staging error lacks path, cause or actionable remedy: %v", err)
		}
		for _, root := range []string{paths.Root64, paths.Root32} {
			if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("staging failure created root %s: %v", root, err)
			}
		}
	})
	t.Run("symlink staging", func(t *testing.T) {
		ops := systemGraphicsFSOps()
		ops.symlink = func(_, _ string) error { return injected }
		_, _, err := ensureGraphicsTreesWithOps(targets[:1], ops)
		if err == nil || !strings.Contains(err.Error(), paths.Root64) || !errors.Is(err, injected) || !graphicsErrorHasRemedy(err.Error()) {
			t.Fatalf("symlink staging error lacks path, cause or actionable remedy: %v", err)
		}
		if _, err := os.Lstat(paths.Root64); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlink staging failure created root: %v", err)
		}
	})
	t.Run("claim root", func(t *testing.T) {
		ops := systemGraphicsFSOps()
		ops.mkdir = func(path string, mode os.FileMode) error {
			if path == paths.Root32 {
				return injected
			}
			return os.Mkdir(path, mode)
		}
		_, _, err := ensureGraphicsTreesWithOps(targets, ops)
		if err == nil || !strings.Contains(err.Error(), paths.Root32) || !errors.Is(err, injected) || !graphicsErrorHasRemedy(err.Error()) {
			t.Fatalf("root creation error lacks path, cause or actionable remedy: %v", err)
		}
		for _, root := range []string{paths.Root64, paths.Root32} {
			if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("root creation rollback left %s: %v", root, err)
			}
		}
	})
}

func graphicsErrorHasRemedy(message string) bool {
	return strings.Contains(message, "writable") && strings.Contains(message, "relaunch")
}

func TestGenerateGraphicsDriversRejectsMalformedAndUnmaterializedMetadata(t *testing.T) {

	tmp := t.TempDir()
	paths := graphicsTestPaths(tmp)
	baked := filepath.Join(tmp, "baked.json")
	for _, data := range []string{
		`{"drivers":[{"name":"mesa","version":"v","arch":"64","lib":"/missing/lib","share":"/missing/share"}],"unknown":true}`,
		`{"drivers":[{"name":"mesa","version":"v","arch":"64","lib":"/missing/lib","share":"/missing/share"}]}`,
	} {
		if err := os.WriteFile(baked, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		err := generateGraphicsDriversIn(NewEnv(map[string]string{GraphicsGPUEnabledEnv: "1"}), paths, baked)
		if err == nil || !strings.Contains(err.Error(), "graphics driver metadata") {
			t.Fatalf("invalid manifest error = %v, want actionable graphics metadata error", err)
		}
	}
}

func graphicsTestPaths(root string) graphicsPaths {
	return graphicsPaths{
		Root64:          filepath.Join(root, "run", "opengl-driver"),
		Root32:          filepath.Join(root, "run", "opengl-driver-32"),
		RuntimeManifest: filepath.Join(root, "run", "yolo", "graphics-drivers.json"),
	}
}

func graphicsTestDirs(t *testing.T, root, name string) (string, string) {
	t.Helper()
	lib, share := filepath.Join(root, name, "lib"), filepath.Join(root, name, "share")
	for _, path := range []string{lib, share} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return lib, share
}

func writeGraphicsTestManifest(t *testing.T, path string, manifest graphicsManifest) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readGraphicsTestManifest(t *testing.T, path string) graphicsManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest graphicsManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}
