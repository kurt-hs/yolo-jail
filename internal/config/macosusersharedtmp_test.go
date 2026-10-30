package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// The hatch is read from the user config alone: a workspace value never opens /tmp.
func TestMacosUserSharedTmpReadsUserConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userCfg := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if MacosUserSharedTmp() {
		t.Error("no user config opened the shared /tmp")
	}
	ws := t.TempDir()
	t.Chdir(ws)
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"macos_user_shared_tmp": true}`)
	if MacosUserSharedTmp() {
		t.Fatal("a WORKSPACE config opened the shared /tmp")
	}
	write(t, userCfg, `{"macos_user_shared_tmp": true}`)
	if !MacosUserSharedTmp() {
		t.Error("the user config's true did not open the shared /tmp")
	}
	write(t, userCfg, `{"macos_user_shared_tmp": "yes"}`)
	if MacosUserSharedTmp() {
		t.Error("a non-boolean value opened the shared /tmp")
	}
}

func TestValidateMacosUserSharedTmpRefusesWorkspaceScope(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"macos_user_shared_tmp": true}`)
	var errs []string
	validateMacosUserSharedTmp(decode(t, `{"macos_user_shared_tmp": true}`), ws, &errs)
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "user-scope only") || !strings.Contains(joined, paths.UserConfigDisplayPath()) {
		t.Errorf("errors = %v, want one saying 'user-scope only' and naming %s", errs, paths.UserConfigDisplayPath())
	}
}

// ValidateConfig runs the check: deleting its call in validate.go fails this.
func TestValidateConfigChecksMacosUserSharedTmp(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	errs, _ := ValidateConfig(decode(t, `{"macos_user_shared_tmp": "yes"}`), ws, nil)
	if !strings.Contains(strings.Join(errs, "\n"), "config.macos_user_shared_tmp: expected true or false") {
		t.Errorf("ValidateConfig did not check macos_user_shared_tmp: %v", errs)
	}
	for _, good := range []string{`true`, `false`} {
		var errs []string
		validateMacosUserSharedTmp(decode(t, `{"macos_user_shared_tmp": `+good+`}`), ws, &errs)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors %v", good, errs)
		}
	}
}
