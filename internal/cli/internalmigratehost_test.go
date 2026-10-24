package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// `yolo internal migrate-host --name=<launcher>` reaches hostmigrate: a named install leaves an
// unidentifiable GOBIN/yolo alone instead of refusing over it (matched-bundle.py passes the
// flag for `just deploy <name>`).
func TestMigrateHostNameFlagLeavesGOBINYoloAlone(t *testing.T) {
	gobin := t.TempDir()
	foreign := filepath.Join(gobin, "yolo")
	if err := os.WriteFile(foreign, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if rc := runMigrateHost([]string{"--gobin=" + gobin, "--name=yolo-stable"}); rc != 0 {
		t.Fatalf("a named migrate-host refused over GOBIN/yolo: rc=%d", rc)
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "#!/bin/sh\necho mine\n" {
		t.Fatalf("GOBIN/yolo changed: %q %v", got, err)
	}
	if rc := runMigrateHost([]string{"--gobin=" + gobin, "--bogus"}); rc != 2 {
		t.Errorf("an unknown flag: rc=%d, want 2", rc)
	}
}
