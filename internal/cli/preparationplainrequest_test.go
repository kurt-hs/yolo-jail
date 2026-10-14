package cli

import (
	"context"
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
