package cli

// preparationapplefresh_test.go drives the Apple explicit-fresh hook through its
// production callsites: an explicit fresh launch (`-u`/`--update`) on the container
// runtime whose liveness is busy or unknown must FAIL with the required-build
// admission refusal — typed, so run.Options.refuseFreshFailure refuses the launch —
// and never report a stale success. The ordinary launch keeps serving its good
// build; only the explicit request is refused.
//
// THE WIRING UNDER TEST: buildForkUnderLock's admission gate
// (appleRequiredBuildAdmission, after the exact-reuse lookup) refuses the build
// with *appleBuildPending; advance.settle reports it through
// advanceOptions.requiredBuildFailure; runFreshPreparationDemands preserves it
// across the worker/launch boundary in the preparation answer; waitFreshPreparation
// rebuilds the typed refusal; and the production callsites' req.FreshFailure
// (runBuildSlot's plain-fork and patched-fork fresh paths, `yolo host --update`'s
// waiter) receives it. Delete any link and a test below goes red: without the
// gate the build child runs, without the hook or the answer marker the waiter
// returns an untyped error.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// appleFreshSlotAnswer is one runBuildSlot fresh key's delivery, without naming
// its map type.
type appleFreshSlotAnswer struct {
	key    string
	reason string
}

// stubAppleListing stands in for the `container ls` query this test's liveness
// case names. Every stubber restores the production query, so the bounded-query
// test below always measures the real one.
func stubAppleListing(t *testing.T, listing string, listErr error) {
	t.Helper()
	old := applePreparationListing
	applePreparationListing = func(context.Context) (string, error) { return listing, listErr }
	t.Cleanup(func() { applePreparationListing = old })
}

// driveAppleScheduler runs the production preparation scheduler until cancel,
// ticking whenever it is ready, so an explicit demand a slot registers is served
// in-process. The cleanup drains the scheduler on every exit path.
func driveAppleScheduler(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ticks <- time.Now():
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
}

// ensureAppleScheduler registers an explicit demand before the scheduler
// starts and waits until the scheduler owns its lease. Registration probes no
// worker when none runs, so this order — demand first, scheduler second, slot
// last — never races scheduler startup: a slot registering later finds the
// worker alive and takes the wake path instead of failing the spawn probe.
func ensureAppleScheduler(t *testing.T, pins []packload.ForkPin, rt, platform string) {
	t.Helper()
	var forks []packload.Fork
	for _, p := range pins {
		forks = append(forks, p.Fork)
	}
	req, err := newPreparationRequest(context.Background(), forks, rt, platform)
	if err != nil {
		t.Fatal(err)
	}
	release, err := registerPreparationDemand(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release() })
	driveAppleScheduler(t)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(preparationRoot(), "worker.json")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler never owned its lease")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runAppleFreshSlot is the production explicit-fresh callsite
// (runBuildSlot's fresh path): it registers the demand, waits for the
// scheduler's answer, and reports failures through FreshFailure exactly as a
// launch does. It returns the delivery and what FreshFailure received, nil when
// the request qualified. FreshFailure fires before the key's answer is
// recorded, so once the slot answers a missing delivery implies the spy fired.
func runAppleFreshSlot(t *testing.T, pins []packload.ForkPin, rt, platform string) (appleFreshSlotAnswer, error) {
	t.Helper()
	spyCalled := make(chan error, 1)
	slotDone := make(chan appleFreshSlotAnswer, 1)
	go func() {
		req := run.ForkBuildRequest{FreshCheck: true, Pins: pins, Platform: platform, Runtime: rt,
			Interrupt: &run.ActInterrupt{},
			FreshFailure: func(err error) {
				select {
				case spyCalled <- err:
				default:
				}
			}}
		forks, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, io.Discard, false)
		var ans appleFreshSlotAnswer
		for _, p := range pins {
			if d, ok := forks[p.Fork.Bin]; ok {
				ans = appleFreshSlotAnswer{key: d.Key, reason: d.Reason}
			}
		}
		slotDone <- ans
	}()
	var ans appleFreshSlotAnswer
	select {
	case ans = <-slotDone:
	case <-time.After(60 * time.Second):
		t.Fatal("explicit fresh slot never answered")
	}
	if ans.key != "" {
		select {
		case spyErr := <-spyCalled:
			t.Fatalf("qualified fresh invoked FreshFailure: %v", spyErr)
		default:
		}
		return ans, nil
	}
	select {
	case spyErr := <-spyCalled:
		return ans, spyErr
	default:
		t.Fatal("failed fresh never reached FreshFailure")
		return ans, nil
	}
}

