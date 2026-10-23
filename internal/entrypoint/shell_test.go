package entrypoint

import (
	"strings"
	"testing"
)

// The bootstrap installs no MCP server package: the preset-gated `npm install -g` it carried
// went with the `mcp_presets` key (docs/design/mcp-presets-removal.md §13 step 3), and a pack's
// server program is installed by the readiness act and its launcher like any other program. A
// retired YOLO_MCP_PRESETS an older host still sends must move nothing.
func TestBootstrapInstallsNoMCPPresetPackage(t *testing.T) {
	e := &Env{Home: t.TempDir(), Vars: map[string]string{
		"YOLO_MCP_PRESETS": `["chrome-devtools","sequential-thinking"]`,
	}}
	script := BootstrapScript(e)
	for _, gone := range []string{"YOLO_MCP_NPM", "chrome-devtools-mcp", "server-sequential-thinking"} {
		if strings.Contains(script, gone) {
			t.Errorf("the bootstrap script still carries %q", gone)
		}
	}
}
