package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInternalLogAppendRejectsUnownedNamesAndLinkedStateDir(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"../victim", "launch.log", "startup.log.1"} {
		if rc := runInternalLogAppend([]string{ws, name}, bytes.NewBufferString("bad")); rc != 2 {
			t.Errorf("%s: %d", name, rc)
		}
	}
	victim := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(ws, ".yolo")); err != nil {
		t.Fatal(err)
	}
	if rc := runInternalLogAppend([]string{ws, "startup.log"}, bytes.NewBufferString("bad")); rc != 1 {
		t.Fatalf("linked state dir: %d", rc)
	}
	entries, err := os.ReadDir(victim)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("helper wrote through linked state dir")
	}
}
