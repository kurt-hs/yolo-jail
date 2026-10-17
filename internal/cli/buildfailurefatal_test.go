package cli

// buildfailurefatal_test.go drives OQ-PPX3's ruling (docs/reference/pi-extensions.md, 2026-10-16)
// through the advance's real call sites: a NEWER BUILD THAT FAILS while a good build of the series as
// it stands serves STOPS the act, with the one refusal shape (ERROR, facts, Repair:, Start anyway:),
// and no automatic fallback to the older build. Only a hatch the user set — YOLO_USE_CACHED_GOOD,
// YOLO_ALLOW_PATCH_FAILURES, YOLO_ALLOW_OLDER_UPSTREAM — or an `agent_updates` hold (OQ-PBR5) runs
// the older build.

import (
	"bytes"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
)

// failedNewerBuild is the state every test here starts from: a good build at v1.1.0, then v1.3.0,
// which the series takes but which does not build, and the launch that met that failure.
func failedNewerBuild(t *testing.T) (*patchedAdvanceFixture, advanceResult, advanceResult, string, string) {
	t.Helper()
	for _, env := range patchrecovery.Scoped {
		t.Setenv(env, "")
	}
	fx, _, _, good, _, _ := firstAdvance(t)
	v13 := fx.commit(t, "v1.3.0", fitting)
	fx.rc = 2
	fx.later(2 * time.Hour)
	failed, out, _ := fx.launch(t, "podman")
	return fx, good, failed, out, v13
}

