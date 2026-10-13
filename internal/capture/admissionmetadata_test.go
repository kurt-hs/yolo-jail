package capture

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAdmitEntryValidatedChecksRetainedCanonicalBeforeGuard(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("artifact ownership requires Linux")
	}
	for _, conflict := range []string{"platform", "legacy-relocation"} {
		t.Run(conflict, func(t *testing.T) {
			store := &Store{Dir: t.TempDir()}
			produce := func(id string) string {
				t.Helper()
				staged, err := store.Stage(id)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Run(Options{Home: t.TempDir(), Out: staged, Artifacts: []string{".local/bin/probe"}, ScanContentRefs: true, Command: []string{"/bin/sh", "-c", `mkdir -p "$HOME/.local/bin"; printf program > "$HOME/.local/bin/probe"`}}); err != nil {
					t.Fatal(err)
				}
				return staged
			}
			first := produce("old")
			m, err := ReadManifest(first)
			if err != nil {
				t.Fatal(err)
			}
			if conflict == "platform" {
				m.Platform = "darwin/arm64"
			} else {
				m.RefScan = RefScanSymlinks
				m.Relocatable = false
			}
			if err := WriteManifest(first, m); err != nil {
				t.Fatal(err)
			}
			old, err := store.AdmitEntryGuarded(context.Background(), first, func(commit func() error) error { return commit() })
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(ManifestPath(old.Root))
			if err != nil {
				t.Fatal(err)
			}
			staged := produce("new")
			required, err := ReadManifest(staged)
			if err != nil {
				t.Fatal(err)
			}
			incompatible := errors.New("retained metadata differs")
			guards := 0
			entry, err := store.AdmitEntryValidated(context.Background(), staged, func(retained *Manifest) error {
				if retained.Platform != required.Platform || retained.RefScan != required.RefScan || retained.Relocatable != required.Relocatable {
					return incompatible
				}
				return nil
			}, func(commit func() error) error { guards++; return commit() })
			if !errors.Is(err, incompatible) || entry != nil || guards != 0 {
				t.Fatalf("entry=%v err=%v guards=%d", entry, err, guards)
			}
			after, err := os.ReadFile(ManifestPath(old.Root))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("canonical metadata overwritten")
			}
			if _, err := os.Stat(filepath.Join(staged, "tree")); err != nil {
				t.Fatal("failed publication deleted staged output")
			}
			if !old.Complete() {
				t.Fatal("legacy completion changed")
			}
		})
	}
}
