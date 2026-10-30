package run

// mountsourcelink_test.go pins oq-cx-symlink (docs/reference/context-mounts.md) at the launch's
// call sites, for READ-ONLY mounts a workspace config declares, JSONC and TOML alike: a source
// written inside the workspace is refused, whatever it links to, and a source reached through a
// link elsewhere is bound only at the destination the gate showed and recorded.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// roLinkFixture is a home with an empty user config and a workspace whose config (file name
// cfgName, contents body) declares a mount; the workspace holds notes -> <home>/.ssh, the
// agent-planted link the reproduction is about.
func roLinkFixture(t *testing.T, cfgName, body string) (ws, home, root string) {
	t.Helper()
	t.Setenv("YOLO_VERSION", "")
	var err error
	home, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	root, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws = filepath.Join(root, "proj")
	for _, d := range []string{ws, filepath.Join(home, ".ssh"), filepath.Join(home, ".config", "yolo-jail")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(ws, "notes")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), []byte("packs = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, cfgName), []byte(strings.ReplaceAll(body, "WS", ws)), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, home, root
}

// THE REPRODUCTION: a workspace config naming ./notes, a link the agent planted in the workspace
// pointing at ~/.ssh, must never become a read-only bind of ~/.ssh. Before the fix the gate
// passed it (its approval showed only the written text) and the assembler bound
// <home>/.ssh:/ctx/.ssh:ro. Now validation and the gate refuse it, naming the workspace.
func TestAWorkspaceMountWrittenInsideTheWorkspaceIsNeverBound(t *testing.T) {
	for _, tc := range []struct{ name, file, body string }{
		{"jsonc relative", config.WorkspaceConfigName, `{"mounts": ["./notes"]}`},
		{"jsonc absolute", config.WorkspaceConfigName, `{"mounts": ["WS/notes"]}`},
		{"toml", paths.WorkspaceConfigTOMLName, "mounts = ['./notes']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, home, _ := roLinkFixture(t, tc.file, tc.body)
			t.Chdir(ws)
			merged := loadMerged(t, ws)
			errs, _ := config.ValidateConfig(merged, ws, nil)
			o, buf := requestGateOptions(t, ws)
			passed := o.checkConfigChanges(merged, "podman")
			// A launch stops at a validation error or a refused gate; only one both passed would
			// go on to assemble, and that one must not bind ~/.ssh.
			if passed && len(errs) == 0 {
				argv, in := assembleIn(t, o, merged)
				for _, a := range ctxMountArgs(argv) {
					if strings.Contains(a, filepath.Join(home, ".ssh")) && in.assemblyErr == nil {
						t.Fatalf("a link in the workspace bound ~/.ssh: %q", a)
					}
				}
			}
			if !joinedContains(errs, "inside the workspace") {
				t.Errorf("validation did not refuse the source written inside the workspace: %q", errs)
			}
			if passed || !strings.Contains(buf.String(), "inside the workspace") {
				t.Errorf("the gate passed the source written inside the workspace (passed=%v):\n%s", passed, buf)
			}
		})
	}
}

// A read-only workspace mount written inside another mount the config declares is refused: no
// mounts in mounts.
func TestAWorkspaceMountInsideAnotherMountIsRefused(t *testing.T) {
	_, _, root := roLinkFixture(t, "unused", "")
	outer := filepath.Join(root, "outer")
	if err := os.MkdirAll(filepath.Join(outer, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(root, "proj")
	if err := os.WriteFile(filepath.Join(ws, paths.WorkspaceConfigTOMLName),
		[]byte("mounts = ['"+outer+"', '"+filepath.Join(outer, "inner")+"']\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errs, _ := config.ValidateConfig(loadMerged(t, ws), ws, nil)
	if !joinedContains(errs, "inside the mount "+outer) {
		t.Fatalf("a mount inside another mount was not refused: %q", errs)
	}
}

// A read-only workspace mount reached through a link outside the workspace is bound at the
// destination the gate showed and recorded; when the link moves after the approval, the launch
// is refused naming the new destination and the next step.
func TestAWorkspaceMountThroughALinkIsBoundAtItsApprovedDestination(t *testing.T) {
	ws, _, root := roLinkFixture(t, "unused", "")
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for _, d := range []string{first, second} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "docs")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, config.WorkspaceConfigName),
		[]byte(`{"mounts": ["`+link+`:/ctx/docs"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	merged := loadMerged(t, ws)
	o, buf := requestGateOptions(t, ws)
	if !o.checkConfigChanges(merged, "podman") {
		t.Fatalf("the gate refused a mount through a link outside the workspace:\n%s", buf)
	}
	if !strings.Contains(buf.String(), first) {
		t.Errorf("the approval did not show the resolved destination %s:\n%s", first, buf)
	}
	argv, _ := assembleIn(t, o, merged)
	if got := ctxMountArgs(argv); len(got) != 1 || got[0] != first+":/ctx/docs:ro" {
		t.Fatalf("ctx mounts = %q, want the approved destination read-only", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	argv, in := assembleIn(t, o, merged)
	if got := ctxMountArgs(argv); len(got) != 0 {
		t.Fatalf("a mount whose link moved was bound: %q", got)
	}
	if in.assemblyErr == nil || !strings.Contains(in.assemblyErr.Error(), second) ||
		!strings.Contains(in.assemblyErr.Error(), "launch again") {
		t.Fatalf("assemblyErr = %v, want the launch refused naming the new destination and the next step", in.assemblyErr)
	}
}

func joinedContains(msgs []string, s string) bool {
	return strings.Contains(strings.Join(msgs, "\n"), s)
}
