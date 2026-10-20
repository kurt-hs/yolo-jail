package run

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// This lays only synthetic machine-tier mirrors under TempDir, not the actual native home.
// Run's delivery is independently pinned by claudesecurestorage_test.go. These fake files do
// not establish vendor lock use, Keychain precedence, Seatbelt enforcement or VM coherence.
func TestNativeSyntheticWorkspaceMirrorsReachOneCredentialAndLockNamespace(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packs := claudePackFixture(t)
	o := goldenOptions("/ws", home)
	o.Getenv = viewEnv("")
	store := o.claudeSecureStorageDir("macos-user", jsonx.NewOrderedMap(), packs, home)
	if store != filepath.Join(home, claudeSharedDirRel) {
		t.Fatalf("synthetic selector = %q", store)
	}
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{claudeview.ViewFile, ".oauth_refresh.lock", claudeview.StorageLockDir} {
		p := filepath.Join(store, name)
		if name == claudeview.ViewFile {
			err = os.WriteFile(p, []byte(`{"claudeAiOauth":{"accessToken":"fake-access","refreshToken":"fake-refresh"}}`), 0o600)
		} else {
			err = os.Mkdir(p, 0o755)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		sidecar := t.TempDir()
		layout := entrypoint.DeriveDarwinHomeLayout(home, sidecar,
			packload.WritableDirs(packs), packload.SharedDirs(packs))
		if !slices.Contains(layout.Dirs, store) {
			t.Fatal("derived native layout omitted the real machine-store directory")
		}
		found := false
		for _, mirror := range layout.Mirrors {
			if mirror.Path != filepath.Join(sidecar, claudeSharedDirRel) {
				continue
			}
			found = true
			if mirror.Target != store {
				t.Fatalf("workspace mirror targets %q instead of the selected machine store", mirror.Target)
			}
			if err := os.Symlink(mirror.Target, mirror.Path); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{claudeview.ViewFile, ".oauth_refresh.lock", claudeview.StorageLockDir} {
				a, err := os.Stat(filepath.Join(store, name))
				if err != nil {
					t.Fatal(err)
				}
				b, err := os.Stat(filepath.Join(mirror.Path, name))
				if err != nil || !os.SameFile(a, b) {
					t.Fatalf("workspace mirror reaches a different %s inode: %v", name, err)
				}
			}
		}
		if !found {
			t.Fatal("derived native layout omitted the workspace's machine-store mirror")
		}
	}
}
