package check

// packmigrateadvice.go is the Packs section's one advice line for a local pack still on its legacy
// manifest (docs/design/pack-manifests.md §5): this release reads pack.json and pack.jsonc, so the
// pack passes, and the migration is the author's act, taken when they choose. The line names the
// verb that takes it.

import (
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// packMigrateAdvice is the advice for entry e, or "" when there is none: e is a local pack (not
// shipped, not fetched) whose directory holds a legacy pack.json or pack.jsonc and no pack.toml.
// A source not visible from here (a jail reading a host path) gives none: there is nothing here
// to migrate.
func packMigrateAdvice(e config.PackEntry) string {
	if e.Embedded() || !e.IsLocal() {
		return ""
	}
	addr, err := packsrc.Parse(e.Source)
	if err != nil || addr.Path == "" {
		return ""
	}
	if regularFile(filepath.Join(addr.Path, packdecl.ManifestTOMLName)) {
		return ""
	}
	for _, legacy := range []string{packdecl.ManifestLegacyJSONName, packdecl.ManifestLegacyJSONCName} {
		if regularFile(filepath.Join(addr.Path, legacy)) {
			return "its manifest is the old " + legacy + "; `yolo pack migrate " + shquote.Quote(addr.Path) +
				"` converts it to " + packdecl.ManifestTOMLName + " (it still loads as it is)"
		}
	}
	return ""
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
