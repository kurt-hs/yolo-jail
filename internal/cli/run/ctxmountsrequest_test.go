package run

// ctxmountsrequest_test.go pins RC-D24's last step at the launch's call site: a writable mount a
// project's TOML config requests, with the user config's `[allow.mounts] on_request = ["write"]`,
// is bound read-write by Run's assembler; without the line it is not bound at all. Delete the
// LoadWorkspaceRWMounts read from configCtxMounts and the first case fails.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func requestFixture(t *testing.T, user string) (ws, src string) {
	t.Helper()
	t.Setenv("YOLO_VERSION", "")
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src = filepath.Join(root, "datasets")
	ws = filepath.Join(root, "proj")
	for _, d := range []string{src, ws, filepath.Join(home, ".config", "yolo-jail")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, paths.WorkspaceConfigTOMLName),
		[]byte("mounts = [{ host = '"+src+"', at = '/ctx/data', write = true }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, src
}

func TestAnAllowedWorkspaceRequestIsBoundReadWrite(t *testing.T) {
	ws, src := requestFixture(t, "[allow.mounts]\non_request = [\"write\"]\n")
	merged, err := config.LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if errs, _ := config.ValidateConfig(merged, ws, nil); len(errs) != 0 {
		t.Fatalf("the allowed request did not validate: %q", errs)
	}
	o := goldenOptions(ws, os.Getenv("HOME"))
	argv, stream := assembleUserMounts(t, o, "podman", merged)
	if got, want := ctxMountArgs(argv), []string{src + ":/ctx/data"}; !slices.Equal(got, want) {
		t.Fatalf("ctx mounts = %q, want %q (read-write: no :ro)", got, want)
	}
	if !strings.Contains(stream, src) {
		t.Errorf("the read-write mount was not disclosed on the launch stream: %q", stream)
	}
}

func TestAWorkspaceRequestWithoutTheAllowLineIsNeverBound(t *testing.T) {
	ws, _ := requestFixture(t, "packs = []\n")
	merged, err := config.LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	o := goldenOptions(ws, os.Getenv("HOME"))
	argv, _ := assembleUserMounts(t, o, "podman", merged)
	if got := ctxMountArgs(argv); len(got) != 0 {
		t.Fatalf("a request the user config does not allow was bound: %q", got)
	}
}
