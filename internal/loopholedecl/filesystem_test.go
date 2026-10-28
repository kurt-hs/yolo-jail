package loopholedecl

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadDirTolerantFSMatchesPathReader(t *testing.T) {
	const jsonc = `{"name":"module","version":1,"future_field":"ignored"}`
	const jsonBody = `{"name":"other","version":1}`
	root := t.TempDir()
	module := filepath.Join(root, "module")
	if err := os.Mkdir(module, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, ManifestName), []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "manifest.json"), []byte(jsonBody), 0o644); err != nil {
		t.Fatal(err)
	}

	pathManifest, pathSkipped, pathErr := LoadDirTolerant(module)
	fsManifest, fsSkipped, fsErr := LoadDirTolerantFS(os.DirFS(root), "module", module)
	if pathErr != nil || fsErr != nil {
		t.Fatalf("path err=%v, fs err=%v", pathErr, fsErr)
	}
	if !reflect.DeepEqual(fsManifest, pathManifest) || !reflect.DeepEqual(fsSkipped, pathSkipped) {
		t.Fatalf("FS decode differs: manifest=%#v skipped=%v; path manifest=%#v skipped=%v", fsManifest, fsSkipped, pathManifest, pathSkipped)
	}
	if fsManifest.Name != "module" {
		t.Fatalf("selected name = %q, want JSONC-first module", fsManifest.Name)
	}
}

func TestLoadDirTolerantFSUsesJSONFallbackAndReportsMissingPath(t *testing.T) {
	const body = `{"name":"module","version":1}`
	root := t.TempDir()
	module := filepath.Join(root, "module")
	if err := os.Mkdir(module, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "manifest.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadDirTolerantFS(os.DirFS(root), "module", module)
	if err != nil || got.Name != "module" {
		t.Fatalf("JSON fallback: manifest=%#v err=%v", got, err)
	}
	missingDir := filepath.Join(root, "missing")
	_, _, err = LoadDirTolerantFS(os.DirFS(root), "missing", missingDir)
	if err == nil || err.Error() != filepath.Join(missingDir, "manifest.jsonc")+" not found" {
		t.Fatalf("missing error = %v", err)
	}
}
