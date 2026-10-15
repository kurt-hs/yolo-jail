package cli

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Production command, authenticated control, O timer dispatcher and independent
// child demand all participate. Deleting the command's lifecycle/dispatch wiring
// leaves this test without the initial child or the proven stop/drain response.
func TestPreparationSchedulerCommandAuthenticatedReplacementDrainsOriginalChild(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	_, _ = pf.launch(t, "podman", &run.ActInterrupt{})
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var once sync.Once
	pf.child = func(ctx context.Context, b forkBuild) int {
		if b.Fork.Key() == poolKeyA {
			once.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
				cancelled <- struct{}{}
				return 130
			case <-release:
			}
		}
		return 0
	}
	commandDone := make(chan int, 1)
	go func() { commandDone <- runPreparationSchedulerCommand() }()
	owner := awaitPreparationOwner(t, preparationRoot())
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = controlPreparationWorker(ctx, preparationRoot(), owner.Identity, "stop")
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("production command never dispatched initial work")
	}
	if err := controlPreparationWorker(context.Background(), preparationRoot(), owner.Identity, "replace"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
		t.Fatal("replacement cancelled original child instead of draining")
	case <-time.After(100 * time.Millisecond):
	}
	if _, live, err := probePreparationWorker(preparationRoot()); err != nil || live != preparationAlive {
		t.Fatalf("draining owner lost authenticated lease: %s %v", live, err)
	}
	close(release)
	select {
	case rc := <-commandDone:
		if rc != 0 {
			t.Fatalf("replacement drain rc=%d", rc)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drained scheduler failed to release original lease")
	}
	if _, live, err := probePreparationWorker(preparationRoot()); err != nil || live != preparationGone {
		t.Fatalf("original owner not proven gone: %s %v", live, err)
	}
	state, err := loadPreparationState()
	if err != nil || state.Worker.Identity != owner.Identity || state.Worker.StoppedAt.IsZero() {
		t.Fatalf("incarnation final state lost: %+v %v", state.Worker, err)
	}
}

func TestPreparationSchedulerUnknownSelectionRetainsButNeverAdmits(t *testing.T) {
	fx, _, _, good, _, _ := firstAdvance(t)
	f := fx.fork(t)
	identity, err := freshPreparationIdentity(f, patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previous := sharedPreparationSpawn
	sharedPreparationSpawn = func(context.Context, preparationDemand) error { return nil }
	t.Cleanup(func() { sharedPreparationSpawn = previous })
	d := preparationDemand{Key: f.Key(), Runtime: "podman", Platform: patchedTestPlatform, Identity: identity}
	request, err := registerSharedDemand(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	defer request.release(false)
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	// Malformed selection is unknown authority, not proof that an owner revoked it.
	writeFile(t, paths.UserConfigPath(), `{"packs":[`)
	_, err = authorizedPreparationDemands(ctx, request.dir, d.Identity)
	if err == nil || errors.Is(err, errPreparationAdmissionStale) {
		t.Fatalf("unknown selection classified as revocation: %v", err)
	}
	admitted := false
	if err := demandAdmissionGuard(ctx, request.dir, d, new(sync.Mutex))(func() error { admitted = true; return nil }); err == nil || admitted {
		t.Fatalf("unknown authority admitted: %v %v", admitted, err)
	}
	if fx.record(t).Good.Entry != good.delivery.Key {
		t.Fatal("unknown selection changed Good")
	}
}