// A FRESH LAUNCH REFUSES: the build of the newer upstream failed, so the launch stops, handing the
// typed failure beside the good build it would take only under a hatch, with the error block and
// the hatches that apply; inside the back-off the next launch refuses again with no build; a hatch
// runs the good build; past the back-off the build is tried again and moves.
func TestANewerBuildThatFailsRefusesTheLaunch(t *testing.T) {
	fx, good, failed, out, v13 := failedNewerBuild(t)
	pf := failed.delivery.PatchFailure
	if !failed.failed || pf == nil || pf.Kind != packsrc.PatchFailureBuild || pf.Target.Commit != v13 ||
		failed.delivery.Key != good.delivery.Key || len(fx.builds) != 2 {
		t.Fatalf("the failed newer build did not refuse with the good build retained: %+v, builds=%d\n%s",
			failed, len(fx.builds), out)
	}
	for _, w := range []string{
		"ERROR: forkpack/tool: the build failed at upstream v1.3.0 (" + shortSHA(v13) + ")",
		"\nRepair:\n", "    yolo capture tool\n",
		"\nStart anyway:\n",
		"YOLO_ALLOW_PATCH_FAILURES=forkpack/tool yolo",
		"YOLO_USE_CACHED_GOOD=forkpack/tool yolo",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("the refusal lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "still running v1.1.0") || strings.Contains(out, "to stay on the running version") {
		t.Errorf("the refusal still says the older build runs:\n%s", out)
	}
	if got, want := hatchEnvs(failed.delivery.Recovery), []string{"YOLO_ALLOW_PATCH_FAILURES=forkpack/tool",
		"YOLO_USE_CACHED_GOOD=forkpack/tool", "YOLO_SKIP_PATCHES=forkpack/tool"}; !slices.Equal(got, want) {
		t.Errorf("the hatch list = %v, want %v", got, want)
	}
	if fx.record(t).PatchFailure != nil {
		t.Error("a build failure was written to the record as a patch failure")
	}

	fx.later(2 * time.Hour)
	again, out, _ := fx.launch(t, "podman")
	if !again.failed || again.delivery.PatchFailure == nil || again.delivery.PatchFailure.Kind != packsrc.PatchFailureBuild ||
		len(fx.builds) != 2 {
		t.Fatalf("inside the back-off the launch did not refuse without building: %+v, builds=%d\n%s",
			again, len(fx.builds), out)
	}

	t.Setenv(patchrecovery.AllowPatchFailuresEnv, "forkpack/tool")
	continued, out, _ := fx.launch(t, "podman")
	if continued.failed || continued.delivery.Key != good.delivery.Key || len(fx.builds) != 2 ||
		!strings.Contains(out, "Continuing with v1.1.0") {
		t.Fatalf("the hatch did not run the good build: %+v\n%s", continued, out)
	}
	t.Setenv(patchrecovery.AllowPatchFailuresEnv, "")

	fx.rc = 0
	fx.later(48 * time.Hour)
	moved, out, _ := fx.launch(t, "podman")
	if !moved.built || moved.failed || len(fx.builds) != 3 || moved.delivery.Key == good.delivery.Key {
		t.Errorf("past the back-off the build was not tried again and moved: %+v, builds=%d\n%s",
			moved, len(fx.builds), out)
	}
}

// A NEWER RELEASE PASSES THE FAILURE BY, as it does a patch failure: inside the failed build's
// back-off, the check that lists a newer release builds it and moves, with no refusal.
func TestANewerReleasePassesAFailedBuildBy(t *testing.T) {
	fx, good, _, _, _ := failedNewerBuild(t)
	fx.commit(t, "v1.4.0", map[int]string{14: "fourteen", 20: "twenty", 22: "twenty-two"})
	fx.rc = 0
	fx.later(2 * time.Hour)
	moved, out, _ := fx.launch(t, "podman")
	if !moved.built || moved.failed || moved.delivery.PatchFailure != nil || moved.delivery.Key == good.delivery.Key ||
		fx.record(t).Good.Tag != "v1.4.0" {
		t.Fatalf("the newer release was not built past the failure: %+v\n%s", moved, out)
	}
}

// YOLO_ALLOW_OLDER_UPSTREAM IS LISTED, AND GOES BACK, when a release between the good build and the
// failed newest is left to try: without it the launch refuses, listing it; with it the walk-back
// builds that release and says so.
func TestTheWalkBackGoesBackPastAFailedNewerBuild(t *testing.T) {
	for _, env := range patchrecovery.Scoped {
		t.Setenv(env, "")
	}
	fx, _, _, _, _, _ := firstAdvance(t)
	fx.commit(t, "v1.2.0", fitting)
	fx.commit(t, "v1.3.0", map[int]string{14: "fourteen", 20: "twenty", 22: "thirteen"})
	fx.failBuildsOf(t, "thirteen")
	fx.later(2 * time.Hour)
	refused, out, _ := fx.launch(t, "podman")
	if !refused.failed || refused.delivery.PatchFailure == nil || refused.delivery.PatchFailure.Kind != packsrc.PatchFailureBuild ||
		!slices.Contains(hatchEnvs(refused.delivery.Recovery), "YOLO_ALLOW_OLDER_UPSTREAM=forkpack/tool") {
		t.Fatalf("the failed newest did not refuse listing the walk-back: %+v\n%s", refused, out)
	}
	t.Setenv(patchrecovery.AllowOlderUpstreamEnv, "forkpack/tool")
	walked, out, _ := fx.launch(t, "podman")
	if !walked.built || walked.failed || walked.delivery.PatchFailure != nil || fx.record(t).Good.Tag != "v1.2.0" ||
		!strings.Contains(out, "walked back to v1.2.0") {
		t.Fatalf("the walk-back did not build v1.2.0: %+v\n%s", walked, out)
	}
}

// IN A JAIL LAUNCH'S SLOT the act hands the typed failure beside the good build and prints no block
// of its own: the launch prints it once (run's refusePatchFailures), then refuses.
func TestANewerBuildThatFailsIsHandedToTheLaunchsRefusal(t *testing.T) {
	fx, good, _, _, _ := failedNewerBuild(t)
	fx.later(2 * time.Hour)
	slotted, term := fx.launchReported(t)
	if slotted.delivery.Key != good.delivery.Key || slotted.delivery.PatchFailure == nil ||
		slotted.delivery.PatchFailure.Kind != packsrc.PatchFailureBuild || slotted.delivery.Runs == "" ||
		strings.Contains(term, "ERROR: ") {
		t.Errorf("the slot's act handed %+v, or printed the launch's block:\n%s", slotted.delivery, term)
	}
}

// THROUGH THE LAUNCH'S BUILD SLOT (runBuildSlot, the fork arm a fresh jail launch runs): without a
// hatch the slot hands the failure beside the good build, which the launch refuses; with
// YOLO_USE_CACHED_GOOD naming the fork the slot hands the good build with no check and no build.
func TestTheBuildSlotHandsAFailedNewerBuildAndCachedGoodRunsTheGoodBuild(t *testing.T) {
	fx, good, _, _, _ := failedNewerBuild(t)
	fx.later(2 * time.Hour)
	plain, out := askForCachedGood(t, fx, "")
	if plain.PatchFailure == nil || plain.PatchFailure.Kind != packsrc.PatchFailureBuild || plain.Key != good.delivery.Key ||
		len(fx.builds) != 2 {
		t.Fatalf("the slot handed %+v after %d builds, want the build failure beside the good build\n%s",
			plain, len(fx.builds), out)
	}
	cached, out := askForCachedGood(t, fx, "forkpack/tool")
	if cached.Key != good.delivery.Key || cached.PatchFailure != nil || len(fx.builds) != 2 ||
		!strings.Contains(out, "Current build failure remains recorded") {
		t.Fatalf("cached-good recovery handed %+v after %d builds, want the good build\n%s", cached, len(fx.builds), out)
	}
}

// A HOLD HOLDS (OQ-PBR5): under `agent_updates` off the good build runs past the recorded failure,
// with no refusal and no build.
func TestAHoldRunsTheGoodBuildPastAFailedNewerBuild(t *testing.T) {
	fx, good, _, _, _ := failedNewerBuild(t)
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	fx.later(2 * time.Hour)
	held, out, _ := fx.launch(t, "podman")
	if held.failed || held.delivery.PatchFailure != nil || held.delivery.Key != good.delivery.Key || len(fx.builds) != 2 {
		t.Fatalf("the hold refused on a failed newer build: %+v\n%s", held, out)
	}
	if suffix := heldSuffix(t, fx); !strings.Contains(suffix, "the build of upstream v1.3.0") {
		t.Errorf("the held suffix does not name the failed build: %q", suffix)
	}
}

// A PATCHED EXTENSION'S FAILED NEWER BUILD (OQ-PBR6: forks and extensions alike), through the
// launch's tree arm: the arm hands the failure beside the good build's per-launch copy, which the
// launch refuses; inside the back-off it does so again with no build, in the at-launch mode and in
// the next-launch mode, whose admitted read no longer hands the older copy silently.
func TestAnExtensionsFailedNewerBuildIsHandedToTheLaunchsRefusal(t *testing.T) {
	for _, env := range patchrecovery.Scoped {
		t.Setenv(env, "")
	}
	fx := newTreeFixture(t, `"f.txt"`)
	good, out := fx.deliver(t, true)
	if good.Dir == "" {
		t.Fatalf("no first build: %+v\n%s", good, out)
	}
	fx.commit(t, "v1.3.0", fitting)
	builds := 0
	withFakeCaptureJail(t, func(o run.Options) int {
		builds++
		// The build line ran (its toolchain witness), and failed.
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		return 2
	})
	fx.setNow(fx.clock().Add(2 * time.Hour))
	failed, out := fx.deliver(t, true)
	if failed.PatchFailure == nil || failed.PatchFailure.Kind != packsrc.PatchFailureBuild || failed.Dir == "" ||
		failed.Entry != good.Entry || builds != 1 {
		t.Fatalf("the failed newer build handed %+v after %d builds, want the failure beside the good build's copy\n%s",
			failed, builds, out)
	}
	if got := hatchEnvs(failed.Recovery); !slices.Contains(got, "YOLO_ALLOW_PATCH_FAILURES="+treeKeyCLI) ||
		!slices.Contains(got, "YOLO_USE_CACHED_GOOD="+treeKeyCLI) {
		t.Errorf("the hatch list = %v", got)
	}

	fx.setNow(fx.clock().Add(2 * time.Hour))
	again, out := fx.deliver(t, true)
	if again.PatchFailure == nil || again.PatchFailure.Kind != packsrc.PatchFailureBuild || builds != 1 {
		t.Fatalf("inside the back-off the arm handed %+v after %d builds\n%s", again, builds, out)
	}

	prevTiming := treeUpdateTiming
	treeUpdateTiming = func(packload.Fork) updateTiming { return timingNextLaunch }
	t.Cleanup(func() { treeUpdateTiming = prevTiming })
	stubSpawn(t)
	next, out := fx.deliver(t, true)
	if next.PatchFailure == nil || next.PatchFailure.Kind != packsrc.PatchFailureBuild || builds != 1 {
		t.Fatalf("the next-launch mode handed %+v after %d builds, want the failure\n%s", next, builds, out)
	}
}

// `yolo pack status` SAYS THE FAILED NEWER BUILD STOPS A LAUNCH, and the one action that runs the
// good build meanwhile, rather than calling the candidate pending.
func TestPackStatusSaysAFailedNewerBuildStopsTheLaunch(t *testing.T) {
	failedNewerBuild(t)
	rc, status, errw := packVerb(t, "status")
	if rc != 0 {
		t.Fatalf("status rc=%d\n%s\n%s", rc, status, errw)
	}
	for _, w := range []string{"its build failed: the build line ran and failed; its build jail exited 2",
		"a fresh launch refuses until a build of it succeeds",
		"`yolo capture tool` retries it now", "YOLO_USE_CACHED_GOOD=forkpack/tool yolo"} {
		if !strings.Contains(status, w) {
			t.Errorf("status lacks %q:\n%s", w, status)
		}
	}
	if strings.Contains(status, patchedNotBuilt) {
		t.Errorf("status calls the failed candidate unbuilt:\n%s", status)
	}
}

// failTreeBuilds makes every later tree build's line run and fail, counting them.
func failTreeBuilds(t *testing.T) *int {
	t.Helper()
	n := 0
	withFakeCaptureJail(t, func(o run.Options) int {
		n++
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		return 2
	})
	return &n
}

// BACKGROUND PREPARATION RECORDS AND REPORTS, AND THE NEXT LAUNCH DOES NOT SERVE THE OLDER BUILD
// SILENTLY: a background advance whose build failed leaves the failure on the record, and the next
// fresh launch in the next-launch mode hands it beside the good build's copy, which refuses.
func TestABackgroundBuildFailureStopsTheNextLaunch(t *testing.T) {
	for _, env := range patchrecovery.Scoped {
		t.Setenv(env, "")
	}
	pf := readyForABackgroundAdvance(t)
	builds := failTreeBuilds(t)
	if rc := runBackgroundAdvance(backgroundAdvanceArgv([]string{poolKeyA}, "podman", patchedTestPlatform)[2:]); rc != 0 {
		t.Fatalf("the background advance returned %d", rc)
	}
	if *builds != 1 {
		t.Fatalf("the background advance ran %d builds, want its one failed build", *builds)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	stubSpawn(t)
	got, out := pf.launch(t, "podman", &run.ActInterrupt{})
	d := got[poolKeyA]
	if d.PatchFailure == nil || d.PatchFailure.Kind != packsrc.PatchFailureBuild || *builds != 1 {
		t.Fatalf("the next launch handed %+v after %d builds, want the background's failure\n%s", d, *builds, out)
	}
	// Its refusal lists the hatches this launch reads — cached-good recovery among them — and not the
	// walk-back, which a read that builds nothing would not run.
	if got := hatchEnvs(d.Recovery); !slices.Contains(got, "YOLO_USE_CACHED_GOOD="+poolKeyA) ||
		!slices.Contains(got, "YOLO_ALLOW_PATCH_FAILURES="+poolKeyA) ||
		slices.Contains(got, "YOLO_ALLOW_OLDER_UPSTREAM="+poolKeyA) {
		t.Errorf("the next launch's hatch list = %v", got)
	}
}

// AT THE HOST, AN EXTENSION'S FAILED NEWER BUILD STOPS `yolo host -- <bin>`: under
// host_apply_on_launch its advance before the gate refuses, and without it the gate reads the
// recorded failure; neither starts the program on the linked older build, and the hatch does.
func TestAHostLaunchStopsOnAnExtensionsFailedNewerBuild(t *testing.T) {
	for _, env := range patchrecovery.Scoped {
		t.Setenv(env, "")
	}
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true`)
	stubBins(t, "tool")
	got := captureHostExec(t)
	var errw bytes.Buffer
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc != 0 || !got.execed {
		t.Fatalf("the first launch: rc=%d execed %v\n%s", rc, got.execed, errw.String())
	}
	fx.commit(t, "v1.3.0", fitting)
	builds := failTreeBuilds(t)
	fx.setNow(fx.now.Add(2 * time.Hour))
	got.execed = false
	errw.Reset()
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc == 0 || got.execed || *builds != 1 {
		t.Fatalf("a failed newer build: rc=%d execed %v builds %d, want a refusal\n%s", rc, got.execed, *builds, errw.String())
	}
	for _, w := range []string{"treepack/tool-ext: the build failed at upstream v1.3.0",
		"\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES=treepack/tool-ext yolo host -- tool ",
		"yolo host: refusing: the newer build of extension treepack/tool-ext failed (the ERROR above); nothing was written."} {
		if !strings.Contains(errw.String(), w) {
			t.Errorf("the refusal lacks %q:\n%s", w, errw.String())
		}
	}

	// WITHOUT host_apply_on_launch the gate reads the recorded failure, inside its back-off.
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":false`)
	errw.Reset()
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc == 0 || got.execed || *builds != 1 ||
		!strings.Contains(errw.String(), "extension treepack/tool-ext: the build failed at upstream v1.3.0") {
		t.Fatalf("the gate over a recorded failed newer build: rc=%d execed %v builds %d\n%s", rc, got.execed, *builds,
			errw.String())
	}
	t.Setenv(patchrecovery.AllowPatchFailuresEnv, "treepack/tool-ext")
	errw.Reset()
	if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc != 0 || !got.execed ||
		!strings.Contains(errw.String(), "Continuing with ") {
		t.Errorf("the hatch: rc=%d execed %v\n%s", rc, got.execed, errw.String())
	}
}

// `yolo host apply --assert` STOPS BEFORE ITS FIRST WRITE on a patched fork's recorded failed newer
// build, read with no advance (hostPatchPreflight), naming the build and not a series.
func TestAHostApplyStopsOnAForksFailedNewerBuildBeforeItWrites(t *testing.T) {
	for _, env := range patchrecovery.Scoped {
		t.Setenv(env, "")
	}
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	fx.writeUserConfig(t, `,"host_management":"own"`)
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("the first launch: rc=%d\n%s", rc, out)
	}
	fx.commit(t, "v1.3.0", fitting)
	fx.rc = 2
	fx.later(2 * time.Hour)
	if rc, _, out := fx.hostLaunch(t); rc == 0 || !strings.Contains(out, "the build failed at upstream v1.3.0") {
		t.Fatalf("the launch that records the failure: rc=%d\n%s", rc, out)
	}
	builds := len(fx.builds)
	var out, errw bytes.Buffer
	rc := hostApplyRefreshAndRender(&out, &errw, false, true, nil, "", "")
	if rc == 0 || strings.Contains(out.String(), "host apply — applying into") || len(fx.builds) != builds ||
		!strings.Contains(errw.String(), "ERROR: fork forkpack/tool: the build failed at upstream v1.3.0") ||
		!strings.Contains(errw.String(), "\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES=forkpack/tool yolo host apply --assert ") ||
		!strings.Contains(errw.String(), "yolo host: refusing the host apply: the newer build of fork forkpack/tool failed") {
		t.Errorf("an apply over a recorded failed newer build: rc=%d, want a refusal before its render\n%s\n%s",
			rc, out.String(), errw.String())
	}
}
