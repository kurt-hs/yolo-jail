package hostfloor

// captureplatform_test.go pins Floor.CapturePlatform at each of the floor's three materializes: an
// installer agent's capture, a plain fork's build and a patched fork's good build. A floor that
// names its capture platform judges every entry by it, not by the process's own, so a synthetic
// Linux floor on a Mac (the cli package's fork-floor fixtures) stays one platform throughout. Each
// entry here is of this process's platform and the floor names another, so a materialize that
// ignored the field would install it.

import (
	"context"
	"strings"
	"testing"
)

const foreignCapturePlatform = "plan9/mips"

func wantForeignPlatformRefusal(t *testing.T, err error, out string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "this is "+foreignCapturePlatform) {
		t.Fatalf("Ensure on a %s floor = %v, want the materialize to refuse an entry of another platform\n%s",
			foreignCapturePlatform, err, out)
	}
}

func TestTheFloorJudgesAnInstallerCaptureByItsCapturePlatform(t *testing.T) {
	w := newLinuxWorld(t)
	cs := newCaptureStore(t)
	w.floor.ResolveCapture = cs.resolve
	w.floor.CapturePlatform = foreignCapturePlatform
	cs.add("claude", "2.1.267", true)
	_, _, err := w.floor.Ensure(context.Background(), installerProgram("claude", "claude"))
	wantForeignPlatformRefusal(t, err, w.out.String())
}

func TestTheFloorJudgesAForkBuildByItsCapturePlatform(t *testing.T) {
	pin := forkCommitOne
	w, bs := forkWorld(t, &pin)
	bs.add("forkcli", forkCommitOne, true)
	w.floor.CapturePlatform = foreignCapturePlatform
	_, _, err := w.floor.Ensure(context.Background(), forkProgram())
	wantForeignPlatformRefusal(t, err, w.out.String())
}

func TestTheFloorJudgesAPatchedGoodBuildByItsCapturePlatform(t *testing.T) {
	w, pf := patchedWorld(t)
	pf.state = PatchedState{Recipe: patchedRecipeOne,
		Good: pf.good(forkCommitOne, patchedRecipeOne, "v1.0.0 (11111111) + 2 patches", true)}
	w.floor.CapturePlatform = foreignCapturePlatform
	_, _, err := w.floor.Ensure(context.Background(), patchedProgram())
	wantForeignPlatformRefusal(t, err, w.out.String())
}
