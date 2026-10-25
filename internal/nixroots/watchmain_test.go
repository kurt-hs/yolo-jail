package nixroots

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/logcap"
)

// WatchMain writes through a writer that bounds the log the boot named, so a watcher that
// runs for the jail's life cannot grow nix-roots.log past the cap. Its first line goes through
// it here: the log is over the cap before WatchMain runs, and after it the log is bounded and
// archived. MUTATION: write to os.Stderr directly in WatchMain and this fails.
func TestWatchMainBoundsTheLogTheBootNamed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nix-roots.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("renewed a link\n"), logcap.MaxBytes/15+2), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	orig := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = orig })
	t.Setenv(LogEnv, path)
	if rc := WatchMain([]string{"unexpected"}); rc != 2 {
		t.Fatalf("WatchMain = %d, want the usage exit 2", rc)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > logcap.MaxBytes {
		t.Fatalf("nix-roots.log is %d bytes after the watcher wrote, past the cap %d", st.Size(), logcap.MaxBytes)
	}
	if _, err := os.Stat(path + logcap.ArchiveSuffix); err != nil {
		t.Errorf("no archived generation: %v", err)
	}
}
