package cli

import (
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// THE GUEST'S SCOPE RULE IS THE GUEST'S: on the host a YOLO_DARWIN_REAL_HOME left in the shell
// must not move workspace-scope judgment off the host's own home. Inside a jail (YOLO_VERSION,
// which the macos-user launcher sets on every sandbox process) it still judges by the named home.
// Fails if guestScopeGuard honors the variable on the host.
func TestGuestScopeGuardIsIgnoredOnTheHost(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv(paths.DarwinRealHomeEnv, other)

	t.Setenv("YOLO_VERSION", "")
	restore := guestScopeGuard()
	hostBreach := paths.WorkspaceScopeBreach(home)
	restore()
	if hostBreach == nil {
		t.Error("on the host, YOLO_DARWIN_REAL_HOME moved the scope judgment off the real home")
	}

	t.Setenv("YOLO_VERSION", "9.9.9-test")
	restore = guestScopeGuard()
	guestBreach := paths.WorkspaceScopeBreach(other)
	restore()
	if guestBreach == nil {
		t.Error("in a jail, the named real home is not the boundary")
	}
}
