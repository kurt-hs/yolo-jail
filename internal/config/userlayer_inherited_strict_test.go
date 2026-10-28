package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictUserReaderRejectsInvalidInheritedLaunchTOML(t *testing.T) {
	home := layerHome(t, `{}`)
	t.Setenv("YOLO_VERSION", "9.9.9-test")
	t.Setenv(UserLayerEnv, "")
	path := filepath.Join(home, ".config", "yolo-jail", "inherited-launch.toml")
	write(t, path, "[machine.resources]\nunrecognized = true\n")
	if _, err := LoadReadableUserScopeConfig(true, func(string) {}); err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("strict public User reader silently accepted invalid inherited TOML: %v", err)
	}
}

func TestStrictUserReaderRejectsMixedInheritedLaunchFormats(t *testing.T) {
	for _, legacyName := range []string{"inherited-launch.jsonc", "inherited-launch.json"} {
		t.Run(legacyName, func(t *testing.T) {
			home := layerHome(t, `{}`)
			t.Setenv("YOLO_VERSION", "9.9.9-test")
			t.Setenv(UserLayerEnv, "")
			dir := filepath.Join(home, ".config", "yolo-jail")
			write(t, filepath.Join(dir, "inherited-launch.toml"), "[machine.resources]\nmemory = '8g'\n")
			if err := os.WriteFile(filepath.Join(dir, legacyName), []byte(`{"resources":{"memory":"16g"}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadReadableUserScopeConfig(true, func(string) {}); err == nil || !strings.Contains(err.Error(), "inherited-launch") {
				t.Fatalf("strict public User reader accepted mixed inherited formats: %v", err)
			}
		})
	}
}
