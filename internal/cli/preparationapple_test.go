package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

func fakeAppleListing(t *testing.T, out string, err error) *int {
	t.Helper()
	old := applePreparationListing
	calls := new(int)
	applePreparationListing = func(ctx context.Context) (string, error) {
		*calls++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
			t.Error("unbounded runtime query")
		}
		return out, err
	}
	t.Cleanup(func() { applePreparationListing = old })
	return calls
}

func appleTimerPass(t *testing.T, now time.Time) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- now
	deadline := time.After(10 * time.Second)
	for {
		subs, err := readPreparationSubscriptions(preparationRoot())
		if err != nil {
			t.Fatal(err)
		}
		complete := len(subs) > 0
		for _, sub := range subs {
			snapshot, err := readPreparationSnapshotFor(preparationRoot(), sub)
			complete = complete && err == nil && snapshot.Phase != bgRunning && snapshot.WorkerIdentity != ""
		}
		if complete {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("Apple scheduler never published completed check")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPreparationAppleRegistrationChecksWithoutBuildThenConsumesPending(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	spawn := stubSpawn(t)
	old := outcomeOfRecord(t, poolKeyA)
	builds := pf.countBuilds()
	calls := fakeAppleListing(t, "ID IMAGE OS ARCH STATE ADDR\n", nil)
	pf.launch(t, "container", &run.ActInterrupt{})
	if spawn.count() != 1 || builds.Load() != 0 {
		t.Fatal("launch did not register check-only preparation")
	}
	appleTimerPass(t, pf.now)
	if builds.Load() != 0 || *calls != 0 {
		t.Fatalf("check-only dispatch built or queried build admission: children=%d queries=%d", builds.Load(), *calls)
	}
	if outcomeOfRecord(t, poolKeyA) != old {
		t.Fatal("timer replaced good build")
	}
	f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
	a, early := newAdvance(f, advanceOptions{platform: patchedTestPlatform, launch: true, out: io.Discard, errw: io.Discard})
	if early != nil || !a.pendingBuild() {
		t.Fatal("background check lost pending candidate")
	}
	got, _ := pf.launch(t, "container", &run.ActInterrupt{})
	if got[poolKeyA].Entry == old || builds.Load() != 2 || *calls != 2 {
		t.Fatalf("foreground did not consume pending: children=%d queries=%d", builds.Load(), *calls)
	}
}

func TestPreparationAppleBlockedBuildKeepsGoodAndReportsFreshFailure(t *testing.T) {
	for _, tc := range []struct {
		name, out string
		err       error
	}{
		{name: "busy", out: "ID IMAGE STATE\nother img running\n"},
		{name: "query-failed", err: errors.New("runtime unavailable")},
		{name: "empty-success"},
		{name: "unknown-state", out: "ID IMAGE STATE\nother img unexpected\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pf := readyForABackgroundAdvance(t)
			pf.agentUpdates(t, `{"treepool":"next-launch"}`)
			stubSpawn(t)
			old := outcomeOfRecord(t, poolKeyA)
			builds := pf.countBuilds()
			fakeAppleListing(t, tc.out, tc.err)
			pf.launch(t, "container", &run.ActInterrupt{})
			appleTimerPass(t, pf.now)
			got, out := pf.launch(t, "container", &run.ActInterrupt{})
			if got[poolKeyA].Entry != old || builds.Load() != 0 || !strings.Contains(out, "build pending") {
				t.Fatalf("blocked required build did not retain Good: %s", out)
			}
			f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
			var freshErr error
			advancePatchedFork(f, advanceOptions{platform: patchedTestPlatform, runtime: "container", launch: true, out: io.Discard, errw: io.Discard, requiredBuildFailure: func(err error) { freshErr = err }})
			if !isAppleBuildPending(freshErr) {
				t.Fatalf("explicit freshness silently succeeded: %v", freshErr)
			}
			if outcomeOfRecord(t, poolKeyA) != old {
				t.Fatal("blocked admission moved Good")
			}
		})
	}
}

func TestPreparationAppleQueryBoundUsesRealCommand(t *testing.T) {
	old := applePreparationListing
	// Keep the real production query, with a deliberately stalled local runtime.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "container"), "#!/bin/sh\nexec sleep 30\n")
	if err := os.Chmod(filepath.Join(dir, "container"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	start := time.Now()
	err := appleRequiredBuildAdmission(context.Background(), "container", "pack/tool")
	if !isAppleBuildPending(err) || time.Since(start) > 4*time.Second {
		t.Fatalf("query not bounded: %v after %s", err, time.Since(start))
	}
	applePreparationListing = old
}

func TestPreparationAppleCapabilitiesDoNotRelabelDarwin(t *testing.T) {
	for _, rt := range []string{"container", "podman", "macos-user", "unknown"} {
		for _, platform := range []string{"linux/arm64", "darwin/arm64", "invalid"} {
			want := (rt == "container" || rt == "podman") && platform == "linux/arm64"
			if run.BackgroundChecksOn(rt, platform) != want {
				t.Fatalf("capability widened for %s/%s", rt, platform)
			}
		}
	}
	if run.BackgroundBuildsOn("container") {
		t.Fatal("Apple cannot background-build")
	}
}

func TestPreparationAppleLateRevocationAndStaleGenerationCannotAdmit(t *testing.T) {
	for _, change := range []string{"revoke", "generation"} {
		t.Run(change, func(t *testing.T) {
			pf := readyForABackgroundAdvance(t)
			pf.agentUpdates(t, `{"treepool":"next-launch"}`)
			stubSpawn(t)
			oldGood := outcomeOfRecord(t, poolKeyA)
			builds := pf.countBuilds()
			queries := fakeAppleListing(t, "ID IMAGE STATE\n", nil)
			pf.launch(t, "container", &run.ActInterrupt{})
			oldHook := backgroundArtifactTestHook
			changed := make(chan struct{})
			var once sync.Once
			backgroundArtifactTestHook = func(phase, key string) {
				if phase != "repair.observe" || key != poolKeyA {
					return
				}
				once.Do(func() {
					if change == "revoke" {
						pf.agentUpdates(t, `false`)
					} else {
						subs, err := readPreparationSubscriptions(preparationRoot())
						if err != nil {
							t.Error(err)
							return
						}
						for _, sub := range subs {
							if sub.Key == poolKeyA {
								sub.InputIdentity = "new-generation"
								if _, err := registerPreparationSubscriptions(preparationRoot(), []preparationSubscription{sub}); err != nil {
									t.Error(err)
								}
							}
						}
					}
					close(changed)
				})
			}
			t.Cleanup(func() { backgroundArtifactTestHook = oldHook })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ticks := make(chan time.Time)
			done := make(chan error, 1)
			go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
			ticks <- pf.now
			select {
			case <-changed:
			case <-time.After(10 * time.Second):
				cancel()
				<-done
				t.Fatal("scheduler did not reach replay authority seam")
			}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				outcome, err := readBackgroundOutcome(backgroundOutcomePath(poolKeyA))
				if err == nil && outcome.State != bgRunning {
					break
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if builds.Load() != 0 || *queries != 0 || outcomeOfRecord(t, poolKeyA) != oldGood {
				t.Fatal("late authority change admitted a build")
			}
		})
	}
}

func TestPreparationApplePlainRequiredBuildGateAndExactReuse(t *testing.T) {
	f := forkBuildHome(t)
	var seen run.Options
	withFakeCaptureJail(t, fakeBuildJail(t, &seen, probetoolBuilt))
	// The fixture jail's manifest is linux/arm64's (fakeCaptureJail), so the build must ask
	// for that platform or the exact-reuse lookup below misses on every other machine.
	b := forkBuild{Fork: f, Commit: forkTestCommit, Platform: "linux/arm64"}
	queries := fakeAppleListing(t, "ID IMAGE STATE\nother img running\n", nil)
	if entry, err := buildFork(b, buildMode{runtime: "container", lock: pidlock.NoWait}, io.Discard, io.Discard, false); entry != nil || !isAppleBuildPending(err) {
		t.Fatalf("cold plain required build was not blocked: %v %v", entry, err)
	}
	if seen.Workspace != "" {
		t.Fatal("blocked cold build started a jail")
	}
	entry, err := buildFork(b, buildMode{runtime: "podman", lock: pidlock.NoWait}, io.Discard, io.Discard, false)
	if err != nil {
		t.Fatal(err)
	}
	before := *queries
	reused, err := buildFork(b, buildMode{runtime: "container", lock: pidlock.NoWait}, io.Discard, io.Discard, false)
	if err != nil || reused.Key != entry.Key || *queries != before {
		t.Fatalf("exact reuse queried runtime or failed: %v %v", reused, err)
	}
}
