package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserConfigDisplayPathNamesTheLiveFile(t *testing.T) {
	for _, tc := range []struct{ existing, want string }{
		{"", UserConfigTOMLName},
		{UserConfigTOMLName, UserConfigTOMLName},
		{UserConfigLegacyJSONCName, UserConfigLegacyJSONCName},
		{UserConfigLegacyJSONName, UserConfigLegacyJSONName},
	} {
		h := t.TempDir()
		t.Setenv("HOME", h)
		dir := filepath.Join(h, ".config", "yolo-jail")
		if tc.existing != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.existing), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := UserConfigDisplayPath(); got != filepath.Join(dir, tc.want) {
			t.Errorf("existing %q: got %s, want %s", tc.existing, got, tc.want)
		}
	}
}
