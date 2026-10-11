package cli

// hostactperf_test.go pins the STEP SPANS inside the host notch's long stages (hostactperf.go;
// docs/reference/perf-logging.md, "The host notch"): a `yolo host -- <bin>` that builds a patched
// extension or a patched fork records every step of that work under host.apply_gate and
// host.resolve_target, named by the tree or fork it is of, and `yolo host apply` records the same
// steps under its own family. Every cell runs the real front door with only the build jail, the floor's
// Node and the exec replaced, so deleting any one step's call site fails the cell naming that span.

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// hostPerfFile is the machine-wide host-notch perf log as it stands.
func hostPerfFile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(run.HostNotchPerfLogPath())
	if err != nil {
		t.Fatalf("the host-notch perf log: %v", err)
	}
	return string(data)
}

// assertSpansEnded fails for each name whose end is not in the perf log.
func assertSpansEnded(t *testing.T, log string, names ...string) {
	t.Helper()
	for _, name := range names {
		if !strings.Contains(log, " end    "+name+"  dur=") {
			t.Errorf("the perf log lacks the end of %s:\n%s", name, log)
		}
	}
}

// treeSteps are the step spans of one patched extension's first build at the host, in the order they
// run: the check and its slot, the walk's replay, the build's lock, its source, the build jail with
// its slot inside it, the wait for that jail to be gone, the admission and the receipt, the
// workspace's cleanup, the settle and the advance's finish, then the render's copy of the tree.
var treeSteps = []string{"check_wait", "check", "replay", "build_lock_wait", "source", "build_jail",
	"build_slot_wait", "reclaim", "admit", "receipt", "cleanup", "settle", "finish", "materialize"}

func treeSpans(family string) []string {
	var names []string
	for _, step := range treeSteps {
		names = append(names, family+".tree."+treeKeyCLI+"."+step)
	}
	return names
}

// A HOST LAUNCH THAT BUILDS ITS OWNER'S PATCHED EXTENSION records host.apply_gate's three steps,
// every step of the extension's advance, build and render, each named by the extension's key, and
// the two tree steps after the gate: the stop without a tree, and the lines naming each build.
func TestAHostLaunchTimesEveryStepOfItsExtensionsBuild(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true`)
	stubBins(t, "tool")
	t.Setenv(paths.TimingEnv, "1")
	got := captureHostExec(t)
	var errw syncBuffer
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc != 0 || !got.execed || len(fx.builds) != 1 {
		t.Fatalf("rc=%d execed=%v builds=%d\n%s", rc, got.execed, len(fx.builds), errw.String())
	}
	log := hostPerfFile(t)
	assertSpansEnded(t, log, "host.apply_gate", "host.apply_gate.advance", "host.apply_gate.preflight",
		"host.apply_gate.render", "host.tree_gate", "host.tree_lines")
	assertSpansEnded(t, log, treeSpans("host")...)
}

// A HOST LAUNCH OF A PATCHED FORK records host.resolve_target's floor step, every step of the fork's
// advance and build, and the floor's own: the preparation the advance runs in, the floor lock's wait,
// the install, the build's materialize, the launcher's command and the publish.
func TestAHostLaunchTimesEveryStepOfItsForksBuildAndFloorInstall(t *testing.T) {
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	t.Setenv(paths.TimingEnv, "1")
	t.Setenv(paths.VerboseEnv, "")
	if rc, _, out := fx.hostLaunch(t); rc != 0 || len(fx.builds) != 1 {
		t.Fatalf("rc=%d builds=%d\n%s", rc, len(fx.builds), out)
	}
	log := hostPerfFile(t)
	assertSpansEnded(t, log, "host.resolve_target", "host.resolve_target.floor")
	var names []string
	for _, step := range []string{"check_wait", "check", "replay", "build_lock_wait", "source", "build_jail",
		"reclaim", "admit", "receipt", "cleanup", "settle", "finish"} {
		names = append(names, "host.fork.forkpack/tool."+step)
	}
	for _, step := range []string{"prepare", "lock_wait", "install", "materialize", "exec_record", "publish"} {
		names = append(names, "host.floor.tool."+step)
	}
	assertSpansEnded(t, log, names...)
}

// `yolo host apply --assert` RECORDS THE SAME STEPS UNDER ITS OWN FAMILY, host_apply, so an apply's
// build is not mistaken for a launch's in the one machine-wide file.
func TestAHostApplyTimesItsExtensionsBuildUnderItsOwnFamily(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.writeTreePackConfig(t, "own")
	t.Setenv(paths.TimingEnv, "1")
	t.Setenv(paths.VerboseEnv, "")
	var errw syncBuffer
	if rc := hostApply([]string{"--assert"}, io.Discard, &errw, false, strings.NewReader("")); rc != 0 || len(fx.builds) != 1 {
		t.Fatalf("rc=%d builds=%d\n%s", rc, len(fx.builds), errw.String())
	}
	log := hostPerfFile(t)
	assertSpansEnded(t, log, treeSpans("host_apply")...)
	if strings.Contains(log, "host.tree.") {
		t.Errorf("an apply's steps are named as a launch's:\n%s", log)
	}
}
