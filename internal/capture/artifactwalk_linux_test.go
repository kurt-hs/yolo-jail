//go:build linux

package capture

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Swap after the opened root is pinned, exactly between classification and the
// first regular-file transfer. The pathname now reaches excluded config, but the
// production transfer must read only the already-open no-follow ancestor.
func TestProgramArtifactWalkPinnedParentSwapDoesNotReadExcludedFile(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{".local/lib/probe", ".config"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".local/lib/probe/z"), []byte("program"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config/z"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := newDriver(Options{Home: home, Out: filepath.Join(t.TempDir(), "out"), Artifacts: []string{".local/lib/probe"}, Command: []string{"unused"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.pinArtifactHome(); err != nil {
		t.Fatal(err)
	}
	defer d.artifactHome.Close()
	var got []byte
	err = d.artifactWalk(func(rel string, f *os.File, info os.FileInfo, target string) error {
		if rel == ".local/lib/probe" {
			if err := os.Rename(filepath.Join(home, rel), filepath.Join(home, "old")); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(home, ".config"), filepath.Join(home, rel))
		}
		if rel == ".local/lib/probe/z" {
			var err error
			got, err = io.ReadAll(f)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "program" {
		t.Fatalf("read excluded bytes: %q", got)
	}
	// A fresh walk must fail closed, rather than follow the swapped parent.
	if err := d.transferArtifacts(snapshot{}, &Result{}); err == nil {
		t.Fatal("swapped source link transferred")
	}
}
