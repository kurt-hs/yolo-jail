package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// A TOML project file that spells a plural top-level key in the singular is refused naming the
// plural key, not as a bare unknown key.
func TestTOMLSingularRootKeyRefusalNamesThePlural(t *testing.T) {
	for _, tc := range []struct{ name, body, key, plural string }{
		{"mcp", "[mcp.foo]\ncommand = \"x\"\n", "mcp", "mcp_servers"},
		{"mount", "[[mount]]\nsource = \"/a\"\ntarget = \"/b\"\n", "mount", "mounts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ws := convertLaterEnv(t)
			writeConvertLaterFile(t, filepath.Join(ws, "yolo-jail.toml"), tc.body)
			_, err := LoadConfig(ws, true, func(string) {})
			want := `config.` + tc.key + `: unknown TOML config key "` + tc.key + `": the key is the plural "` + tc.plural + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("load error = %v, want it to contain %q", err, want)
			}
		})
	}
}
