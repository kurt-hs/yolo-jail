package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// This reaches registration, the independent demand worker, guarded fork pin
// publication and the build slot's actual delivery/refusal, not just the resolver.
func TestFreshPlainBuildSlotUsesIndependentDemandAndReusesExactArtifact(t *testing.T) {
	repo, commit := forkRepo(t)
	first := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	stubSharedPreparationWorker(t)
	var builds atomic.Int32
	entries := []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32}}
	fake := fakeCaptureJail(t, new(run.Options), entries)
	withFakeCaptureJail(t, func(o run.Options) int {
		n := builds.Add(1)
		entries[2].Size = 32 + int64(n)
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		rc := fake(o)
		out := filepath.Join(o.Workspace, captureOutLeaf)
		manifest, err := capture.ReadManifest(out)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Platform = patchedTestPlatform
		if err := capture.WriteManifest(out, manifest); err != nil {
			t.Fatal(err)
		}
		return rc
	})
	old := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
		return forkBuildRunJail(staging, b, s, color), false
	}
	t.Cleanup(func() { forkBuildChild = old })
	slot := func(fresh bool) string {
		pins := run.PinLaunchForks([]packload.Fork{f}, nil)
		req := run.ForkBuildRequest{Pins: pins, FreshUpdate: fresh, Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
		var out syncBuffer
		deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
		d := deliveries[f.Bin]
		if d.Key == "" || d.FreshFailure != "" {
			t.Fatalf("plain public slot refused: %+v\n%s", d, out.String())
		}
		return d.Key
	}
	key := slot(true)
	if builds.Load() != 1 || pinnedCommit(t) != first {
		t.Fatalf("initial fresh: builds %d pin %s", builds.Load(), pinnedCommit(t))
	}
	record, err := patchedAdvanceStore(true).LoadCheckRecord(f.Key())
	if err != nil || record.Fresh == nil || record.Good == nil {
		t.Fatalf("no completed epoch/Good: %+v %v", record, err)
	}
	if slot(true) != key || builds.Load() != 1 {
		t.Fatal("unchanged fresh source forced a rebuild")
	}
	next := commit("second")
	if slot(false) != key || pinnedCommit(t) != first || builds.Load() != 1 {
		t.Fatal("ordinary warm launch moved its standing pin")
	}
	// Ensure receipt ordering does not test an unrelated second-resolution tie-break.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	if slot(true) == key || pinnedCommit(t) != next || builds.Load() != 2 {
		t.Fatalf("new revision not prepared once: pin %s builds %d", pinnedCommit(t), builds.Load())
	}
}

func TestFreshPlainBuildSlotFailedFetchNeverDeliversOldGood(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	freshPlainProgram(t, f)
	// Remove the upstream after pinning. The guarded resolver must refuse even
	// though its mirror and historical standing pin remain available.
	if err := os.Rename(repo, repo+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	stubSharedPreparationWorker(t)
	req := run.ForkBuildRequest{Pins: run.PinLaunchForks([]packload.Fork{f}, nil), FreshUpdate: true, Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
	deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req}, io.Discard, false)
	d := deliveries[f.Bin]
	if d.Key != "" || d.FreshFailure == "" {
		t.Fatalf("failed fresh source delivered historical pin: %+v", d)
	}
	lock, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil || lock.Forks[f.Key()].Commit == "" {
		t.Fatalf("historical pin erased: %+v %v", lock, err)
	}
}

