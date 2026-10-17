package cli

// cachedgoodmac_test.go pins cached-good recovery (YOLO_USE_CACHED_GOOD, PF-D82) for a patched
// fork's darwin build on macos-user (docs/reference/patched-forks.md PF-D97): the build is captured
// in its build's staging home, not the container home /home/agent, and is served when it meets the
// relocation contract the sandbox's materialize enforces; and on a Mac that alternates podman and
// macos-user launches, each platform's own build of the one recorded good commit is the one served.

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
)

const macTestPlatform = "darwin/arm64"

// macStagingHome is a darwin fork build's capture home, the shape macosuser.ForkBuildStagingRoot
// gives it.
const macStagingHome = "/Users/Shared/yolo-jail/fork-builds/0123456789abcdef/home"

// macAdmittedGood admits a darwin build of v1.1.0 captured in macStagingHome, as a macos-user
// launch's advance does.
func macAdmittedGood(t *testing.T, relocatable bool) (*patchedAdvanceFixture, string) {
	t.Helper()
	fx := newPatchedAdvanceFixture(t, "")
	fx.platform, fx.captureHome, fx.relocatable = macTestPlatform, macStagingHome, relocatable
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	good, out, _ := fx.launchOn(t, "macos-user", macTestPlatform)
	if good.delivery.Key == "" {
		t.Fatalf("fixture did not admit the darwin good build:\n%s\n%s", good.delivery.Reason, out)
	}
	return fx, good.delivery.Key
}

// askForCachedGoodOn is askForCachedGood for a launch on platform under runtime.
func askForCachedGoodOn(t *testing.T, fx *patchedAdvanceFixture, platform, runtime string) (entrypoint.ForkDelivery, string) {
	t.Helper()
	var output bytes.Buffer
	request := run.ForkBuildRequest{Pins: []packload.ForkPin{{Fork: fx.fork(t)}}, CachedGoodOwner: "forkpack/tool",
		Platform: platform, Runtime: runtime, Workspace: t.TempDir(), Interrupt: &run.ActInterrupt{},
		Hand: func(string, run.HandedFork) error { return nil },
	}
	got, _ := runBuildSlot(run.BuildSlotRequest{Forks: &request, MaxBuilds: 1}, &output, false)
	return got["tool"], output.String()
}

// A DARWIN GOOD BUILD CAPTURED IN ITS STAGING HOME IS SERVED on macos-user when it is relocatable,
// through the build slot's cached-good path (runCachedGoodFork). Require the container home of
// every platform again and this fails.
func TestCachedGoodServesARelocatableDarwinBuildFromItsStagingHome(t *testing.T) {
	fx, key := macAdmittedGood(t, true)
	d, out := askForCachedGoodOn(t, fx, macTestPlatform, "macos-user")
	if d.Key != key {
		t.Fatalf("cached good on macos-user delivered %+v, want the darwin good %s\n%s", d, key, out)
	}
}

// A DARWIN BUILD THE MATERIALIZE COULD NOT MOVE is refused, naming why, rather than handed to a
// sandbox materialize that would refuse it later.
func TestCachedGoodRefusesADarwinBuildThatIsNotRelocatable(t *testing.T) {
	fx, _ := macAdmittedGood(t, false)
	d, out := askForCachedGoodOn(t, fx, macTestPlatform, "macos-user")
	if d.Key != "" || !strings.Contains(d.Reason, "cannot move from its build's home "+macStagingHome) {
		t.Errorf("a non-relocatable darwin good was not refused for its home: %+v\n%s", d, out)
	}
}

// A LINUX BUILD STILL NEEDS THE CONTAINER HOME: the darwin rule does not loosen it.
func TestCachedGoodStillRefusesALinuxBuildOutsideTheContainerHome(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.captureHome, fx.relocatable = "/tmp/elsewhere/home", true
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	if good, out, _ := fx.launch(t, "podman"); good.delivery.Key == "" {
		t.Fatalf("fixture did not admit the good build:\n%s", out)
	}
	d, _ := askForCachedGood(t, fx, "forkpack/tool")
	if d.Key != "" || !strings.Contains(d.Reason, "not the container home /home/agent") {
		t.Errorf("a linux good captured outside /home/agent was not refused: %+v", d)
	}
}

// THE ADVANCE OFFERS THE HATCH for a darwin good: a failed edit on macos-user lists
// YOLO_USE_CACHED_GOOD first, as on a container backend (TestAFailedEditListsCachedGood).
func TestAFailedEditOnMacosUserListsCachedGood(t *testing.T) {
	t.Setenv(patchrecovery.AllowOlderUpstreamEnv, "")
	fx, _ := macAdmittedGood(t, true)
	writeFile(t, fx.manifest, strings.Replace(mustRead(t, fx.manifest), `"build":"sh build.sh"`, `"build":"sh build2.sh"`, 1))
	fx.rc = 2
	fx.later(2 * time.Hour)
	failed, out, _ := fx.launchOn(t, "macos-user", macTestPlatform)
	if failed.delivery.Key != "" {
		t.Fatalf("the failed edit handed %+v\n%s", failed.delivery, out)
	}
	if got := hatchEnvs(failed.delivery.Recovery); len(got) == 0 || got[0] != "YOLO_USE_CACHED_GOOD=forkpack/tool" {
		t.Errorf("the hatch list = %v, want cached-good first\n%s", got, out)
	}
}

// ON A MAC THAT ALTERNATES BACKENDS, cached good serves THIS platform's build of the recorded good
// commit: the record's one Good.Entry names whichever platform built last, and the other platform's
// launch resolves its own build of that commit, as the advance's exact lookup does.
func TestCachedGoodServesThisPlatformsBuildOfTheGoodCommit(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	linux, out, _ := fx.launch(t, "podman")
	if linux.delivery.Key == "" {
		t.Fatalf("fixture did not admit the linux good:\n%s", out)
	}
	fx.platform, fx.captureHome, fx.relocatable = macTestPlatform, macStagingHome, true
	fx.programBody = "#!/bin/sh\n# darwin\n"
	mac, out, _ := fx.launchOn(t, "macos-user", macTestPlatform)
	if mac.delivery.Key == "" || mac.delivery.Key == linux.delivery.Key {
		t.Fatalf("fixture did not admit a separate darwin build: %+v\n%s", mac.delivery, out)
	}
	good := fx.record(t).Good
	if good == nil || good.Entry != mac.delivery.Key {
		t.Fatalf("the record's Good.Entry is %+v, want the darwin build %s", good, mac.delivery.Key)
	}
	if d, out := askForCachedGood(t, fx, "forkpack/tool"); d.Key != linux.delivery.Key {
		t.Errorf("a podman launch's cached good delivered %+v, want its own linux build %s\n%s", d, linux.delivery.Key, out)
	}
	if d, out := askForCachedGoodOn(t, fx, macTestPlatform, "macos-user"); d.Key != mac.delivery.Key {
		t.Errorf("a macos-user launch's cached good delivered %+v, want the darwin build %s\n%s", d, mac.delivery.Key, out)
	}
}
