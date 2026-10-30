package entrypoint

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// isolateWorkspaceMise points the retire surgery (retireWorkspaceMiseTools) at a mise.toml in a
// fresh temporary directory, which does not exist, for the whole package run, and returns the
// undo. TestMain calls it.
//
// THE WORKSPACE mise.toml IS NEVER THE MACHINE'S. Production reads the literal
// /workspace/mise.toml and REWRITES it when it holds a retired tool, so every test that renders
// a jail's mise config read, and could edit, whatever file that path names on the machine
// running the suite. In this repository's own jail that is the live checkout's mise.toml, which
// also made scripts/completion-census.py record these tests as readers of mise.toml only when
// the census ran from the checkout that IS /workspace. CI has no /workspace, so an absent file is
// what these tests always meant. A test about the surgery points it at its own fixture
// (mise_migration_test.go).
func isolateWorkspaceMise() (release func()) {
	dir, err := os.MkdirTemp("", "yolo-test-workspace-mise-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "entrypoint tests: no stand-in for the workspace mise.toml:", err)
		os.Exit(1)
	}
	restore := OverrideWorkspaceMisePath(filepath.Join(dir, "mise.toml"))
	return func() {
		restore()
		_ = os.RemoveAll(dir)
	}
}

// TestTheWorkspaceMiseIsIsolatedHere pins TestMain's isolateWorkspaceMise call: without it this
// package's tests read, and may rewrite, the machine's /workspace/mise.toml again.
func TestTheWorkspaceMiseIsIsolatedHere(t *testing.T) {
	if workspaceMisePath == "/workspace/mise.toml" {
		t.Fatal("the retire surgery reads the machine's /workspace/mise.toml; this package's TestMain must call isolateWorkspaceMise")
	}
	if _, err := os.Stat(filepath.Dir(workspaceMisePath)); err != nil {
		t.Fatalf("the stand-in workspace mise.toml's directory %q is gone: %v", filepath.Dir(workspaceMisePath), err)
	}
}
