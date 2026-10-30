package check

// mountaccept_test.go pins host `yolo check --accept-config-changes` for the mounts a workspace
// config declares (oq-cx-symlink in docs/reference/context-mounts.md; RC-D24): it shows each
// mount at the destination its source resolves to, loud for a writable one, before it records,
// and the record holds that destination, so a launch binds exactly what the check showed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func mountCheckHome(t *testing.T, userTOML string) (root string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_USER_LAYER", "")
	must(t, os.MkdirAll(filepath.Join(home, ".config", "yolo-jail"), 0o755))
	must(t, os.WriteFile(filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), []byte(userTOML), 0o644))
	root, err = filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	return root
}

// A read-only mount through a link: the check shows the link's destination and records it.
func TestCheckAcceptShowsAndRecordsAMountsResolvedDestination(t *testing.T) {
	root := mountCheckHome(t, "packs = []\n")
	dest := filepath.Join(root, "first")
	must(t, os.MkdirAll(dest, 0o755))
	link := filepath.Join(root, "docs")
	must(t, os.Symlink(dest, link))
	var ws string
	out := runCheckOverConfigWith(t, `{"mounts": ["`+link+`:/ctx/docs"]}`, false, func(w string) { ws = w },
		func(o *Options) {
			o.AcceptConfigChanges = true
			onTheHost(o)
		})
	if !strings.Contains(out, "read-only mount: "+dest) {
		t.Fatalf("the check did not show the mount's resolved destination %s:\n%s", dest, out)
	}
	if got := config.ApprovedWorkspaceMounts(ws); len(got) != 1 || got[0].Resolved != dest {
		t.Fatalf("ApprovedWorkspaceMounts = %+v, want the destination the check showed recorded\n%s", got, out)
	}
}

// A writable request: the check prints the WRITABLE HOST MOUNT block before it records (MAJOR 3).
func TestCheckAcceptShowsAWritableRequestLoud(t *testing.T) {
	root := mountCheckHome(t, "packs = []\n\n[allow.mounts]\non_request = [\"write\"]\n")
	src := filepath.Join(root, "datasets")
	must(t, os.MkdirAll(src, 0o755))
	var ws string
	out := runCheckOverConfigWith(t, `{}`, false, func(w string) { ws = w }, func(o *Options) {
		o.AcceptConfigChanges = true
		onTheHost(o)
		must(t, os.Remove(filepath.Join(o.Workspace, config.WorkspaceConfigName)))
		must(t, os.WriteFile(filepath.Join(o.Workspace, paths.WorkspaceConfigTOMLName),
			[]byte("mounts = [{ host = '"+src+"', at = '/ctx/data', write = true }]\n"), 0o644))
	})
	if !strings.Contains(out, config.WritableMountMarker+": "+src) {
		t.Fatalf("the check recorded a writable mount without showing it:\n%s", out)
	}
	if got := config.ApprovedWorkspaceMounts(ws); len(got) != 1 || !got[0].Mount.RW || got[0].Resolved != src {
		t.Fatalf("ApprovedWorkspaceMounts = %+v, want the request recorded\n%s", got, out)
	}
}

// The check records only the read it validated: a gate read that differs from the workspace
// config the check validated is refused, and its mounts are not handed on to be recorded.
func TestCheckAcceptRefusesAReadItDidNotValidate(t *testing.T) {
	root := mountCheckHome(t, "packs = []\n")
	ws := filepath.Join(root, "proj")
	must(t, os.MkdirAll(ws, 0o755))
	must(t, os.WriteFile(filepath.Join(ws, config.WorkspaceConfigName), []byte(`{"mounts": ["`+root+`/x"]}`), 0o644))
	read, err := config.ReadWorkspaceForGate(ws)
	must(t, err)
	var out strings.Builder
	r := &reporter{w: &out}
	if _, ok := (&Options{}).acceptWorkspaceMounts(r, ws, nil, read, nil); ok {
		t.Fatalf("a read the check did not validate was accepted:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "nothing was recorded") {
		t.Fatalf("the refusal does not say nothing was recorded:\n%s", out.String())
	}
}

// A source written inside the workspace is refused by the check, and nothing is recorded.
func TestCheckAcceptRecordsNoMountInsideTheWorkspace(t *testing.T) {
	mountCheckHome(t, "packs = []\n")
	var ws string
	out := runCheckOverConfigWith(t, `{}`, false, func(w string) {
		ws = w
		must(t, os.MkdirAll(filepath.Join(w, "notes"), 0o755))
	}, func(o *Options) {
		o.AcceptConfigChanges = true
		onTheHost(o)
		must(t, os.WriteFile(filepath.Join(o.Workspace, config.WorkspaceConfigName),
			[]byte(`{"mounts": ["`+filepath.Join(o.Workspace, "notes")+`"]}`), 0o644))
	})
	if !strings.Contains(out, "inside the workspace") {
		t.Fatalf("the check did not refuse a mount inside the workspace:\n%s", out)
	}
	if _, err := os.Stat(config.ApprovalSnapshotPath(ws)); !os.IsNotExist(err) {
		t.Fatalf("a refused mount was recorded (stat err %v)\n%s", err, out)
	}
}
