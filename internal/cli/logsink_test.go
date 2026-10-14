package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInternalLogAppendDispatchUsesConfinedWriter(t *testing.T) {
	ws := t.TempDir()
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("{\"schema\":1}\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	})
	if rc := runInternal([]string{"log-append", ws, "receipts.jsonl"}); rc != 0 {
		t.Fatalf("dispatch returned %d", rc)
	}
	got, err := os.ReadFile(filepath.Join(ws, ".yolo", "receipts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\"schema\":1}\n" {
		t.Fatalf("receipt content %q", got)
	}
}

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
