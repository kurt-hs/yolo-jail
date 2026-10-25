package logcap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A long-lived writer's log is bounded without being reopened: the descriptor opened once
// keeps writing, and once a write takes the file past the cap the log is trimmed in place and
// the archive gets the newest lines. A few large lines, not a loop.
func TestWriterTrimsTheLogItHoldsWithoutAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nix-roots.log")
	f, err := Open(path, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := NewWriter(path, f)
	line := append(bytes.Repeat([]byte("r"), MaxBytes/3), '\n')
	for i := 0; i < 4; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if got := size(t, path); got > MaxBytes {
		t.Fatalf("the active log is %d bytes, past the cap %d, with its writer still open", got, MaxBytes)
	}
	if got := size(t, path+ArchiveSuffix); got == 0 || got > MaxBytes {
		t.Fatalf("archive is %d bytes, want the newest lines within the cap", got)
	}
	// The same descriptor keeps appending to the log, not to a detached inode, and O_APPEND
	// lands it at the new end rather than past a hole.
	if _, err := w.Write([]byte("after the trim\n")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(body, []byte("after the trim\n")) || bytes.IndexByte(body, 0) >= 0 {
		t.Fatalf("the log after the trim does not end in the new line cleanly (%d bytes)", len(body))
	}
}

// A log moved away from path is not the writer's to trim through path.
func TestWriterLeavesAReplacedPathAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	f, err := Open(path, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(path, filepath.Join(dir, "moved.log")); err != nil {
		t.Fatal(err)
	}
	other := bytes.Repeat([]byte("o\n"), MaxBytes/2+1)
	if err := os.WriteFile(path, other, 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewWriter(path, f)
	if _, err := w.Write(append(bytes.Repeat([]byte("w"), MaxBytes), '\n')); err != nil {
		t.Fatal(err)
	}
	if got := size(t, path); got != int64(len(other)) {
		t.Fatalf("the writer trimmed a file it does not write: %d bytes, want %d", got, len(other))
	}
}
