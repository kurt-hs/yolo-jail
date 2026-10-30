package cli

// applyhostmanifestless_test.go pins the HOST notch of RC-D13's C6: a pack folder with only
// `home/` and `overlays/` directories, and no pack.json or pack.toml, delivers at `apply --host`.
// The loader discovers both (packload/conventions.go); this proves the host render consumes them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestlessHomeAndOverlayFolderAtEveryNotch(t *testing.T) {
	home := t.TempDir()
	packDir := filepath.Join(t.TempDir(), "ml")
	writeFile(t, filepath.Join(packDir, "home", ".config", "mlnote", "hello.txt"), "manifest-less home file\n")
	writeFile(t, filepath.Join(packDir, "overlays", "claude-settings.jsonc"),
		"{ // overlay\n \"mlOverlayKey\": \"from-overlay\" }\n")
	selectOwned(t, home, `"claude",{"source":"file://`+packDir+`","name":"ml"}`)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	rc, report := applyWith(t, true, strings.NewReader("y\n"))
	if rc != 0 {
		t.Fatalf("host apply --assert rc=%d\n%s", rc, report)
	}
	got, err := os.ReadFile(filepath.Join(home, ".config", "mlnote", "hello.txt"))
	if err != nil || string(got) != "manifest-less home file\n" {
		t.Fatalf("a manifest-less pack's home/ file did not reach ~/.config/mlnote/hello.txt: %v %q\n%s", err, got, report)
	}
	settings, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(settings), `"mlOverlayKey"`) {
		t.Fatalf("a manifest-less pack's overlays/claude-settings.jsonc did not reach ~/.claude/settings.json: %v\n%s\nreport:\n%s",
			err, settings, report)
	}
}
