package cli

import (
	"context"
	"fmt"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestTask2ProducerCancellationWithSecondFreshWaiter(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	first, _, _ := fx.launch(t, "podman")
	if first.failed {
		t.Fatalf("seed: %+v", first)
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 20: "twenty"})
	f := fx.fork(t)
	after, err := patchedAdvanceStore(true).FreshCheckBoundary(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := freshPreparationIdentity(f, patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	childCancelled := make(chan struct{}, 1)
	var calls atomic.Int32
	previous := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-ctx.Done():
				childCancelled <- struct{}{}
				return 130, false
			case <-release:
			}
		}
		return previous(ctx, bound, staging, b, s, color)
	}
	defer func() { forkBuildChild = previous }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var secondOut syncBuffer
	options := func(ctx context.Context, out io.Writer) advanceOptions {
		return advanceOptions{platform: patchedTestPlatform, runtime: "podman", launch: true, ctx: ctx, out: out, errw: out, freshCheck: true, freshIdentity: identity, freshAfterSeq: after}
	}
	firstDone, secondDone := make(chan advanceResult, 1), make(chan advanceResult, 1)
	go func() { firstDone <- advancePatchedFork(f, options(ctx, io.Discard)) }()
	<-entered
	go func() { secondDone <- advancePatchedFork(f, options(context.Background(), &secondOut)) }()
	waitFor(t, "second fresh requester waiting on owned production", func() bool { return strings.Contains(secondOut.String(), "waiting for pid") })
	cancel()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Error("cancelled waiter did not detach promptly")
	}
	select {
	case <-childCancelled:
		t.Error("producer context cancelled shared build while second fresh waiter remained")
	default:
	}
	close(release)
	second := <-secondDone
	if second.failed {
		t.Errorf("second demand failed: %+v\n%s", second, secondOut.String())
	}
	if calls.Load() != 1 {
		t.Errorf("cancelled shared producer was replaced by %d child builds", calls.Load())
	}
}

// Unit fixtures use the exact worker and lease protocol, with a separately owned worker context.
// Process tests below use self-exec instead and prove that this seam is not lifetime authority.
func stubSharedPreparationWorker(t *testing.T) {
	t.Helper()
	previous := sharedPreparationSpawn
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	sharedPreparationSpawn = func(_ context.Context, d preparationDemand) error {
		wg.Add(1)
		go func() { defer wg.Done(); _ = runSharedPreparation(ctx, d, io.Discard) }()
		return nil
	}
	t.Cleanup(func() { cancel(); wg.Wait(); sharedPreparationSpawn = previous })
}

