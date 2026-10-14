package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

func preparationFixture(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "scheduler")
	exe := filepath.Join(t.TempDir(), "yolo")
	if err := os.WriteFile(exe, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, exe
}
func awaitPreparationOwner(t *testing.T, root string) preparationWorker {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, s, _ := probePreparationWorker(root)
		if s == preparationAlive {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("worker not ready")
	return preparationWorker{}
}
func TestPreparationWakeStopAndIncarnation(t *testing.T) {
	root, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	started := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- runPreparationWorker(ctx, root, exe, ticks, func(ctx context.Context) error { started <- struct{}{}; <-ctx.Done(); return ctx.Err() })
	}()
	r := awaitPreparationOwner(t, root)
	if err := controlPreparationWorker(ctx, root, "stale", "stop"); err == nil {
		t.Fatal("stale incarnation accepted")
	}
	if err := controlPreparationWorker(ctx, root, r.Identity, "wake"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("wake did not dispatch")
	}
	if err := controlPreparationWorker(ctx, root, r.Identity, "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel owned work")
	}
	_, s, err := probePreparationWorker(root)
	if err != nil || s != preparationGone {
		t.Fatalf("%s %v", s, err)
	}
	select {
	case <-started:
		t.Fatal("stop restarted")
	default:
	}
}
func TestPreparationReplacementDrainsAndBlocksDispatch(t *testing.T) {
	root, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runPreparationWorker(ctx, root, exe, ticks, func(context.Context) error { started <- struct{}{}; <-release; return nil })
	}()
	r := awaitPreparationOwner(t, root)
	ticks <- time.Now()
	<-started
	if err := controlPreparationWorker(ctx, root, r.Identity, "replace"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("replacement did not drain")
	default:
	}
	if err := controlPreparationWorker(ctx, root, r.Identity, "wake"); err == nil {
		t.Fatal("draining owner accepted wake")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("dispatched after replacement")
	default:
	}
}
func TestPreparationHeldUnknownNeverTakesOver(t *testing.T) {
	root, exe := preparationFixture(t)
	lock, err := pidlock.Acquire(filepath.Join(root, "worker.lock"), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := writePreparationJSON(filepath.Join(root, "scheduler.json"), preparationWorker{Identity: "stale", PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	_, s, err := probePreparationWorker(root)
	if s != preparationUnknown || err == nil {
		t.Fatalf("%s %v", s, err)
	}
	if err := runPreparationWorker(context.Background(), root, exe, nil, func(context.Context) error { t.Fatal("unknown dispatched"); return nil }); !errors.Is(err, pidlock.ErrHeld) {
		t.Fatalf("%v", err)
	}
	if err := controlPreparationWorker(context.Background(), root, "stale", "stop"); err == nil {
		t.Fatal("unreachable stop accepted")
	}
}
func TestPreparationTimerChecksSelectedExecutable(t *testing.T) {
	root, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	calls := make(chan struct{}, 2)
	go func() {
		done <- runPreparationWorker(ctx, root, exe, ticks, func(context.Context) error { calls <- struct{}{}; return nil })
	}()
	awaitPreparationOwner(t, root)
	ticks <- time.Now()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("timer callsite missing")
	}
	if err := os.WriteFile(exe, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing identity evidence")
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not exit")
	}
	select {
	case <-calls:
		t.Fatal("replaced binary dispatched")
	default:
	}
}
func TestPreparationStatusReadOnlyStaleOwner(t *testing.T) {
	root, _ := preparationFixture(t)
	lock, err := pidlock.Acquire(filepath.Join(root, "worker.lock"), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()
	record := filepath.Join(root, "scheduler.json")
	if err := writePreparationJSON(record, preparationWorker{Identity: "dead", PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "worker.lock"))
	owner, _ := os.ReadFile(record)
	_, s, err := probePreparationWorker(root)
	if err != nil || s != preparationGone {
		t.Fatalf("%s %v", s, err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "worker.lock"))
	retained, _ := os.ReadFile(record)
	if string(before) != string(after) || string(owner) != string(retained) {
		t.Fatal("status mutated state")
	}
}

func TestPreparationNewLeaseRejectsPriorIdentity(t *testing.T) {
	root, exe := preparationFixture(t)
	start := func() (preparationWorker, chan error, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runPreparationWorker(ctx, root, exe, nil, func(context.Context) error { return nil }) }()
		return awaitPreparationOwner(t, root), done, cancel
	}
	first, done, cancel := start()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	second, done, cancel := start()
	defer cancel()
	if first.Identity == second.Identity {
		t.Fatal("lease incarnation reused")
	}
	if err := controlPreparationWorker(context.Background(), root, first.Identity, "stop"); err == nil {
		t.Fatal("stale owner stopped replacement")
	}
	if err := controlPreparationWorker(context.Background(), root, second.Identity, "status"); err != nil {
		t.Fatal(err)
	}
	if err := controlPreparationWorker(context.Background(), root, second.Identity, "stop"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestPreparationConcurrentLeaseDoesNotDispatch(t *testing.T) {
	root, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runPreparationWorker(ctx, root, exe, nil, func(context.Context) error { return nil }) }()
	r := awaitPreparationOwner(t, root)
	if err := runPreparationWorker(ctx, root, exe, nil, func(context.Context) error { t.Fatal("duplicate dispatch"); return nil }); !errors.Is(err, pidlock.ErrHeld) {
		t.Fatalf("%v", err)
	}
	if err := controlPreparationWorker(ctx, root, r.Identity, "stop"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestPreparationUnreachableControlIsBoundedAndRetainsEvidence(t *testing.T) {
	root, _ := preparationFixture(t)
	lock, err := pidlock.Acquire(filepath.Join(root, "worker.lock"), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	record := filepath.Join(root, "scheduler.json")
	if err := writePreparationJSON(record, preparationWorker{Identity: "unknown", PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "control.endpoint"), []byte("malformed"), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, s, err := probePreparationWorker(root)
	if err == nil || s != preparationUnknown || time.Since(started) > 2*time.Second {
		t.Fatalf("%s %v", s, err)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatal("owner evidence removed")
	}
}

func TestPreparationUnreadableExecutableStopsBeforeDispatch(t *testing.T) {
	root, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() {
		done <- runPreparationWorker(ctx, root, exe, ticks, func(context.Context) error { t.Error("unproven executable dispatched"); return nil })
	}()
	awaitPreparationOwner(t, root)
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unreadability not reported")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestPreparationSlowAuthenticatedClientDoesNotDelayStop(t *testing.T) {
	root, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() {
		done <- runPreparationWorker(ctx, root, exe, ticks, func(context.Context) error { return nil })
	}()
	owner := awaitPreparationOwner(t, root)
	slow, err := svcendpoint.DialLocal(filepath.Join(root, "control.endpoint"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	// Authenticate, then hold the application request open without a complete JSON command.
	if _, err = slow.Write([]byte(`{"Identity":`)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := controlPreparationWorker(ctx, root, owner.Identity, "stop"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > preparationControlBound/2 {
		t.Fatalf("slow client serialized stop: %v", elapsed)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func startAuthenticatedPreparationOwner(t *testing.T, root string) preparationWorker {
	t.Helper()
	_, exe := preparationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ticks := make(chan time.Time)
	go func() {
		done <- runPreparationWorker(ctx, root, exe, ticks, func(context.Context) error { return nil })
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
	})
	return awaitPreparationOwner(t, root)
}
