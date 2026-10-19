package entrypoint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/testutil"
)

// admitForkLauncherCapture creates the real admitted artifact a source launcher expects: the
// executable output plus its build receipt. It lets launcher tests exercise the production
// capture-materialize command rather than teaching a fake yolo to claim the build is present.
func admitForkLauncherCapture(t *testing.T, home, bin, output, installed string) (storeDir, key string) {
	t.Helper()
	if !strings.HasPrefix(output, ".local/") || filepath.IsAbs(output) || filepath.Clean(output) != output {
		t.Fatalf("fixture output %q must be a clean path under .local", output)
	}
	store := &capture.Store{Dir: t.TempDir()}
	staged, err := store.Stage("fork-launcher")
	if err != nil {
		t.Fatal(err)
	}
	tree := capture.TreeDir(staged)
	path := filepath.Join(tree, filepath.FromSlash(output))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	entries := []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}}
	parts := strings.Split(filepath.ToSlash(filepath.Dir(output)), "/")
	current := ".local"
	for _, part := range parts[1:] {
		current += "/" + part
		entries = append(entries, capture.ManifestEntry{Path: current, Kind: capture.KindDir, Mode: "0755"})
	}
	entries = append(entries, capture.ManifestEntry{
		Path: filepath.ToSlash(output), Kind: capture.KindFile,
		Mode: fmt.Sprintf("%04o", info.Mode().Perm()), Size: int64(len(body)),
	})
	if err := capture.WriteManifest(staged, &capture.Manifest{
		Schema: capture.ManifestSchema, Home: home, Platform: capture.Platform(),
		Surfaces: []string{".local"}, Excluded: []string{}, Entries: entries,
		AbsoluteRefs: []capture.AbsoluteRef{},
	}); err != nil {
		t.Fatal(err)
	}
	entry, err := store.AdmitEntry(staged)
	if err != nil {
		t.Fatal(err)
	}
	line := BuildReceipt{
		Bin: bin, Source: "git+https://example.invalid/fork.git", Key: entry.Key,
		Path: entry.Root, Platform: capture.Platform(), Revision: "abc123", Recipe: "fixture",
		Act: ReceiptActRecord, Time: time.Now(),
	}.Line()
	if err := AppendReceiptLine(capture.ReceiptsPath(entry.Root), line); err != nil {
		t.Fatal(err)
	}
	return store.Dir, entry.Key
}

// forkMaterializerPath returns the production yolo binary used to materialize fork fixtures.
func forkMaterializerPath(t *testing.T) string {
	t.Helper()
	return testutil.YoloBinaryDir(t)
}
