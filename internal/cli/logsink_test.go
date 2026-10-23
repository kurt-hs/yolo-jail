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

// TestMainJudgesGuestScopeAgainstTheRealHome pins cli.Main's paths.GuardGuestScope call site
// (MMP-D7): a run-as-you macos-user session's HOME is <ws>/.yolo/home, so without the guard a
// guest `yolo internal log-append` refuses to create the workspace's own .yolo ("the workspace
// CONTAINS your home"). Delete the call in Main and the first half fails.
func TestMainJudgesGuestScopeAgainstTheRealHome(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realHome := filepath.Join(base, "you")
	ws := filepath.Join(realHome, "code", "alpha")
	if err := os.MkdirAll(filepath.Join(ws, ".yolo", "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(ws, ".yolo", "home"))
	t.Setenv("YOLO_INVOCATION_CWD", "")
	appendReceipt := func() int {
		oldStdin := os.Stdin
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte("{\"schema\":1}\n"))
		_ = writer.Close()
		os.Stdin = reader
		defer func() { os.Stdin = oldStdin; _ = reader.Close() }()
		return Main([]string{"yolo", "internal", "log-append", ws, "receipts.jsonl"})
	}

	t.Setenv("YOLO_DARWIN_REAL_HOME", realHome)
	if rc := appendReceipt(); rc != 0 {
		t.Fatalf("with the real home named, log-append returned %d: Main no longer judges guest "+
			"scope against YOLO_DARWIN_REAL_HOME", rc)
	}
	if _, err := os.Stat(filepath.Join(ws, ".yolo", "receipts.jsonl")); err != nil {
		t.Fatalf("no receipt written: %v", err)
	}

	// The contrast: unnamed, HOME's rule stands and refuses.
	t.Setenv("YOLO_DARWIN_REAL_HOME", "")
	if rc := appendReceipt(); rc == 0 {
		t.Error("with no real home named, log-append admitted a workspace containing HOME; " +
			"this pin's contrast is gone")
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
