package run

// hostdoorwayoldercopy_test.go pins the `yolo host` half of the owner's coexistence decision
// (2026-10-16, superseding HD-D21's refusal; docs/design/host-daemon-ownership.md): a launch that
// starts a host service for its doorway while an older yolo's machine-wide copy of that service is
// still running opens the doorway anyway, printing the one line a jail launch prints
// (launchcheckrefusal_test.go) and stopping nothing. HostDoorways.Start is the call site;
// deleting its line fails the first test, and the second is its control.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/launchservice"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// errDoorwayStub is what each test's doorway start answers, ending the launch where the doorway
// would open.
var errDoorwayStub = errors.New("the doorway was started")

// hostDoorwayStartAgainst serves the fixture daemon, runs prep, and starts a `yolo host` doorway to
// it, reporting what Start printed, its error, and whether the doorway itself was started.
func hostDoorwayStartAgainst(t *testing.T, prep ...func(*testing.T)) (out string, err error, doorwayStarted bool) {
	t.Helper()
	launchCheckIsolation(t)
	serveLaunchCheckDaemon(t, answersTheCheck)
	for _, p := range prep {
		p(t)
	}
	pack := writeRealLoopholePack(t, "yjtest-doorway-pack", launchCheckFixtureName, askedEveryLaunchManifest(t))
	packs := []*packload.Pack{pack}
	d := &HostDoorways{
		plans: []*launchservice.Plan{launchservice.PlanAt(launchservice.Declared{
			Service: launchCheckFixtureName, Pack: "yjtest-doorway-pack", Cmd: []string{"yolo", "doorway"},
		}, "127.0.0.1:1", "YOLO_TEST_DOOR_TOKEN", "token", nil)},
		set:   loopholes.NewSet(loopholes.DiscoverOptions{PackModules: packLoopholeModules(packs)}),
		packs: packs,
	}
	var buf bytes.Buffer
	_, stop, _, err := d.Start(newConfig(), t.TempDir(), "pi", &buf,
		func(*launchservice.Plan, map[string]string) (*launchservice.Running, error) {
			doorwayStarted = true
			return nil, errDoorwayStub
		})
	if stop != nil {
		stop()
	}
	return buf.String(), err, doorwayStarted
}

// TestAHostDoorwayOpensBesideAnOlderYolosCopy: the doorway opens, the start prints the line naming
// the older copy and its optional later stop, and the older copy is still running afterwards.
func TestAHostDoorwayOpensBesideAnOlderYolosCopy(t *testing.T) {
	out, err, started := hostDoorwayStartAgainst(t, anOlderCopy)
	if !started || !errors.Is(err, errDoorwayStub) {
		t.Errorf("the doorway did not open beside an older yolo's machine-wide copy of its service "+
			"(started %v, err %v):\n%s", started, err, out)
	}
	assertTheOlderCopyLine(t, out)
	assertTheOlderCopyWasNotStopped(t)
}

// TestAHostDoorwayWithNoOlderCopyOpens is its control: the same start reaches the doorway and
// names no older copy.
func TestAHostDoorwayWithNoOlderCopyOpens(t *testing.T) {
	out, err, started := hostDoorwayStartAgainst(t)
	if !started || !errors.Is(err, errDoorwayStub) {
		t.Errorf("the doorway did not open (started %v, err %v), so its twin proves nothing",
			started, err)
	}
	assertNoOlderCopyLine(t, out)
}