// An explicit fresh patched advance on a busy or unknown container runtime fails
// typed: FreshFailure receives the admission refusal, no build child runs, the
// good build is preserved, and nothing is delivered.
func TestAppleExplicitFreshPendingFailsTyped(t *testing.T) {
	for _, liveness := range []struct {
		name       string
		listing    string
		listErr    error
		wantReason string
	}{
		{"busy", "ID IMAGE STATE\nother img running\n", nil, "a conflicting container is running (other)"},
		{"unknown", "", errors.New("container ls failed"), "runtime liveness is unknown"},
	} {
		t.Run(liveness.name, func(t *testing.T) {
			fx, _, _, good, _, _ := firstAdvance(t)
			fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
			fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 25: "new release"})
			f := fx.fork(t)
			stubSpawn(t)
			stubAppleListing(t, liveness.listing, liveness.listErr)
			pins := []packload.ForkPin{{Fork: f}}
			ensureAppleScheduler(t, pins, "container", patchedTestPlatform)
			ans, spyErr := runAppleFreshSlot(t, pins, "container", patchedTestPlatform)
			if !isAppleBuildPending(spyErr) {
				t.Fatalf("FreshFailure did not receive the admission refusal: %v", spyErr)
			}
			if !strings.Contains(spyErr.Error(), liveness.wantReason) {
				t.Fatalf("refusal lost its pending reason: %v", spyErr)
			}
			if ans.key != "" {
				t.Fatalf("refused fresh reported a stale success: %+v", ans)
			}
			if fx.child != 0 {
				t.Fatalf("refused fresh started a build child: %d", fx.child)
			}
			if g := fx.record(t).Good; g == nil || g.Entry != good.delivery.Key || !storeEntryExists(g.Entry) {
				t.Fatalf("refused fresh lost the good build: %+v", fx.record(t).Good)
			}
		})
	}
}

// An explicit fresh request with nothing pending qualifies without touching
// the hook: the good build delivers and FreshFailure is never invoked.
func TestAppleExplicitFreshExactReuseDoesNotInvokeHook(t *testing.T) {
	fx, _, _, good, _, _ := firstAdvance(t)
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	f := fx.fork(t)
	stubSpawn(t)
	stubAppleListing(t, "ID IMAGE STATE\nother img running\n", nil)
	pins := []packload.ForkPin{{Fork: f}}
	ensureAppleScheduler(t, pins, "container", patchedTestPlatform)
	ans, spyErr := runAppleFreshSlot(t, pins, "container", patchedTestPlatform)
	if spyErr != nil {
		t.Fatalf("exact reuse invoked FreshFailure: %v", spyErr)
	}
	if ans.key != good.delivery.Key {
		t.Fatalf("exact reuse did not deliver the good build: %+v want %s", ans, good.delivery.Key)
	}
}

