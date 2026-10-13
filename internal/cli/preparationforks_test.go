package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func preparationForkRequest(f packload.Fork, commit, runtime string) run.ForkBuildRequest {
	return run.ForkBuildRequest{Pins: []packload.ForkPin{{Fork: f, Commit: commit, Ref: "main"}},
		Platform: patchedTestPlatform, Runtime: runtime, Workspace: "/ws", Interrupt: &run.ActInterrupt{}}
}

func setForkNextLaunch(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(paths.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.TrimSuffix(strings.TrimSpace(string(data)), "}") + `,"agent_updates":{"forkpack":"next-launch"}}`
	if err := os.WriteFile(paths.UserConfigPath(), []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitPreparation(t *testing.T, key string, ready func(preparationSnapshot) bool) preparationSnapshot {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		snapshot, err := readPreparationSnapshot(key)
		if err == nil && ready(snapshot) {
			return snapshot
		}
		select {
		case <-deadline:
			t.Fatalf("preparation %s did not reach expected state; last snapshot %+v", key, snapshot)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestPlainForkPreparationTimerFillsMissingArtifactAtStandingPin(t *testing.T) {
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)

	var builds atomic.Int32
	previousChild := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, streams captureStreams,
		color bool) (int, bool) {
		builds.Add(1)
		return forkBuildRunJail(staging, b, streams, color), false
	}
	t.Cleanup(func() { forkBuildChild = previousChild })
	var seen run.Options
	withFakeCaptureJail(t, fakeBuildJail(t, &seen, probetoolBuilt))

	beforePin, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	pinned, ok := beforePin.Get(f.Key())
	if !ok || pinned.Commit != forkTestCommit {
		t.Fatalf("fixture pin = %+v, want the recorded standing pin %s", pinned, forkTestCommit)
	}
	var launchLog strings.Builder
	req := preparationForkRequest(f, pinned.Commit, "podman")
	req.Platform = "linux/arm64"
	deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &launchLog, false)
	if deliveries[f.Bin].Key == "" || builds.Load() != 1 {
		t.Fatalf("cold launch did not build its required pinned program once: deliveries=%+v builds=%d\n%s",
			deliveries, builds.Load(), launchLog.String())
	}
	if entry, _, err := resolveForkBuild(&capture.Store{Dir: paths.CapturesDir()}, f.Bin, req.Platform,
		f.Source, pinned.Commit, forkBuild{Fork: f, Commit: pinned.Commit, Platform: req.Platform}.recipe()); err != nil || entry.Key != deliveries[f.Bin].Key {
		t.Fatalf("launch artifact did not resolve through the regular pinned-program selector: entry=%+v err=%v delivery=%+v",
			entry, err, deliveries[f.Bin])
	}
	if snapshot, err := readPreparationSnapshot(f.Key()); err != nil || snapshot.Generation == 0 || snapshot.InputIdentity == "" {
		t.Fatalf("runBuildSlot did not register the selected plain fork: %+v err=%v", snapshot, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- time.Now()
	waitPreparation(t, f.Key(), func(snapshot preparationSnapshot) bool {
		return snapshot.Phase == "unchanged" && snapshot.ServingEntry == deliveries[f.Bin].Key
	})
	if builds.Load() != 1 {
		t.Fatalf("an unchanged admitted artifact was rebuilt: %d builds", builds.Load())
	}

	store := &capture.Store{Dir: paths.CapturesDir()}
	if err := os.RemoveAll(store.EntryDir(deliveries[f.Bin].Key)); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now().Add(time.Second)
	waitPreparation(t, f.Key(), func(snapshot preparationSnapshot) bool {
		return snapshot.Phase == "admitted" && snapshot.ServingEntry != ""
	})
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Fatalf("timer did not fill exactly one missing pinned artifact: builds=%d", builds.Load())
	}
	afterPin, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	after, ok := afterPin.Get(f.Key())
	if !ok || after.Commit != pinned.Commit || after.Source != pinned.Source {
		t.Fatalf("timer moved the standing pin: before=%+v after=%+v", pinned, after)
	}
}

func TestPreparationForkIdentityIncludesPinAndRecipe(t *testing.T) {
	f := packload.Fork{Pack: "forkpack", Base: "basepack", Bin: "tool", Source: forkTestSource,
		Build: "make install", Produces: []string{".local/bin/tool"}}
	first, err := preparationForkIdentity(f, forkTestCommit, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := preparationForkIdentity(f, strings.Repeat("a", 40), "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	f.Build = "make install-v2"
	changedRecipe, err := preparationForkIdentity(f, forkTestCommit, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if first == moved || first == changedRecipe {
		t.Fatalf("fork identity did not cover pin/recipe: first=%s moved=%s recipe=%s", first, moved, changedRecipe)
	}
}

func TestPreparationAdmissionGuardRejectsRevokedOrEditedFork(t *testing.T) {
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)
	pin := packload.ForkPin{Fork: f, Commit: forkTestCommit, Ref: "main"}
	req := preparationForkRequest(f, pin.Commit, "podman")
	if err := registerForkPreparation([]packload.ForkPin{pin}, req, io.Discard); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readPreparationSnapshot(f.Key())
	if err != nil || snapshot.Generation == 0 {
		t.Fatalf("fork was not registered: %+v err=%v", snapshot, err)
	}

	guard := preparationAdmissionGuard(f.Key(), "podman", patchedTestPlatform, snapshot.Generation)
	committed := false
	if err := guard(func() error { committed = true; return nil }); err != nil || !committed {
		t.Fatalf("unchanged selected inputs failed admission: committed=%v err=%v", committed, err)
	}

	manifest := filepath.Join(f.Root, "pack.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"build":"make install"`, `"build":"make changed"`, 1)
	if edited == string(data) {
		t.Fatalf("fixture manifest did not contain the expected build recipe: %s", data)
	}
	if err := os.WriteFile(manifest, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	committed = false
	if err := guard(func() error { committed = true; return nil }); !errors.Is(err, errPreparationAdmissionStale) || committed {
		t.Fatalf("stale fork recipe admitted: committed=%v err=%v", committed, err)
	}
}

func TestPreparationAdmissionGuardRejectsManualForkPinMovement(t *testing.T) {
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)
	pin := packload.ForkPin{Fork: f, Commit: forkTestCommit, Ref: "main"}
	req := preparationForkRequest(f, pin.Commit, "podman")
	if err := registerForkPreparation([]packload.ForkPin{pin}, req, io.Discard); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readPreparationSnapshot(f.Key())
	if err != nil {
		t.Fatal(err)
	}

	lock, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	manualCommit := strings.Repeat("b", 40)
	lock.Set(packsrc.ForkLockEntry{Key: f.Key(), Source: f.Source, Ref: "main", Commit: manualCommit})
	if err := lock.Save(forkLockPath()); err != nil {
		t.Fatal(err)
	}
	committed := false
	guard := preparationAdmissionGuard(f.Key(), "podman", patchedTestPlatform, snapshot.Generation)
	if err := guard(func() error { committed = true; return nil }); !errors.Is(err, errPreparationAdmissionStale) || committed {
		t.Fatalf("an old pin's candidate survived manual pin movement: committed=%v err=%v", committed, err)
	}
	current, err := currentForkPin(f)
	if err != nil || current.Commit != manualCommit {
		t.Fatalf("timer changed or ignored the manually recorded pin: pin=%+v err=%v", current, err)
	}
}
