package run

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// restoreWorkspaceMise is isolateWorkspaceMise's undo of the seam; nil until TestMain calls it.
var restoreWorkspaceMise func()

// isolateWorkspaceMise points the entrypoint's retire surgery at a mise.toml in a fresh
// temporary directory, which does not exist, for the whole package run, and returns the undo.
// TestMain calls it.
//
// THE WORKSPACE mise.toml IS NEVER THE MACHINE'S. Production reads the literal
// /workspace/mise.toml and REWRITES it when it holds a retired tool, and tests in this package
// render a jail's mise config in process, so they read, and could edit,
// whatever file that path names on the machine running the suite: in this repository's own
// jail, the live checkout's (internal/entrypoint's isolateWorkspaceMise carries the census
// measurement). CI has no /workspace, so an absent file is what these tests always meant.
func isolateWorkspaceMise() (release func()) {
	dir, err := os.MkdirTemp("", "yolo-test-workspace-mise-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "run tests: no stand-in for the workspace mise.toml:", err)
		os.Exit(1)
	}
	restoreWorkspaceMise = entrypoint.OverrideWorkspaceMisePath(filepath.Join(dir, "mise.toml"))
	return func() {
		restoreWorkspaceMise()
		_ = os.RemoveAll(dir)
	}
}

// TestTheWorkspaceMiseIsIsolatedHere pins TestMain's isolateWorkspaceMise call: without it this
// package's tests read, and may rewrite, the machine's /workspace/mise.toml again.
func TestTheWorkspaceMiseIsIsolatedHere(t *testing.T) {
	if restoreWorkspaceMise == nil {
		t.Fatal("the retire surgery reads the machine's /workspace/mise.toml; this package's TestMain must call isolateWorkspaceMise")
	}
}