// The delayed request registers through the public slot before the winner starts.
// Only worker dispatch is paused; the winner uses the real plain preparation/build path.
func TestFreshPlainBuildSlotCompletedWinnerRequiresCurrentEpoch(t *testing.T) {
	for _, change := range []string{"deleted", "recreated", "rollback", "inputs", "intact"} {
		t.Run(change, func(t *testing.T) {
			repo, _ := forkRepo(t)
			forkPinHome(t, "git+file://"+repo+"?ref=main")
			f := selectedPlainProgram(t)
			var builds atomic.Int32
			entries := []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32}}
			fake := fakeCaptureJail(t, new(run.Options), entries)
			withFakeCaptureJail(t, func(o run.Options) int {
				builds.Add(1)
				writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
				rc := fake(o)
				out := filepath.Join(o.Workspace, captureOutLeaf)
				m, err := capture.ReadManifest(out)
				if err != nil {
					t.Fatal(err)
				}
				m.Platform = patchedTestPlatform
				if err := capture.WriteManifest(out, m); err != nil {
					t.Fatal(err)
				}
				return rc
			})
			oldChild := forkBuildChild
			forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
				return forkBuildRunJail(staging, b, s, color), false
			}
			t.Cleanup(func() { forkBuildChild = oldChild })
			registered, resume := make(chan preparationDemand, 1), make(chan struct{})
			oldSpawn := sharedPreparationSpawn
			sharedPreparationSpawn = func(ctx context.Context, d preparationDemand) error {
				registered <- d
				<-resume
				return runSharedPreparation(ctx, d, io.Discard)
			}
			t.Cleanup(func() { sharedPreparationSpawn = oldSpawn })
			done := make(chan entrypoint.ForkDelivery, 1)
			go func() {
				req := run.ForkBuildRequest{Pins: run.PinLaunchForks([]packload.Fork{f}, nil), FreshUpdate: true, Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
				d, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req}, io.Discard, false)
				done <- d[f.Bin]
			}()
			var demand preparationDemand
			select {
			case demand = <-registered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not register")
			}
			store := patchedAdvanceStore(true)
			winner, err := runFreshPlainPreparation(context.Background(), f, demand, func(save func() error) error { return save() }, io.Discard)
			if err != nil {
				close(resume)
				<-done
				t.Fatal(err)
			}
			pins, err := os.ReadFile(forkLockPath())
			if err != nil {
				t.Fatal(err)
			}
			record, err := store.LoadCheckRecord(f.Key())
			if err != nil || record.Good == nil {
				t.Fatalf("no winner: %+v %v", record, err)
			}
			starts, err := filepath.Glob(filepath.Join(store.Dir, "checks", "start-*.json"))
			if err != nil || len(starts) != 1 {
				t.Fatalf("start ledger: %v %v", starts, err)
			}
			switch change {
			case "deleted", "recreated":
				if err := os.Remove(starts[0]); err != nil {
					t.Fatal(err)
				}
				if change == "recreated" {
					if _, err := store.CheckBoundary(f.Key()); err != nil {
						t.Fatal(err)
					}
				}
			case "inputs":
				config, err := os.ReadFile(paths.UserConfigPath())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.UserConfigPath(), append(config, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			case "rollback":
				data, err := json.Marshal(demand.CheckBoundary)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(starts[0], data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			close(resume)
			var delivered entrypoint.ForkDelivery
			select {
			case delivered = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("delayed request did not settle")
			}
			if change == "intact" {
				if delivered.Key != winner.delivery.Key || delivered.FreshFailure != "" {
					t.Fatalf("compatible winner not reused: %+v", delivered)
				}
			} else if delivered.Key != "" || delivered.FreshFailure == "" {
				t.Errorf("%s ledger certified historical winner: %+v", change, delivered)
			}
			after, err := os.ReadFile(forkLockPath())
			current, recErr := store.LoadCheckRecord(f.Key())
			if err != nil || recErr != nil || string(after) != string(pins) || !reflect.DeepEqual(current.Good, record.Good) || builds.Load() != 1 {
				t.Fatalf("reuse/refusal changed pin/Good/build count: %v %v builds=%d", err, recErr, builds.Load())
			}
			// Ordinary historical serving is not subject to the new fresh ledger gate.
			ordinary := run.ForkBuildRequest{Pins: run.PinLaunchForks([]packload.Fork{f}, nil), Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
			historical, _ := runBuildSlot(run.BuildSlotRequest{Forks: &ordinary}, io.Discard, false)
			if historical[f.Bin].Key != winner.delivery.Key || builds.Load() != 1 {
				t.Fatalf("ordinary historical serving lost: %+v builds=%d", historical[f.Bin], builds.Load())
			}
		})
	}
}

