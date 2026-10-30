package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStandardReaderAcceptsTheLegacyShapesTheConverterKeeps pins RC-D31: a TOML file holding the
// string command or boolean pids limit the legacy reader accepts loads, and a wrong-typed
// neighbor is still refused. It fails if either legacy spelling is refused again.
func TestStandardReaderAcceptsTheLegacyShapesTheConverterKeeps(t *testing.T) {
	for _, tc := range []struct{ name, body, refused string }{
		{name: "string command", body: "[mcp_servers.local]\ncommand = 'npx'\nargs = ['-y']\n"},
		{name: "number command refused", body: "[mcp_servers.local]\ncommand = 7\n", refused: "expected a list"},
		{name: "boolean pids limit", body: "[machine.resources]\npids_limit = true\n"},
		{name: "string pids limit refused", body: "[machine.resources]\npids_limit = '10'\n", refused: "expected a number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("YOLO_VERSION", "")
			t.Setenv(UserLayerEnv, "")
			workspace := t.TempDir()
			write(t, filepath.Join(workspace, WorkspaceTOMLConfigName), tc.body)
			_, err := LoadConfig(workspace, true, nil)
			if tc.refused == "" && err != nil {
				t.Fatalf("LoadConfig refused a legacy-accepted shape: %v", err)
			}
			if tc.refused != "" && (err == nil || !strings.Contains(err.Error(), tc.refused)) {
				t.Fatalf("LoadConfig err = %v, want %q", err, tc.refused)
			}
		})
	}
}
