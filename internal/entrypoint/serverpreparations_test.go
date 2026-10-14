package entrypoint

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestSelectedServerPreparationsUseEffectivePresetRendererNotCredentialArgs(t *testing.T) {
	restore := packload.OverrideSkewTolerance(false)
	defer restore()
	cases := []struct {
		name, table string
		want        int
	}{
		{"enabled", "{}", 1},
		{"removed", `{"sequential-thinking":null}`, 0},
		{"replaced", `{"sequential-thinking":{"command":"other","args":["secret"]}}`, 0},
		{"replaced-payload", `{"sequential-thinking":{"command":"/home/agent/.yolo/bin/mcp/node","args":["/elsewhere/server","secret"]}}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEnv(map[string]string{"JAIL_HOME": "/home/agent", "YOLO_MCP_PRESETS": `["sequential-thinking"]`, "YOLO_MCP_SERVERS": tc.table})
			selected := SelectedServerPreparations(e, nil, ProgramInstallOnly)
			if len(selected) != tc.want {
				t.Fatalf("effective vector %+v", selected)
			}
			if tc.want == 1 && (selected[0].Install.Package != "@modelcontextprotocol/server-sequential-thinking" || selected[0].Bin != "mcp-server-sequential-thinking" || len(selected[0].Install.InstallerEnv) > 0) {
				t.Fatalf("not installation-only: %+v", selected)
			}
		})
	}
}
