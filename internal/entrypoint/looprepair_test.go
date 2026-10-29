package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveLoopingLinkPreservesConcurrentReplacement(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(home, "loop")
	if err := os.Symlink(link, link); err != nil {
		t.Fatal(err)
	}
	found := inspectLoopingLink(home, link)
	if !found.cycle || !found.inHome {
		t.Fatalf("loop inspection = %#v, want a proven in-home cycle", found)
	}

	originalRename := renameLoopEntry
	t.Cleanup(func() { renameLoopEntry = originalRename })
	calls := 0
	renameLoopEntry = func(oldDirFD int, old string, newDirFD int, new string) error {
		calls++
		if calls == 1 {
			if err := os.Remove(link); err != nil {
				return err
			}
			if err := os.WriteFile(link, []byte("raced replacement"), 0o600); err != nil {
				return err
			}
		}
		if err := renameNoReplaceBetweenAt(oldDirFD, old, newDirFD, new); err != nil {
			return err
		}
		if calls == 1 {
			return os.WriteFile(link, []byte("concurrent destination"), 0o600)
		}
		return nil
	}

	err := removeLoopingLink(home, found)
	if err == nil || !strings.Contains(err.Error(), "concurrent destination was left untouched") {
		t.Fatalf("removeLoopingLink error = %v, want safe refusal preserving the moved entry", err)
	}
	if got, err := os.ReadFile(link); err != nil || string(got) != "concurrent destination" {
		t.Errorf("concurrent destination = %q, %v; want its bytes preserved", got, err)
	}
	quarantines, err := filepath.Glob(filepath.Join(home, ".yolo-loop-quarantine-*", "entry"))
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("quarantined entries = %v, %v; want one preserved replacement", quarantines, err)
	}
	if got, err := os.ReadFile(quarantines[0]); err != nil || string(got) != "raced replacement" {
		t.Errorf("quarantined replacement = %q, %v; want its bytes preserved", got, err)
	}
}

func TestRemoveLoopingLinkRefusesParentSymlinkEscape(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	alias := filepath.Join(home, "redirect")
	loop := filepath.Join(outside, "loop")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	found := inspectLoopingLink(home, filepath.Join(alias, "loop"))
	if !found.cycle || !found.inHome {
		t.Fatalf("loop inspection = %#v, want lexical in-home cycle used by confinement check", found)
	}
	if err := removeLoopingLink(home, found); err == nil {
		t.Fatal("removeLoopingLink succeeded through a symlinked parent outside its root")
	}
	if fi, err := os.Lstat(loop); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("outside loop changed through a parent symlink: %v, %v", fi, err)
	}
}
