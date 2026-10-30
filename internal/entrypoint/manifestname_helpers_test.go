package entrypoint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// shippedCopyManifest copies the manifest of the pack rooted at srcRoot into destDir under
// the name it has there: pack.toml for a grouped manifest, pack.jsonc or pack.json for a
// legacy one. A
// pack holding both is refused by the loader, so the first one found is the one it reads.
func shippedCopyManifest(t *testing.T, srcRoot, destDir string) {
	t.Helper()
	for _, name := range []string{packdecl.ManifestTOMLName, packdecl.ManifestLegacyJSONCName, packdecl.ManifestLegacyJSONName} {
		body, err := os.ReadFile(filepath.Join(srcRoot, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destDir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("no pack manifest (%s, %s or %s) in %s", packdecl.ManifestTOMLName, packdecl.ManifestLegacyJSONCName, packdecl.ManifestLegacyJSONName, srcRoot)
}