// An explicit fresh plain program on a busy or unknown container runtime fails
// typed as well: the plain resolver reaches the same gate through buildFork,
// and the answer preserves it the same way. Phase one admits the revision on
// podman; phase two refuses its successor on container without starting a jail.
func TestAppleExplicitFreshPlainFailsTyped(t *testing.T) {
	for _, liveness := range []struct {
		name       string
		listing    string
		listErr    error
		wantReason string
	}{
		{"busy", "ID IMAGE STATE\nother img running\n", nil, "a conflicting container is running (other)"},
		{"unknown", "", errors.New("container ls failed"), "runtime liveness is unknown"},
	} {
		t.Run(liveness.name, func(t *testing.T) {
			repo, commit := forkRepo(t)
			forkPinHome(t, "git+file://"+repo+"?ref=main")
			f := selectedPlainProgram(t)
			entries := []capture.ManifestEntry{
				{Path: ".local", Kind: capture.KindDir, Mode: "0755"},
				{Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"},
				{Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32},
			}
			count := 0
			fake := fakeCaptureJail(t, new(run.Options), entries)
			withFakeCaptureJail(t, func(o run.Options) int {
				count++
				writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
				return fake(o)
			})
			stubSpawn(t)
			pins := []packload.ForkPin{{Fork: f}}
			ensureAppleScheduler(t, pins, "podman", "linux/arm64")
			admitted, refused := runAppleFreshSlot(t, pins, "podman", "linux/arm64")
			if refused != nil || admitted.key == "" {
				t.Fatalf("podman fresh did not admit: %+v %v", admitted, refused)
			}
			if count != 1 {
				t.Fatalf("podman fresh built %d jails, want 1", count)
			}
			commit("second")
			stubAppleListing(t, liveness.listing, liveness.listErr)
			ans, spyErr := runAppleFreshSlot(t, []packload.ForkPin{{Fork: f}}, "container", "linux/arm64")
			if !isAppleBuildPending(spyErr) {
				t.Fatalf("plain FreshFailure did not receive the admission refusal: %v", spyErr)
			}
			if !strings.Contains(spyErr.Error(), liveness.wantReason) {
				t.Fatalf("plain refusal lost its pending reason: %v", spyErr)
			}
			if ans.key != "" {
				t.Fatalf("refused plain fresh reported a stale success: %+v", ans)
			}
			if count != 1 {
				t.Fatalf("refused plain fresh started a build jail: %d", count)
			}
			rec, err := patchedAdvanceStore(true).LoadCheckRecord(f.Key())
			if err != nil || rec.Good == nil || rec.Good.Entry != admitted.key || !storeEntryExists(rec.Good.Entry) {
				t.Fatalf("refused plain fresh lost its admission: %+v %v", rec.Good, err)
			}
		})
	}
}

// The gate sits AFTER the exact-reuse lookup: a cold container build with a
// conflicting jail is refused without starting one, and a build the store
// already holds reuses it without consulting the runtime at all.
func TestAppleBuildGateAfterExactReuse(t *testing.T) {
	f := forkBuildHome(t)
	var seen run.Options
	withFakeCaptureJail(t, fakeBuildJail(t, &seen, probetoolBuilt))
	b := forkBuild{Fork: f, Commit: forkTestCommit, Platform: "linux/arm64"}
	queries := 0
	stubAppleListing(t, "ID IMAGE STATE\nother img running\n", nil)
	old := applePreparationListing
	applePreparationListing = func(context.Context) (string, error) {
		queries++
		return old(context.Background())
	}
	if entry, err := buildFork(b, buildMode{runtime: "container", lock: pidlock.NoWait}, io.Discard, io.Discard, false); entry != nil || !isAppleBuildPending(err) {
		t.Fatalf("cold container build was not refused: %v %v", entry, err)
	}
	if seen.Workspace != "" {
		t.Fatal("refused cold build started a jail")
	}
	if queries != 1 {
		t.Fatalf("cold container build consulted the runtime %d times, want 1", queries)
	}
	entry, err := buildFork(b, buildMode{runtime: "podman", lock: pidlock.NoWait}, io.Discard, io.Discard, false)
	if err != nil {
		t.Fatal(err)
	}
	if queries != 1 {
		t.Fatalf("podman build consulted the container runtime: %d queries", queries)
	}
	reused, err := buildFork(b, buildMode{runtime: "container", lock: pidlock.NoWait}, io.Discard, io.Discard, false)
	if err != nil || reused.Key != entry.Key || queries != 1 {
		t.Fatalf("exact reuse consulted the runtime or failed: %v %v queries=%d", reused, err, queries)
	}
}

// The admission query stays bounded against a misbehaving runtime: a stalled
// `container` CLI cannot park an explicit fresh request past the query's bound.
func TestAppleListingQueryIsBounded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "container"), "#!/bin/sh\nexec sleep 30\n")
	if err := os.Chmod(filepath.Join(dir, "container"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	start := time.Now()
	err := appleRequiredBuildAdmission(context.Background(), "container", "pack/tool")
	if !isAppleBuildPending(err) || time.Since(start) > 10*time.Second {
		t.Fatalf("query not bounded: %v after %s", err, time.Since(start))
	}
}
