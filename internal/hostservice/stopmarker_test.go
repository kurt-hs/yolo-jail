package hostservice

import (
	"os"
	"path/filepath"
	"testing"
)

// THE MARKER NAMES PIDS: it stands only for a death of a pid it lists, and is consumed by that one.
func TestAStopMarkerStandsOnlyForThePidsItNames(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "d.sock")
	if err := WriteStopMarker(socket, []int{41, 42}); err != nil {
		t.Fatal(err)
	}
	if TakeStopMarker(socket, 43) {
		t.Error("a marker naming 41 and 42 stood for the death of 43")
	}
	if !TakeStopMarker(socket, 42) {
		t.Error("a marker naming 42 did not stand for its death")
	}
	if _, err := os.Lstat(StopMarkerPath(socket)); !os.IsNotExist(err) {
		t.Errorf("the marker was not consumed (%v)", err)
	}
	if TakeStopMarker(socket, 41) {
		t.Error("a consumed marker stood for a second death")
	}
}

// /tmp IS SHARED: a planted link is never written through, and a marker that is not a regular
// file is never honored.
func TestAStopMarkerIsNeverALink(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "d.sock")
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, StopMarkerPath(socket)); err != nil {
		t.Fatal(err)
	}
	if TakeStopMarker(socket, 42) {
		t.Error("a link at the marker's path was honored")
	}
	if err := WriteStopMarker(socket, []int{7}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(target); string(raw) != "42\n" {
		t.Errorf("writing the marker wrote through a planted link: %q", raw)
	}
	if !TakeStopMarker(socket, 7) {
		t.Error("the marker written in the link's place does not stand for its pid")
	}
}