// ANOTHER PROCESS HOLDING THE DEMAND'S state.lock at a plain fork's fresh admission is contention:
// the worker waits for it and admits, as a patched fork's worker does
// (TestSharedFreshDemandRetriesStateLockAtInitialAndFinalAdmissions), rather than failing the
// `--update` request outright.
func TestFreshPlainDemandRetriesStateLockHeldByAnotherProcess(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	stubSharedPreparationWorker(t)
	entries := []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32}}
	fake := fakeCaptureJail(t, new(run.Options), entries)
	withFakeCaptureJail(t, func(o run.Options) int {
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		rc := fake(o)
		out := filepath.Join(o.Workspace, captureOutLeaf)
		m, err := capture.ReadManifest(out)
		if err != nil {
			t.Fatal(err)
		}
		m.Platform = patchedTestPlatform
		if err := capture.WriteManifest(out, m); err != nil {
			t.Fatal(err)
		}
		return rc
	})
	old := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
		return forkBuildRunJail(staging, b, s, color), false
	}
	t.Cleanup(func() { forkBuildChild = old })

	// The lock is taken through a separate open file description, which flock treats as another
	// holder exactly as it would another process.
	lockReady := make(chan *pidlock.Lock, 1)
	var once atomic.Bool
	previousHook := sharedDemandAdmissionHook
	sharedDemandAdmissionHook = func(d preparationDemand) {
		if d.Key != f.Key() || !d.Fresh || !once.CompareAndSwap(false, true) {
			return
		}
		lock, err := pauseDemandStateLock(t, filepath.Join(sharedDemandDir(d.Key, d.Runtime, d.Platform), "state.lock"))
		if err != nil {
			t.Errorf("pause plain demand authorization: %v", err)
			return
		}
		lockReady <- lock
	}
	t.Cleanup(func() { sharedDemandAdmissionHook = previousHook })

	req := run.ForkBuildRequest{Pins: run.PinLaunchForks([]packload.Fork{f}, nil), FreshUpdate: true, Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
	var out syncBuffer
	done := make(chan entrypoint.ForkDelivery, 1)
	go func() {
		deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
		done <- deliveries[f.Bin]
	}()
	var held *pidlock.Lock
	select {
	case held = <-lockReady:
	case d := <-done:
		t.Fatalf("plain fresh request finished before its admission: %+v\n%s", d, out.String())
	case <-time.After(testsupport.ReadinessBudget(t)):
		t.Fatalf("plain fresh request never reached its admission; output:\n%s", out.String())
	}
	t.Cleanup(func() { held.Release() })
	time.Sleep(3 * sharedDemandPoll)
	select {
	case d := <-done:
		t.Fatalf("plain fresh request settled while another holder had state.lock: %+v\n%s", d, out.String())
	default:
	}
	held.Release()
	select {
	case d := <-done:
		if d.Key == "" || d.FreshFailure != "" {
			t.Fatalf("plain fresh request did not admit after the lock was released: %+v\n%s", d, out.String())
		}
	case <-time.After(testsupport.ReadinessBudget(t)):
		t.Fatalf("plain fresh request did not retry after state.lock was released; output:\n%s", out.String())
	}
}

// freshPlainLaunchResolutionHeld stands in for another process holding the plain fork's pack
// source lock at the LAUNCH's own authorization (resolveDemandFork, reached through
// launchDemandForkResolver): every resolution reports packsrc.ErrLockHeld, as a NoWait mirror try
// does, until release is called. contended is closed at the first held answer.
func freshPlainLaunchResolutionHeld(t *testing.T) (contended <-chan struct{}, release func()) {
	t.Helper()
	first, released := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	previous := launchDemandForkResolver
	launchDemandForkResolver = func(ctx context.Context, d preparationDemand) (packload.Fork, func(), error) {
		select {
		case <-released:
			return previous(ctx, d)
		default:
		}
		once.Do(func() { close(first) })
		return packload.Fork{}, func() {}, fmt.Errorf("packs: forkpack: the pack mirror of fixture is %w", packsrc.ErrLockHeld)
	}
	t.Cleanup(func() { launchDemandForkResolver = previous })
	return first, func() { releaseOnce.Do(func() { close(released) }) }
}

