package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// A HOLD HOLDS AT THE HOST'S PRE-WRITE CHECK TOO (docs/reference/patched-forks.md §3.4, OQ-PBR5:
// "Your prior state should have absolutely no bearing on your current state if you turn off
// updates"): under an `agent_updates` hold, a patched fork's recorded patch failure does not stop
// `yolo host apply --assert` while a good build of it serves; without the hold it does. Red with
// hostPatchPreflight not asking whether the fork is held.
func TestAHeldForksRecordedFailureDoesNotStopTheHostApply(t *testing.T) {
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "")
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	fx.writeUserConfig(t, `,"host_management":"own"`)
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("the first launch: rc=%d\n%s", rc, out)
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
	fx.later(2 * time.Hour)
	if rc, _, out := fx.hostLaunch(t); rc == 0 || !strings.Contains(out, "0001-ten.patch") {
		t.Fatalf("the launch that records the failure: rc=%d\n%s", rc, out)
	}
	fx.writeUserConfig(t, `,"host_management":"own","agent_updates":{"forkpack":false}`)
	var out, errw bytes.Buffer
	rc := hostApplyRefreshAndRender(&out, &errw, false, true, nil, "", "")
	if strings.Contains(errw.String(), "yolo host: refusing the host apply") || strings.Contains(errw.String(), "ERROR: ") {
		t.Errorf("the held fork's recorded failure stopped the apply: rc=%d\n%s\n%s", rc, out.String(), errw.String())
	}
}

// THE SAME FOR A PATCHED EXTENSION at the apply `yolo pack update` runs: held, its linked good build
// serves and its recorded failure does not stop the apply. Red with hostPatchPreflight's tree half
// not asking whether the extension is held.
func TestAHeldExtensionsRecordedFailureDoesNotStopThePackUpdateApply(t *testing.T) {
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "")
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":false`)
	stubBins(t, "tool")
	var out, errw bytes.Buffer
	if rc := hostApplyRefreshAndRender(&out, &errw, false, true, nil, "", ""); rc != 0 {
		t.Fatalf("the first apply: rc=%d\n%s\n%s", rc, out.String(), errw.String())
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
	fx.setNow(fx.now.Add(2 * time.Hour))
	if advanceHostTrees(io.Discard, false, "", nil) {
		t.Fatal("the advance that records the failure did not stop")
	}
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":false,"agent_updates":{"treepack":false}`)
	out.Reset()
	errw.Reset()
	rc := hostApplyRefreshAndRender(&out, &errw, false, true, nil, "", packUpdateBuildsNoPatched)
	if strings.Contains(errw.String(), "yolo host: refusing the host apply") {
		t.Errorf("the held extension's recorded failure stopped the apply: rc=%d\n%s\n%s", rc, out.String(), errw.String())
	}
}
