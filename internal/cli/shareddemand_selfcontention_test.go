package cli

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// A worker's own demand monitor is not contention. The monitor polls state.lock for the whole of
// the work, and an admission that met that poll used to be refused as "lock held": here, a fresh
// plain fork's request failed outright (CI's flaky TestAppleExplicitFreshPlainFailsTyped), and a
// background advance went busy until the next tick. The monitor is held inside state.lock exactly
// when the first admission arrives, so the overlap is not left to timing.
func TestSharedDemandWorkersOwnMonitorDoesNotRefuseItsAdmission(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	stubSharedPreparationWorker(t)
	f := selectedPlainProgram(t)
	entries := []capture.ManifestEntry{
		{Path: ".local", Kind: capture.KindDir, Mode: "0755"},
		{Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"},
		{Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32},
	}
	fake := fakeCaptureJail(t, new(run.Options), entries)
	withFakeCaptureJail(t, func(o run.Options) int {
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		return fake(o)
	})
	stubSpawn(t)

	monitorHolding := make(chan struct{})
	var monitorOnce, admissionOnce sync.Once
	budget := testsupport.ReadinessBudget(t)
	previousMonitor, previousAdmission := sharedDemandMonitorHook, sharedDemandAdmissionHook
	sharedDemandMonitorHook = func() {
		monitorOnce.Do(func() {
			close(monitorHolding)
			// Still inside state.lock while the admission that was waiting for this proceeds.
			time.Sleep(4 * sharedDemandPoll)
		})
	}
	sharedDemandAdmissionHook = func(d preparationDemand) {
		if d.Key != f.Key() {
			return
		}
		admissionOnce.Do(func() {
			select {
			case <-monitorHolding:
			case <-time.After(budget):
				t.Error("the worker's demand monitor never polled state.lock")
			}
		})
	}
	t.Cleanup(func() { sharedDemandMonitorHook, sharedDemandAdmissionHook = previousMonitor, previousAdmission })

	admitted, refused := runAppleFreshSlot(t, []packload.ForkPin{{Fork: f}}, "podman", "linux/arm64")
	if refused != nil || admitted.key == "" {
		t.Fatalf("a fresh plain request was refused by its own worker's monitor: %+v %v", admitted, refused)
	}
}
