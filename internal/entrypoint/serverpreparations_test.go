package entrypoint

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// SelectedServerPreparations is the program each held pack `mcp` server's `bin` names, from the
// composed table alone: the server your null removed prepares nothing, and core names no server
// of its own since the `mcp_presets` key retired (a retired YOLO_MCP_PRESETS moves nothing).
func TestSelectedServerPreparationsFollowTheComposedTable(t *testing.T) {
	restore := packload.OverrideSkewTolerance(false)
	defer restore()
	for _, tc := range []struct {
		name, user string
		wire       bool
		want       int
	}{
		{"held", "", true, 1},
		{"removed", `{"chrome-devtools":null}`, true, 0},
		{"no composed table", "", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chromeServerEnv(t, tc.user, tc.wire)
			e.Vars["YOLO_MCP_PRESETS"] = `["chrome-devtools","sequential-thinking"]`
			packs, err := LoadJailPacks(e)
			if err != nil {
				t.Fatal(err)
			}
			selected := SelectedServerPreparations(e, packs, ProgramInstallOnly)
			if len(selected) != tc.want {
				t.Fatalf("selected %+v, want %d", selected, tc.want)
			}
			if tc.want == 1 && (selected[0].Bin != "chrome-devtools-mcp" || selected[0].Pack != "chrome-devtools" ||
				len(selected[0].Install.InstallerEnv) > 0) {
				t.Fatalf("not the pack's installation-only program: %+v", selected)
			}
		})
	}
}
