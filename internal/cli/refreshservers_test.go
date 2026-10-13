package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestFreshServerHiddenDispatchUsesAppliedMCPPresets(t *testing.T) {
	// runInternal reads the staged jail pack tree and flips packload's process-wide decoder
	// tolerance. Keep both sides of this test on the host's strict authoring decoder.
	packload.OverrideSkewTolerance(false)
	t.Cleanup(func() { packload.OverrideSkewTolerance(false) })
	home := t.TempDir()
	packRoot := t.TempDir()
	prefix := t.TempDir()
	binDir := t.TempDir()
	log := filepath.Join(t.TempDir(), "npm.log")
	write := func(path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(binDir, "npm"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$FRESH_NPM_LOG\"\nexit 1\n", 0o755)
	t.Setenv("HOME", home)
	t.Setenv("JAIL_HOME", home)
	t.Setenv("YOLO_PACK_ROOT", packRoot)
	t.Setenv("YOLO_MCP_PRESETS", `["sequential-thinking"]`)
	t.Setenv("NPM_CONFIG_PREFIX", prefix)
	t.Setenv("FRESH_NPM_LOG", log)
	t.Setenv("PATH", binDir)

	if rc := runInternal([]string{"refresh-servers", "--fresh"}); rc == 0 {
		t.Fatal("the hidden explicit server-refresh caller accepted a failed applied MCP install")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "install -g --prefer-online @modelcontextprotocol/server-sequential-thinking" {
		t.Fatalf("fresh server dispatch used argv %q; want the applied preset's declared package", got)
	}
}
