package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// A workspace config naming on_request refuses the launch, naming the user-scope file
// the key belongs in (boundary-broker.md BB-D75).
func TestALaunchRefusesAWorkspaceOnRequest(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	os.Unsetenv("YOLO_VERSION")
	o, buf, _ := brokeredFixture(t)
	if err := os.WriteFile(filepath.Join(o.Workspace, config.WorkspaceConfigName),
		[]byte(`{"on_request": "echo approve"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := o.loadAndValidateConfig(); ok {
		t.Fatalf("a launch passed with a workspace on_request:\n%s", buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "on_request: user-scope only") || !strings.Contains(out, paths.UserConfigPath()) {
		t.Errorf("the refusal does not name the user config:\n%s", out)
	}
}
