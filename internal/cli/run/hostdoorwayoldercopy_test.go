package run

// hostdoorwayoldercopy_test.go pins the `yolo host` half of HD-D21 (docs/design/
// host-daemon-ownership.md): a launch that starts a host service for its doorway while an older
// yolo's machine-wide copy of that service is still running refuses before the doorway opens,
// naming the stop of THAT copy, as a jail launch refuses (launchcheckrefusal_test.go). It took
// over the file of HD-D5 (3)'s preamble refusal, which no launch can meet once no launch fronts a
// machine-wide daemon. HostDoorways.Start is the call site; deleting its check fails the first
// test, and the second is its control.

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/launchservice"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// errDoorwayStub is what the control's doorway start answers, ending the launch where the doorway
// would open.
var errDoorwayStub = errors.New("the doorway was started")

// hostDoorwayStartAgainst serves the fixture daemon, runs prep, and starts a `yolo host` doorway to
// it, reporting Start's error and whether the doorway itself was started.
func hostDoorwayStartAgainst(t *testing.T, prep ...func(*testing.T)) (err error, doorwayStarted bool) {
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
	return err, doorwayStarted
}

func TestAHostDoorwayBesideAnOlderYolosCopyRefusesTheLaunch(t *testing.T) {
	err, started := hostDoorwayStartAgainst(t, anOlderCopy)
	if started {
		t.Error("the doorway opened beside an older yolo's machine-wide copy of its service")
	}
	if err == nil || errors.Is(err, errDoorwayStub) {
		t.Fatalf("Start returned %v, want the refusal", err)
	}
	for _, want := range []string{"a machine-wide daemon for '" + launchCheckFixtureName + "' that an older yolo",
		broker.StopHostWideCommand(launchCheckFixtureName)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	assertTheOlderCopyWasNotStopped(t)
}

// TestAHostDoorwayWithNoOlderCopyOpens is its control: the same start reaches the doorway.
func TestAHostDoorwayWithNoOlderCopyOpens(t *testing.T) {
	err, started := hostDoorwayStartAgainst(t)
	if !started || !errors.Is(err, errDoorwayStub) {
		t.Errorf("the doorway did not open (started %v, err %v), so its refusal twin proves nothing",
			started, err)
	}
}
