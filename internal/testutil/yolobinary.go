package testutil

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// YoloBinaryDir builds the real hidden-command dispatcher for generated-shell tests.
// It uses Go's shared default build cache, never a workspace-local cache.
func YoloBinaryDir(t *testing.T) string {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	dir := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "yolo"), "./cmd/yolo")
	cmd.Dir = filepath.Join(filepath.Dir(source), "../..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build yolo: %v\n%s", err, out)
	}
	return dir
}