// ANOTHER PROCESS HOLDING THE PACK SOURCE LOCK at a plain fork's fresh LAUNCH request is
// contention, as it is at the shared worker's admission
// (TestFreshPlainDemandRetriesStateLockHeldByAnotherProcess): the launch waits for it and
// prepares, rather than refusing `--update` outright.
func TestFreshPlainLaunchWaitsForPackSourceLockHeldByAnotherProcess(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	stubSharedPreparationWorker(t)
	entries := []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32}}
	fake := fakeCaptureJail(t, new(run.Options), entries)
	withFakeCaptureJail(t, func(o run.Options) int {
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		rc := fake(o)
		out := filepath.Join(o.Workspace, captureOutLeaf)
		m, err := capture.ReadManifest(out)
		if err != nil {
			t.Fatal(err)
		}
		m.Platform = patchedTestPlatform
		if err := capture.WriteManifest(out, m); err != nil {
			t.Fatal(err)
		}
		return rc
	})
	old := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
		return forkBuildRunJail(staging, b, s, color), false
	}
	t.Cleanup(func() { forkBuildChild = old })
	contended, release := freshPlainLaunchResolutionHeld(t)
	t.Cleanup(release)

	req := run.ForkBuildRequest{Pins: run.PinLaunchForks([]packload.Fork{f}, nil), FreshUpdate: true, Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
	var out syncBuffer
	done := make(chan entrypoint.ForkDelivery, 1)
	go func() {
		deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
		done <- deliveries[f.Bin]
	}()
	select {
	case <-contended:
	case d := <-done:
		t.Fatalf("plain fresh launch finished before resolving its pack source: %+v\n%s", d, out.String())
	case <-time.After(testsupport.ReadinessBudget(t)):
		t.Fatalf("plain fresh launch never resolved its pack source; output:\n%s", out.String())
	}
	time.Sleep(3 * sharedDemandPoll)
	select {
	case d := <-done:
		t.Fatalf("plain fresh launch settled while another holder had its pack source lock: %+v\n%s", d, out.String())
	default:
	}
	release()
	select {
	case d := <-done:
		if d.Key == "" || d.FreshFailure != "" {
			t.Fatalf("plain fresh launch did not prepare after the lock was released: %+v\n%s", d, out.String())
		}
	case <-time.After(testsupport.ReadinessBudget(t)):
		t.Fatalf("plain fresh launch did not retry after the lock was released; output:\n%s", out.String())
	}
}

// A HOLD THAT OUTLASTS THE BOUND refuses the fresh launch request, and the refusal names the
// holder and the next step: rerun `--update` once that process finishes.
func TestFreshPlainLaunchPackSourceLockPastBoundNamesTheRetry(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	stubSharedPreparationWorker(t)
	previousBound := freshPlainResolutionWait
	freshPlainResolutionWait = 3 * sharedDemandPoll
	t.Cleanup(func() { freshPlainResolutionWait = previousBound })
	_, release := freshPlainLaunchResolutionHeld(t)
	t.Cleanup(release)

	req := run.ForkBuildRequest{Pins: run.PinLaunchForks([]packload.Fork{f}, nil), FreshUpdate: true, Runtime: "podman", Platform: patchedTestPlatform, Interrupt: &run.ActInterrupt{}}
	var out syncBuffer
	deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
	d := deliveries[f.Bin]
	if d.Key != "" || d.FreshFailure == "" {
		t.Fatalf("a pack source held past the bound did not refuse: %+v\n%s", d, out.String())
	}
	for _, want := range []string{f.Label(), "held by another yolo process", "retry with `yolo --update -- <command>` once that process finishes"} {
		if !strings.Contains(d.FreshFailure, want) {
			t.Errorf("refusal %q does not name %q", d.FreshFailure, want)
		}
	}
}