func TestSharedDemandSchedulerStopDetachesWhileFreshTreeRequestRemains(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var calls atomic.Int32
	pf.child = func(ctx context.Context, b forkBuild) int {
		if b.Fork.Key() != poolKeyA {
			return 0
		}
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-ctx.Done():
				cancelled <- struct{}{}
				return 130
			case <-release:
			}
		}
		return 0
	}
	_, _ = pf.launch(t, "podman", nil)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ticks := make(chan time.Time)
	schedulerDone := make(chan error, 1)
	go func() { schedulerDone <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timer did not start owned production")
	}
	f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
	req := run.TreeBuildRequest{Trees: []packload.Fork{f}, Build: true, Runtime: "podman", Platform: patchedTestPlatform, FreshUpdate: true, CopyRoot: filepath.Join(t.TempDir(), "tree.patched")}
	var output syncBuffer
	freshDone := make(chan map[string]run.TreeDelivery, 1)
	go func() {
		_, trees := runBuildSlot(run.BuildSlotRequest{Trees: &req}, &output, false)
		freshDone <- trees
	}()
	waitFor(t, "fresh tree requester attached", func() bool {
		dir := sharedDemandDir(poolKeyA, "podman", patchedTestPlatform)
		var live []livePreparationDemand
		err := withDemandState(context.Background(), dir, false, func() error { var e error; live, e = livePreparationDemands(dir); return e })
		return err == nil && len(live) == 2
	})
	stop()
	select {
	case <-schedulerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler tried to own another requester's production")
	}
	select {
	case <-cancelled:
		t.Fatal("scheduler exit killed production still owned by fresh demand")
	default:
	}
	close(release)
	select {
	case result := <-freshDone:
		if result[poolKeyA].Dir == "" || result[poolKeyA].FreshFailure != "" {
			t.Fatalf("fresh tree failed: %+v\n%s", result, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fresh tree request did not finish")
	}
	if calls.Load() != 1 {
		t.Fatalf("timer/fresh demand rebuilt %d children", calls.Load())
	}
}

func TestSharedDemandWorkspaceOnlyFreshBuildSlotKeepsForegroundAuthority(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%v", cancelled), func(t *testing.T) {
			fx := newPatchedAdvanceFixture(t, "")
			fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
			seeded, _, _ := fx.launch(t, "podman")
			if seeded.failed {
				t.Fatal("seed failed")
			}
			fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 20: "twenty"})
			req := forkRequest(t, fx, "podman")
			req.FreshUpdate = true
			// This declaration is still in the launch's selected vector, as a workspace-only pack is,
			// but is no longer authorized by the independent detached user resolver.
			writeFile(t, filepath.Join(fx.home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[]}`)
			sharedPreparationSpawn = func(context.Context, preparationDemand) error {
				t.Error("workspace manifest reached detached execution")
				return errBackgroundFromTest
			}
			entered := make(chan struct{})
			originalChild := forkBuildChild
			if cancelled {
				forkBuildChild = func(ctx context.Context, _ time.Duration, _ string, _ forkBuild, _ captureStreams, _ bool) (int, bool) {
					close(entered)
					<-ctx.Done()
					return 130, false
				}
			}
			defer func() { forkBuildChild = originalChild }()
			var output syncBuffer
			done := make(chan map[string]entrypoint.ForkDelivery, 1)
			go func() { result, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req}, &output, false); done <- result }()
			if cancelled {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("foreground child did not start")
				}
				if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case result := <-done:
				if cancelled {
					if result["tool"].FreshFailure == "" || result["tool"].Key != "" {
						t.Fatalf("cancelled foreground request claimed freshness: %+v", result)
					}
				} else {
					if result["tool"].Key == "" || result["tool"].FreshFailure != "" || result["tool"].Key == seeded.delivery.Key {
						t.Fatalf("workspace foreground route failed: %+v\n%s", result, output.String())
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("foreground-only preparation did not complete")
			}
			if !strings.Contains(output.String(), "foreground-only preparation") {
				t.Fatalf("foreground limitation was not disclosed: %s", output.String())
			}
		})
	}
}

func TestSharedDemandUnknownLeaseKeepsChildButRefusesAdmission(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	seeded, _, _ := fx.launch(t, "podman")
	if seeded.failed {
		t.Fatal("seed failed")
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 20: "twenty"})
	f := fx.fork(t)
	identity, err := freshPreparationIdentity(f, patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	after, err := patchedAdvanceStore(true).FreshCheckBoundary(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	dir := sharedDemandDir(f.Key(), "podman", patchedTestPlatform)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "request-unknown.json")
	lease, err := pidlock.Acquire(path+".lease", pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	d := preparationDemand{Key: f.Key(), Runtime: "podman", Platform: patchedTestPlatform, Identity: identity, Fresh: true, AfterSeq: after}
	if err = writeDemandJSON(path, d); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	previousChild := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
		close(entered)
		<-release
		if ctx.Err() != nil {
			t.Error("unknown demand liveness was treated as zero demand")
		}
		return previousChild(ctx, bound, staging, b, s, color)
	}
	defer func() { forkBuildChild = previousChild }()
	done := make(chan error, 1)
	go func() { done <- runSharedPreparation(context.Background(), d, io.Discard) }()
	<-entered
	if err = os.Rename(path+".lease", path+".lease-held"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path+".lease", 0700); err != nil {
		t.Fatal(err)
	}
	// The guard observes the unreadable lease even if the periodic watcher has not ticked.
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unknown lease dispatch/publication was not refused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish unknown-liveness act")
	}
	if got := fx.record(t).Good.Entry; got != seeded.delivery.Key {
		t.Fatalf("unknown demand admitted %s", got)
	}
	if _, err = os.Stat(path + ".result"); !os.IsNotExist(err) {
		t.Fatalf("unknown demand got a published result: %v", err)
	}
}

func TestSharedDemandSleepingSchedulerLeaseKeepsProducerAfterLastLaunchDetaches(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	_, _ = pf.launch(t, "podman", nil)
	timer, err := pidlock.Acquire(preparationWorkerLockPath(), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer timer.Release()
	if err = withPreparationState(func(state *preparationState) error {
		state.Worker = preparationWorker{PID: os.Getpid(), Identity: "owned-test-timer"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	pf.child = func(ctx context.Context, b forkBuild) int {
		if b.Fork.Key() == poolKeyA {
			close(entered)
			select {
			case <-ctx.Done():
				cancelled <- struct{}{}
				return 130
			case <-release:
			}
		}
		return 0
	}
	f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
	identity, err := freshPreparationIdentity(f, patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	after, err := patchedAdvanceStore(true).FreshCheckBoundary(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	ctx, detach := context.WithCancel(context.Background())
	defer detach()
	var output syncBuffer
	done := make(chan advanceResult, 1)
	go func() {
		done <- advancePatchedFork(f, advanceOptions{launch: true, runtime: "podman", platform: patchedTestPlatform, ctx: ctx, out: &output, errw: &output, freshCheck: true, freshIdentity: identity, freshAfterSeq: after})
	}()
	<-entered
	detach()
	if result := <-done; !result.failed {
		t.Fatal("detached launch claimed freshness")
	}
	// No launch remains; the positive scheduler lease and current authorized subscription suffice.
	automatic := preparationDemand{Key: f.Key(), Runtime: "podman", Platform: patchedTestPlatform, Identity: identity}
	if record, err := sharedTimerDemand(context.Background(), automatic); err != nil || record == nil {
		t.Fatalf("timer demand not witnessed: %+v %v", record, err)
	}
	close(release)
	dir := sharedDemandDir(f.Key(), "podman", patchedTestPlatform)
	waitFor(t, "timer-owned producer settled", func() bool { return !pidlock.Held(filepath.Join(dir, "worker.lock")) })
	select {
	case <-cancelled:
		t.Fatal("last launch detach killed timer-owned production")
	default:
	}
	record, err := patchedAdvanceStore(true).LoadCheckRecord(f.Key())
	if err != nil || record.Good == nil || record.Good.Commit != upstreamGit(t, pf.repo, "rev-parse", "HEAD") {
		t.Fatalf("timer-owned admission: good=%+v want=%s err=%v", record.Good, upstreamGit(t, pf.repo, "rev-parse", "HEAD"), err)
	}
}

func TestSharedDemandCancelledRequesterDetachesDuringKeyedBuildLockWait(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	seeded, _, _ := fx.launch(t, "podman")
	if seeded.failed {
		t.Fatal("seed failed")
	}
	tip := fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 20: "twenty"})
	f := fx.fork(t)
	identity, err := freshPreparationIdentity(f, patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	after, err := patchedAdvanceStore(true).FreshCheckBoundary(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	build := forkBuild{Fork: f, Commit: tip, Platform: patchedTestPlatform, Series: mustSeries(t, fx)}
	lock, err := pidlock.Acquire(build.lockPath(), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	var workerOutput syncBuffer
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	workerDone := make(chan error, 1)
	sharedPreparationSpawn = func(_ context.Context, d preparationDemand) error {
		go func() { workerDone <- runSharedPreparation(workerCtx, d, &workerOutput) }()
		return nil
	}
	ctx, detach := context.WithCancel(context.Background())
	defer detach()
	done := make(chan advanceResult, 1)
	go func() {
		done <- advancePatchedFork(f, advanceOptions{launch: true, runtime: "podman", platform: patchedTestPlatform, ctx: ctx, out: io.Discard, errw: io.Discard, freshCheck: true, freshIdentity: identity, freshAfterSeq: after})
	}()
	waitFor(t, "owned worker waiting on build lock", func() bool { return strings.Contains(workerOutput.String(), "waiting for pid") })
	detach()
	select {
	case result := <-done:
		if !result.failed || result.delivery.Key != "" {
			t.Fatalf("cancelled lock waiter claimed freshness: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("keyed lock blocked requester detach")
	}
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("zero-demand did not cancel owned keyed lock wait")
	}
	if got := fx.record(t).Good.Entry; got != seeded.delivery.Key {
		t.Fatalf("cancelled keyed lock wait changed admission: %s", got)
	}
	if len(fx.builds) != 1 {
		t.Fatalf("cancelled lock wait started %d builds", len(fx.builds))
	}
}

func TestSharedDemandHostFreshCheckKeepsUsableFloorWithoutRebuildingMissingStoreCopy(t *testing.T) {
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("seed rc=%d\n%s", rc, out)
	}
	good := fx.record(t).Good.Entry
	removeStoreEntry(t, good)
	before := fx.record(t).StartedSeq
	builds := len(fx.builds)
	got := captureHostExec(t)
	var output syncBuffer
	if rc := hostMain([]string{"--update", "--", "tool"}, io.Discard, &output, false, nil); rc != 0 || got.target == "" {
		t.Fatalf("fresh installed-copy route rc=%d target=%s\n%s", rc, got.target, output.String())
	}
	if record := fx.record(t); record.StartedSeq <= before || record.Good.Entry != good || len(fx.builds) != builds {
		t.Fatalf("freshness rebuilt unchanged installed bytes: seq=%d before=%d builds=%d before=%d good=%+v", record.StartedSeq, before, len(fx.builds), builds, record.Good)
	}
}
