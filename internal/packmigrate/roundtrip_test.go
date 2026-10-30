package packmigrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// shippedPackDirs is every pack directory the repository ships that carries a manifest: a
// pack.toml for every converted pack, and a pack.json for the few that stay on their legacy
// manifest because they hold a JSON null (TestNoShippedPackHasAJSONManifest names them).
func shippedPackDirs(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var dirs []string
	for _, name := range []string{packdecl.ManifestTOMLName, packdecl.ManifestLegacyJSONName} {
		manifests, err := filepath.Glob(filepath.Join("..", "..", "packs", "*", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, manifest := range manifests {
			if dir := filepath.Dir(manifest); !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no shipped packs/*/pack.toml found: the round trip is not reading the tree it exists for")
	}
	return dirs
}

// TestEncodeDecodeRoundTripEveryShippedManifest pins the encoder against the grouped reader on
// every manifest yolo ships, read the way a launch reads it (packload.LoadDir): the encoded
// TOML decodes in strict mode with zero problems and says the same thing on every accessor
// (ManifestDifferences), or the encoder refuses a value TOML cannot say — and the only such
// value is a JSON null, which the readable-config plan §7 records as a representation limit.
func TestEncodeDecodeRoundTripEveryShippedManifest(t *testing.T) {
	roundTripped, refused := 0, 0
	for _, dir := range shippedPackDirs(t) {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			pack, problems := packload.LoadDir(dir, name)
			if len(problems) != 0 || pack == nil {
				t.Fatalf("shipped pack does not load: %v", problems)
			}
			encoded, err := packdecl.EncodeTOML(pack.Decl)
			var unrepresentable *packdecl.UnrepresentableError
			if errors.As(err, &unrepresentable) {
				raw, readErr := os.ReadFile(filepath.Join(dir, packdecl.ManifestLegacyJSONName))
				if readErr != nil {
					t.Fatal(readErr)
				}
				if unrepresentable.Reason != "is null" || !legacyHoldsNull(t, raw) {
					t.Fatalf("encoder refused a non-null value: %v", err)
				}
				refused++
				t.Logf("refused (representation limit): %v", unrepresentable.Path)
				return
			}
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
			roundTripped++
		})
	}
	if roundTripped == 0 || roundTripped < refused {
		t.Fatalf("round-tripped %d shipped manifests and refused %d: the encoder is not converting the tree", roundTripped, refused)
	}
}

func legacyHoldsNull(t *testing.T, raw []byte) bool {
	t.Helper()
	doc, err := configdoc.Parse(raw, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	var walk func(any) bool
	walk = func(value any) bool {
		switch v := value.(type) {
		case nil:
			return true
		case *jsonx.OrderedMap:
			for _, key := range v.Keys() {
				item, _ := v.Get(key)
				if walk(item) {
					return true
				}
			}
		case []any:
			for _, item := range v {
				if walk(item) {
					return true
				}
			}
		}
		return false
	}
	return walk(doc.Values)
}
