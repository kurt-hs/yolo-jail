package packload_test

import (
	"os"
	"path/filepath"
)

// shippedManifestPath is the manifest a materialized shipped pack carries at root: pack.toml
// once the pack is converted, pack.json while it stays on its legacy manifest.
func shippedManifestPath(root string) string {
	toml := filepath.Join(root, "pack.toml")
	if _, err := os.Stat(toml); err == nil {
		return toml
	}
	return filepath.Join(root, "pack.json")
}
