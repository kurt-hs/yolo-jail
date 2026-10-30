package packmigrate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// shippedPackDirs is every pack directory the repository ships: each carries a pack.toml
// (TestNoShippedPackHasAJSONManifest pins that no shipped pack keeps a legacy manifest).
func shippedPackDirs(t *testing.T) []string {
	t.Helper()
	manifests, err := filepath.Glob(filepath.Join("..", "..", "packs", "*", packdecl.ManifestTOMLName))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Fatal("no shipped packs/*/pack.toml found: the round trip is not reading the tree it exists for")
	}
	dirs := make([]string, 0, len(manifests))
	for _, manifest := range manifests {
		dirs = append(dirs, filepath.Dir(manifest))
	}
	return dirs
}

// TestEncodeDecodeRoundTripEveryShippedManifest pins the encoder against the grouped reader on
// every manifest yolo ships, read the way a launch reads it (packload.LoadDir): the encoded
// TOML decodes in strict mode with zero problems and says the same thing on every accessor
// (ManifestDifferences). Every shipped manifest is a pack.toml, so the encoder may refuse none
// of them: whatever a shipped pack says, TOML said first.
func TestEncodeDecodeRoundTripEveryShippedManifest(t *testing.T) {
	for _, dir := range shippedPackDirs(t) {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			pack, problems := packload.LoadDir(dir, name)
			if len(problems) != 0 || pack == nil {
				t.Fatalf("shipped pack does not load: %v", problems)
			}
			encoded, err := packdecl.EncodeTOML(pack.Decl)
			if err != nil {
				t.Fatalf("EncodeTOML: %v", err)
			}
			decoded, problems := packdecl.DecodeTOML(encoded)
			if len(problems) != 0 {
				t.Fatalf("strict DecodeTOML of the encoded manifest: %v\n%s", problems, encoded)
			}
			if diffs := ManifestDifferences(pack.Decl, decoded); len(diffs) != 0 {
				t.Fatalf("round trip changed the manifest's meaning:\n  %s\n%s", strings.Join(diffs, "\n  "), encoded)
			}
			again, err := packdecl.EncodeTOML(decoded)
			if err != nil {
				t.Fatalf("re-encoding the decoded grouped manifest: %v", err)
			}
			redecoded, problems := packdecl.DecodeTOML(again)
			if len(problems) != 0 {
				t.Fatalf("strict DecodeTOML of the re-encoded manifest: %v\n%s", problems, again)
			}
			if diffs := ManifestDifferences(decoded, redecoded); len(diffs) != 0 {
				t.Fatalf("grouped round trip changed the manifest's meaning:\n  %s", strings.Join(diffs, "\n  "))
			}
		})
	}
}
