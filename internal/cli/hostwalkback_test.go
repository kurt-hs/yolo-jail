package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
)

// A WALK-BACK THAT SUCCEEDS AT `yolo host -- <bin>` says one line, the disclosure, and no error block
// for the newest it walked back from: the block is deferred while the walk-back is on. Red with walk()
// reporting the newest's block before failedOrWalkBack runs.
func TestASuccessfulHostWalkBackSaysOnlyItsDisclosureLine(t *testing.T) {
	t.Setenv(patchrecovery.AllowPatchFailuresEnv, "")
	t.Setenv(patchrecovery.AllowOlderUpstreamEnv, "")
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("the first launch: rc=%d\n%s", rc, out)
	}
	fx.commit(t, "v1.2.0", fitting)
	fx.commit(t, "v1.3.0", conflicting)
	fx.later(2 * time.Hour)
	t.Setenv(patchrecovery.AllowOlderUpstreamEnv, "forkpack/tool")
	rc, target, out := fx.hostLaunch(t)
	if rc != 0 || target == "" || !strings.Contains(out, "fork forkpack/tool: walked back to v1.2.0") {
		t.Fatalf("the walk-back did not run v1.2.0: rc=%d target=%q\n%s", rc, target, out)
	}
	if strings.Contains(out, "ERROR: ") || strings.Contains(out, "Start anyway:") {
		t.Errorf("a successful walk-back said the newest's error block:\n%s", out)
	}
}

// A WALK-BACK THAT RUNS OUT says the newest's block once, and its hatch list leaves out the walk-back
// the user already asked for and that already failed. Red with the block said before the walk-back,
// or recoveryHatches listing a hatch whose variable already covers the key.
func TestAnExhaustedHostWalkBackSaysTheBlockOnceWithoutItsOwnHatch(t *testing.T) {
	t.Setenv(patchrecovery.AllowPatchFailuresEnv, "")
	t.Setenv(patchrecovery.AllowOlderUpstreamEnv, "")
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("the first launch: rc=%d\n%s", rc, out)
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
	fx.commit(t, "v1.3.0", conflicting)
	fx.later(2 * time.Hour)
	t.Setenv(patchrecovery.AllowOlderUpstreamEnv, "forkpack/tool")
	rc, _, out := fx.hostLaunch(t)
	if rc == 0 {
		t.Fatalf("an exhausted walk-back ran: rc=%d\n%s", rc, out)
	}
	if n := strings.Count(out, "ERROR: "); n != 1 || !strings.Contains(out, ": patch series does not apply to upstream v1.3.0") {
		t.Errorf("the error block is said %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "Start anyway:"); n != 1 || strings.Contains(out, patchrecovery.AllowOlderUpstreamEnv+"=") ||
		!strings.Contains(out, "YOLO_ALLOW_PATCH_FAILURES=forkpack/tool yolo host -- tool ") {
		t.Errorf("the hatch list (said %d times) lists the walk-back already tried, or lacks the bypass:\n%s", n, out)
	}
}
