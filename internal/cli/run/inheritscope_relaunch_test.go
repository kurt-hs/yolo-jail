package run

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// A real first install must not turn an unchanged account relaunch into an owner-file
// refusal. This is synthetic account delivery, not native Seatbelt execution or refresh.
func TestMacosUserAccountRelaunchKeepsDeliveredInheritedJSONC(t *testing.T) {
	treeLaunchHome(t, true) // Object pack entries exercise RenderFilteredForDelivery's JSONC fallback.
	ws := floortest.ResolvedTemp(t)
	sidecar := paths.WorkspaceHomeState(ws)
	guestHome := floortest.ResolvedTemp(t)
	if err := os.MkdirAll(filepath.Join(sidecar, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sidecar, "config"), filepath.Join(guestHome, ".config")); err != nil {
		t.Fatal(err)
	}
	entries, err := config.LoadPacks(nil)
	if err != nil {
		t.Fatal(err)
	}
	var selected []*packload.Pack
	for _, entry := range entries {
		p, problems := packload.LoadDir(strings.TrimPrefix(entry.Source, "file://"), entry.Name)
		if p == nil || len(problems) != 0 {
			t.Fatalf("loading selected pack %s: %v", entry.Name, problems)
		}
		selected = append(selected, p)
	}
	stageRoot := floortest.ResolvedTemp(t)
	const session = "synthetic-account"
	sealed := macosuser.StagedHomeOverlay(session, stageRoot)
	// Production staging seals the source before the installer hardlinks it. Keep
	// that same private source across launches; never install from writable Run staging.
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", stageRoot).Run() })
	calls := 0
	launch := func() (int, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		o := dispatchOptions(t, ws, "macos-user", &stdout, &stderr, nil)
		o.MacosUserLaunchProbes = setUpMacProbes
		o.CapturesDir = func() string { return filepath.Join(stageRoot, "captures") }
		o.BuildSlot = deliverTreeCopyWithFile(t)
		o.MacosUserRun = fakeMacosUserRun(func(c macosUserCall) int {
			calls++
			if !slices.Contains(c.overlay.Dests, inheritPreflightLegacyRel) {
				t.Fatal("fixture did not render the inherited JSONC fallback")
			}
			if !macosuser.HomeOverlayStagedCurrent(c.overlay.Tree, session, stageRoot) {
				for _, argv := range macosuser.StageHomeOverlayCommands(c.overlay.Tree, session, stageRoot) {
					if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
						t.Fatalf("synthetic overlay staging %v: %v\n%s", argv, err, out)
					}
				}
			}
			e := entrypoint.DarwinEnvFrom(map[string]string{
				"HOME": guestHome, "JAIL_HOME": guestHome,
				entrypoint.DarwinHomeSidecarEnv: sidecar,
				"YOLO_DARWIN_HOME_OVERLAY":      sealed,
			}, guestHome)
			e.Stderr = &stderr
			if err := entrypoint.InstallHomeOverlay(e, selected); err != nil {
				t.Fatalf("real installer: %v\n%s", err, stderr.String())
			}
			return 0
		})
		return Run(*o), stderr.String()
	}
	if rc, stderr := launch(); rc != 0 || calls != 1 {
		t.Fatalf("first Run() = %d, callbacks = %d\nstderr:\n%s", rc, calls, stderr)
	}
	delivered := filepath.Join(sidecar, "config", "yolo-jail", "config.jsonc")
	before := overlayFileInfo(t, delivered)
	body, err := os.ReadFile(delivered)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0o444 || !os.SameFile(before, overlayFileInfo(t, filepath.Join(sealed, filepath.FromSlash(inheritPreflightLegacyRel)))) {
		t.Fatal("first install did not hardlink the sealed 0444 inherited snapshot")
	}
	rc, stderr := launch()
	after := overlayFileInfo(t, delivered)
	got, err := os.ReadFile(delivered)
	if err != nil || !bytes.Equal(body, got) || before.Mode() != after.Mode() || !os.SameFile(before, after) {
		t.Fatalf("relaunch changed delivered inode, mode or bytes: read error %v", err)
	}
	if rc != 0 || calls != 2 {
		t.Fatalf("unchanged second Run() = %d, callbacks = %d; want successful dispatch with preserved delivery\nstderr:\n%s", rc, calls, stderr)
	}
}
