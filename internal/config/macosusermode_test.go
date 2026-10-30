package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestMacosUserModeReadsUserConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userCfg := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, set := MacosUserMode(); set || got != "" {
		t.Errorf("no user config: MacosUserMode() = %q, %v; want \"\", false", got, set)
	}
	write(t, userCfg, `{"macos_user_mode": "account"}`)
	if got, set := MacosUserMode(); !set || got != "account" {
		t.Errorf("MacosUserMode() = %q, %v; want account, true", got, set)
	}
	ws := t.TempDir()
	t.Chdir(ws)
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"macos_user_mode": "self"}`)
	if got, _ := MacosUserMode(); got != "account" {
		t.Fatalf("a WORKSPACE config chose the macos-user mode (%q)", got)
	}
}

func TestValidateMacosUserModeRefusesWorkspaceScope(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"macos_user_mode": "account"}`)
	var errs []string
	validateMacosUserMode(decode(t, `{"macos_user_mode": "account"}`), ws, &errs)
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "user-scope only") {
		t.Errorf("errors = %v, want one saying 'user-scope only'", errs)
	}
	if !strings.Contains(joined, paths.UserConfigDisplayPath()) {
		t.Errorf("errors = %v, want one naming the user config file %s", errs, paths.UserConfigDisplayPath())
	}
}

func TestValidateMacosUserModeShape(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	for _, bad := range []string{`"jail"`, `true`, `"Self"`} {
		var errs []string
		validateMacosUserMode(decode(t, `{"macos_user_mode": `+bad+`}`), ws, &errs)
		if len(errs) == 0 || !strings.Contains(errs[0], `"self" or "account"`) {
			t.Errorf("%s: errors = %v, want one naming \"self\" or \"account\"", bad, errs)
		}
	}
	for _, good := range []string{`"self"`, `"account"`} {
		var errs []string
		validateMacosUserMode(decode(t, `{"macos_user_mode": `+good+`}`), ws, &errs)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors %v", good, errs)
		}
	}
}
