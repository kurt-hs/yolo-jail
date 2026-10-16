package cli

// These public build-slot regressions retain typed Apple admission failures through
// the independent O requester/worker result. Exact reuse must bypass liveness queries;
// an explicit fresh request must not turn a pending build into historical success.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
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

// runAppleFreshSlot reaches the production explicit-fresh slot and returns its
// typed delivery evidence, not a test callback or a reconstructed string error.
func runAppleFreshSlot(t *testing.T, pins []packload.ForkPin, rt, platform string) (appleFreshSlotAnswer, error) {
	t.Helper()
	spyCalled := make(chan error, 1)
	slotDone := make(chan appleFreshSlotAnswer, 1)
	go func() {
		req := run.ForkBuildRequest{FreshUpdate: true, Pins: pins, Platform: platform, Runtime: rt,
			Interrupt: &run.ActInterrupt{}}

		forks, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, io.Discard, false)
		var ans appleFreshSlotAnswer
		for _, p := range pins {
			if d, ok := forks[p.Fork.Bin]; ok {
				ans = appleFreshSlotAnswer{key: d.Key, reason: d.Reason}
				if d.FreshFailure != "" {
					select {
					case spyCalled <- d.FreshError:
					default:
					}
				}
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
		{"busy", "ID IMAGE OS ARCH STATE\nyolo-other img linux arm64 running\n", nil, "running yolo jail(s): yolo-other"},
		{"unknown", "", errors.New("container ls failed"), "could not establish Apple Container liveness"},
	} {
		t.Run(liveness.name, func(t *testing.T) {
			fx, _, _, good, _, _ := firstAdvance(t)
			fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
			fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 25: "new release"})
			f := fx.fork(t)
			stubSpawn(t)
			stubAppleListing(t, liveness.listing, liveness.listErr)
			before := fx.record(t)
			pins := []packload.ForkPin{{Fork: f}}
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
			after := fx.record(t)
			failures := func(r *packsrc.CheckRecord) []packsrc.EntryOutcome {
				var failed []packsrc.EntryOutcome
				for _, o := range r.Outcomes {
					if o.Kind == packsrc.OutcomeBuildFailed {
						failed = append(failed, o)
					}
				}
				return failed
			}
			if !reflect.DeepEqual(failures(before), failures(after)) {
				t.Fatalf("pending admission changed failure outcomes: before %+v after %+v", before.Outcomes, after.Outcomes)
			}
			if snapshot, err := readPreparationSnapshot(f.Key()); err != nil || !snapshot.RetryAt.IsZero() {
				t.Fatalf("pending admission scheduled retry backoff: %+v %v", snapshot, err)
			}
			if g := fx.record(t).Good; g == nil || g.Entry != good.delivery.Key || !storeEntryExists(g.Entry) {
				t.Fatalf("refused fresh lost the good build: %+v", fx.record(t).Good)
			}
			stubAppleListing(t, "ID IMAGE OS ARCH STATE\n", nil)
			retry, err := runAppleFreshSlot(t, pins, "container", patchedTestPlatform)
			if err != nil || retry.key == "" || retry.key == good.delivery.Key || fx.child != 1 {
				t.Fatalf("same candidate could not build immediately: %+v err=%v children=%d", retry, err, fx.child)
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
	stubAppleListing(t, "ID IMAGE OS ARCH STATE\nyolo-other img linux arm64 running\n", nil)
	pins := []packload.ForkPin{{Fork: f}}
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
		{"busy", "ID IMAGE OS ARCH STATE\nyolo-other img linux arm64 running\n", nil, "running yolo jail(s): yolo-other"},
		{"unknown", "", errors.New("container ls failed"), "could not establish Apple Container liveness"},
	} {
		t.Run(liveness.name, func(t *testing.T) {
			repo, commit := forkRepo(t)
			forkPinHome(t, "git+file://"+repo+"?ref=main")
			stubSharedPreparationWorker(t)
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
	stubAppleListing(t, "ID IMAGE OS ARCH STATE\nyolo-other img linux arm64 running\n", nil)
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
	// The bound shortened, not removed: the fake runtime sleeps 30 s, so a listing with no
	// bound still fails the 10 s check below.
	old := appleListingBound
	appleListingBound = 100 * time.Millisecond
	t.Cleanup(func() { appleListingBound = old })
	start := time.Now()
	err := appleRequiredBuildAdmission(context.Background(), "container", "pack/tool")
	if !isAppleBuildPending(err) || time.Since(start) > 10*time.Second {
		t.Fatalf("query not bounded: %v after %s", err, time.Since(start))
	}
}

func TestAppleAdmissionCancelsActiveListing(t *testing.T) {
	f := forkBuildHome(t)
	children := 0
	withFakeCaptureJail(t, func(run.Options) int { children++; return 0 })
	started, stopped := make(chan struct{}), make(chan struct{})
	old := applePreparationListing
	applePreparationListing = func(ctx context.Context) (string, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return "", ctx.Err()
	}
	defer func() { applePreparationListing = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := buildFork(forkBuild{Fork: f, Commit: forkTestCommit, Platform: "linux/arm64"}, buildMode{runtime: "container", admitCtx: ctx, lock: pidlock.NoWait}, io.Discard, io.Discard, false)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("listing did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("operation cancellation did not cancel runtime listing")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("listing remained alive")
	}
	if children != 0 {
		t.Fatalf("canceled admission started %d children", children)
	}
}

func TestAppleFreshWaiterPreservesIndependentWorkerPending(t *testing.T) {
	fx, _, _, good, _, _ := firstAdvance(t)
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 25: "new release"})
	f := fx.fork(t)
	stubSpawn(t)
	stubSharedPreparationWorker(t)
	stubAppleListing(t, "ID IMAGE OS ARCH STATE\nyolo-other img linux arm64 running\n", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := newPreparationRequest(ctx, []packload.Fork{f}, "container", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	release, err := registerPreparationDemand(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = waitFreshPreparation(ctx, req, f.Key())
	var pending *appleBuildPending
	if !errors.As(err, &pending) || pending.Capture != f.CaptureArg() || !strings.Contains(pending.Reason, "yolo-other") {
		t.Fatalf("independent fresh waiter erased typed pending: %v", err)
	}
	if fx.child != 0 || fx.record(t).Good.Entry != good.delivery.Key {
		t.Fatal("pending fresh mutated admission or started child")
	}
}
