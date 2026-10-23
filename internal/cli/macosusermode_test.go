package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// TestMacosLaunchRefusesSelfModeUntilItIsBuilt: a user config asking for `self` makes
// the macos-user launch exit 1 before it builds anything, and the line names the key
// and the one next step. It drives macosUserRun itself, so deleting the check from the
// seam fails this test rather than only the helper's.
func TestMacosLaunchRefusesSelfModeUntilItIsBuilt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userCfg := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte(`{"macos_user_mode": "self"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := macosUserModeRefusal()
	if !strings.Contains(msg, "macos_user_mode") || !strings.Contains(msg, "remove it") {
		t.Errorf("refusal = %q, want one naming macos_user_mode and 'remove it'", msg)
	}
	if !strings.Contains(msg, "not built yet") {
		t.Errorf("refusal = %q, want one saying the mode is not built yet", msg)
	}

	code := macosUserRun(nil, "", nil, nil, "", "", macosuser.HomeOverlay{}, macosuser.HostContext{},
		false, nil, nil, macosuser.JailDaemons{}, nil, nil)
	if code != 1 {
		t.Errorf("macosUserRun exit = %d, want 1 for a self-mode refusal", code)
	}
}

// TestMacosLaunchAllowsAccountMode: the default and an explicit `account` are not refused.
func TestMacosLaunchAllowsAccountMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	if msg := macosUserModeRefusal(); msg != "" {
		t.Errorf("no user config: refusal = %q, want none", msg)
	}
	userCfg := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte(`{"macos_user_mode": "account"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if msg := macosUserModeRefusal(); msg != "" {
		t.Errorf("account mode: refusal = %q, want none", msg)
	}
}
