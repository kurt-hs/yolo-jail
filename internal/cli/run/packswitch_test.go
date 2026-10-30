package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLaunchRefusesAnAllowTableForAnUnselectedPack pins the call site, not the predicate: a user
// config with `[allow.aws-auth]` and no `packs` entry for it must stop the launch in
// loadAndValidateConfig, with the command that fixes it. Deleting the AllowPackSwitchErrors call
// from preflight.go fails this test (the launch then reaches the repo-root refusal).
func TestLaunchRefusesAnAllowTableForAnUnselectedPack(t *testing.T) {
	ws := capabilityWorkspace(t, `{}`)
	cfgDir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("[allow.aws-auth]\nprofile = \"dev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	o := capabilityGateOptions(t, ws, nil, &stdout, &stderr)

	rc := Run(*o)

	if rc != 1 {
		t.Fatalf("Run() = %d, want 1\nstderr:\n%s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "yolo pack add aws-auth") {
		t.Errorf("the refusal does not name `yolo pack add aws-auth`:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), gotPastTheGate) {
		t.Errorf("the launch got past the pack-switch gate:\n%s", stderr.String())
	}
}
