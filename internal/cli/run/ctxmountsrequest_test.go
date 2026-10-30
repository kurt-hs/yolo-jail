package run

// ctxmountsrequest_test.go pins RC-D24's last steps at the launch's call sites: a writable mount
// a project's TOML config requests is bound read-write from what the config-change gate approved
// (runConfigGate keeps it on Options.workspaceMounts), at the path the gate resolved; nothing
// re-reads the workspace files to learn what to mount, so a request no gate approved is never
// bound; the gate refuses a request that fails a refusal before it asks anything; and the
// assembler refuses a launch whose approved request no longer resolves to the approved path.

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

const allowWrite = "[allow.mounts]\non_request = [\"write\"]\n"

func requestFixture(t *testing.T, user string) (ws, src string) {
	t.Helper()
	return requestFixtureFor(t, user, "")
}

// requestFixtureFor is a home with user as its user config and a workspace whose yolo-jail.toml
// requests host writable at /ctx/data; host "" requests a fresh directory, returned as src.
func requestFixtureFor(t *testing.T, user, host string) (ws, src string) {
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
	if host == "" {
		host = src
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, paths.WorkspaceConfigTOMLName),
		[]byte("mounts = [{ host = '"+host+"', at = '/ctx/data', write = true }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, src
}

// requestGateOptions are goldenOptions for a gate with no terminal and --accept-config-changes.
func requestGateOptions(t *testing.T, ws string) (*Options, *bytes.Buffer) {
	t.Helper()
	emptyLoopholeDirs(t)
	o := goldenOptions(ws, os.Getenv("HOME"))
	var buf bytes.Buffer
	o.Stderr, o.Stdout = &buf, &buf
	o.IsTTYStdin = func() bool { return false }
	o.IsTTYStderr = func() bool { return false }
	o.AcceptConfigChanges = true
	return o, &buf
}

func loadMerged(t *testing.T, ws string) *jsonx.OrderedMap {
	t.Helper()
	merged, err := config.LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return merged
}

// assembleIn runs the assembler and returns its input, so a test can read assemblyErr.
func assembleIn(t *testing.T, o *Options, cfg *jsonx.OrderedMap) ([]string, *assembleInput) {
	t.Helper()
	in := &assembleInput{cfg: cfg, rt: "podman", cname: "yolo-ws-abcd1234", packs: claudePackFixture(t),
		agentsPath: "/agents/yolo-ws-abcd1234", wsState: "/ws/.yolo/home", miseStore: "/mise-store",
		yoloVersion: "9.9.9-test", mountTargets: map[string]struct{}{}}
	return o.assembleRunCmd(in), in
}

// The gate approves the request and the assembler binds it read-write, at the resolved path:
// delete the line in runConfigGate that keeps the approved requests and this fails.
func TestAnAllowedWorkspaceRequestIsBoundReadWrite(t *testing.T) {
	ws, src := requestFixture(t, allowWrite)
	merged := loadMerged(t, ws)
	if errs, _ := config.ValidateConfig(merged, ws, nil); len(errs) != 0 {
		t.Fatalf("the allowed request did not validate: %q", errs)
	}
	o, buf := requestGateOptions(t, ws)
	if !o.checkConfigChanges(merged, "podman") {
		t.Fatalf("the gate refused an allowed request:\n%s", buf)
	}
	if !strings.Contains(buf.String(), config.WritableMountMarker) {
		t.Errorf("--accept-config-changes approved the writable mount without showing it:\n%s", buf)
	}
	argv, _ := assembleIn(t, o, merged)
	if got, want := ctxMountArgs(argv), []string{src + ":/ctx/data"}; !slices.Equal(got, want) {
		t.Fatalf("ctx mounts = %q, want %q (read-write: no :ro)", got, want)
	}
	if !strings.Contains(buf.String(), src) {
		t.Errorf("the read-write mount was not disclosed on the launch stream: %q", buf)
	}
}

// NO THIRD READ: with no gate run, the assembler binds no request, whatever the files say.
func TestARequestNoGateApprovedIsNeverBound(t *testing.T) {
	ws, _ := requestFixture(t, allowWrite)
	o, _ := requestGateOptions(t, ws)
	argv, _ := assembleIn(t, o, loadMerged(t, ws))
	if got := ctxMountArgs(argv); len(got) != 0 {
		t.Fatalf("a request no gate approved was bound: %q", got)
	}
}

// The gate refuses, before asking or recording anything, a request the user config does not
// allow and one the refusal set refuses (the reviewer's host = '~', which the launch's earlier
// validation would also refuse, but which the gate's own read must not trust that for).
func TestTheGateRefusesARequestItsOwnReadFails(t *testing.T) {
	for _, tc := range []struct{ name, user, host, want string }{
		{"no allow line", "packs = []\n", "", "[allow.mounts]"},
		{"the home", allowWrite, "~", "the read-write source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, _ := requestFixtureFor(t, tc.user, tc.host)
			o, buf := requestGateOptions(t, ws)
			if o.checkConfigChanges(jsonx.NewOrderedMap(), "podman") {
				t.Fatalf("the gate passed a request it must refuse:\n%s", buf)
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Fatalf("the refusal does not say %q:\n%s", tc.want, buf)
			}
			if _, err := os.Stat(config.ApprovalSnapshotPath(ws)); !os.IsNotExist(err) {
				t.Fatalf("a refused request was recorded (stat err %v)", err)
			}
			if len(o.workspaceMounts) != 0 {
				t.Fatalf("a refused gate left requests to bind: %+v", o.workspaceMounts)
			}
		})
	}
}

// BLOCKER 1, the bind: a request approved through a link that has since moved is not bound, and
// the assembler refuses the launch naming both paths and the next step.
func TestTheAssemblerRefusesARequestWhoseLinkMoved(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	harmless, other := filepath.Join(root, "harmless"), filepath.Join(root, "other")
	for _, d := range []string{harmless, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "cache")
	if err := os.Symlink(harmless, link); err != nil {
		t.Fatal(err)
	}
	ws, _ := requestFixtureFor(t, allowWrite, link)
	merged := loadMerged(t, ws)
	o, buf := requestGateOptions(t, ws)
	if !o.checkConfigChanges(merged, "podman") {
		t.Fatalf("the gate refused:\n%s", buf)
	}
	if len(o.workspaceMounts) != 1 || o.workspaceMounts[0].Resolved != harmless {
		t.Fatalf("approved = %+v, want the link resolved to %s", o.workspaceMounts, harmless)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	argv, in := assembleIn(t, o, merged)
	if got := ctxMountArgs(argv); len(got) != 0 {
		t.Fatalf("a request whose link moved was bound: %q", got)
	}
	if in.assemblyErr == nil || !strings.Contains(in.assemblyErr.Error(), other) ||
		!strings.Contains(in.assemblyErr.Error(), "launch again") {
		t.Fatalf("assemblyErr = %v, want the launch refused naming the new target and the next step", in.assemblyErr)
	}
}
