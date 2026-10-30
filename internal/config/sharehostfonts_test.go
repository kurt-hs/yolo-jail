package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// share_host_fonts is user-scope only (OQ-NX9, ruled 2026-10-23, in
// docs/design/provisioner-sets.md): a workspace config travels with the repository, and the key
// opens the user's own font folder to the jail, so the workspace value is never READ, and the
// validator refuses it rather than letting it look like it worked.

func TestShareHostFontsReadsUserConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	ws := t.TempDir()
	t.Chdir(ws)
	userCfgPath := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
	if err := os.MkdirAll(filepath.Dir(userCfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if ShareHostFonts() {
		t.Error("ShareHostFonts with no user config = true, want false")
	}
	write(t, userCfgPath, `{"share_host_fonts": true}`)
	if !ShareHostFonts() {
		t.Error("ShareHostFonts with the user config true = false, want true")
	}
	write(t, userCfgPath, `{"share_host_fonts": tru`)
	if ShareHostFonts() {
		t.Error("an unparseable user config opted in; it must fail closed")
	}
	// A workspace "true" is never consulted.
	write(t, userCfgPath, `{"packs": []}`)
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"share_host_fonts": true}`)
	if ShareHostFonts() {
		t.Fatal("a WORKSPACE config turned share_host_fonts on: a repository could open its " +
			"user's font folder to every jail")
	}
}

func TestValidateShareHostFontsRefusesWorkspaceScope(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"share_host_fonts": true}`)
	var errs []string
	validateShareHostFonts(decode(t, `{"share_host_fonts": true}`), ws, &errs)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"config.share_host_fonts", "user-scope only", "config.toml"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors = %v, want one containing %q", errs, want)
		}
	}
}

// The call site: ValidateConfig itself refuses it, so deleting the wiring fails this test.
func TestValidateConfigRefusesWorkspaceShareHostFonts(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("HOME", t.TempDir())
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"share_host_fonts": true}`)
	errs, _ := ValidateConfig(decode(t, `{"share_host_fonts": true}`), ws, nil)
	if !strings.Contains(strings.Join(errs, "\n"), "share_host_fonts: user-scope only") {
		t.Errorf("ValidateConfig errors = %v, want the share_host_fonts workspace refusal", errs)
	}
}

func TestValidateShareHostFontsUserScopeAndShape(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	var errs []string
	validateShareHostFonts(decode(t, `{"share_host_fonts": true}`), ws, &errs)
	if len(errs) != 0 {
		t.Errorf("user-scoped share_host_fonts produced errors: %v", errs)
	}
	errs = nil
	validateShareHostFonts(decode(t, `{"share_host_fonts": "yes"}`), ws, &errs)
	if len(errs) == 0 || !strings.Contains(errs[0], "expected a boolean") {
		t.Errorf("errs = %v, want 'expected a boolean'", errs)
	}
	if _, known := knownTopLevelConfigKeys["share_host_fonts"]; !known {
		t.Error("share_host_fonts is not in knownTopLevelConfigKeys")
	}
}
