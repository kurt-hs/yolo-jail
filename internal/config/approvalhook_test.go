package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// The hook is read from the user config alone, and is off unless set there.
func TestApprovalHookReadsUserConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv(UserLayerEnv, "")
	userCfg := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ApprovalHook(); got != "" {
		t.Fatalf("no config set a hook: %q", got)
	}
	ws := t.TempDir()
	t.Chdir(ws)
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"approval_hook": "echo approve"}`)
	if got := ApprovalHook(); got != "" {
		t.Fatalf("a WORKSPACE config set the hook: %q", got)
	}
	write(t, userCfg, `{"approval_hook": "notify-send \"$YOLO_REQUEST_SUMMARY\""}`)
	if got := ApprovalHook(); got != `notify-send "$YOLO_REQUEST_SUMMARY"` {
		t.Fatalf("the user config's hook: %q", got)
	}
	for _, off := range []string{`""`, `"  "`, `true`, `null`} {
		write(t, userCfg, `{"approval_hook": `+off+`}`)
		if got := ApprovalHook(); got != "" {
			t.Errorf("%s read as hook %q", off, got)
		}
	}
}

// A workspace config naming the hook is refused, naming the user-scope file; ValidateConfig,
// which `yolo check` and every launch run, carries the refusal (deleting its call fails this).
func TestValidateConfigRefusesAWorkspaceApprovalHook(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	write(t, filepath.Join(ws, WorkspaceConfigName), `{"approval_hook": "echo approve"}`)
	errs, _ := ValidateConfig(decode(t, `{"approval_hook": "echo approve"}`), ws, nil)
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "config.approval_hook: user-scope only") || !strings.Contains(joined, paths.UserConfigPath()) {
		t.Errorf("errors = %v, want a user-scope refusal naming %s", errs, paths.UserConfigPath())
	}
}

func TestValidateApprovalHookShape(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("YOLO_VERSION", "")
	for _, bad := range []string{`""`, `3`, `["notify-send"]`} {
		var errs []string
		validateApprovalHook(decode(t, `{"approval_hook": `+bad+`}`), ws, &errs)
		if !strings.Contains(strings.Join(errs, "\n"), "config.approval_hook: expected a shell command") {
			t.Errorf("%s: errors %v", bad, errs)
		}
	}
	var errs []string
	validateApprovalHook(decode(t, `{"approval_hook": "notify-send hi"}`), ws, &errs)
	if len(errs) != 0 {
		t.Errorf("a user-scope command was refused: %v", errs)
	}
}
