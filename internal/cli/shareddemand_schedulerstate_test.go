package cli

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// The scheduler's own bookkeeping is not another process owning the key. scheduler-state.lock
// guards only scheduler.json's read-modify-write, and the scheduler takes it on every tick and
// after every finished task. A timer admission revalidates its subscription under that lock, and
// used to try it once: when the scheduler's record refresh overlapped the admission, the retry
// tick that should have admitted went busy again
// (TestSharedBackgroundDemandStateLockStaysBusyAndRetriesNextTick, on CI and under GOMAXPROCS=1).
// Here a short hold is made to overlap the timer's FIRST admission, so the overlap is not left
// to timing: the admission must wait it out and admit on that tick.
func TestSharedBackgroundAdmissionWaitsOutTheSchedulersStateUpdate(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubSpawn(t)
	pf.launch(t, "podman", &run.ActInterrupt{})
	if err := withPreparationState(func(state *preparationState) error { state.Worker.Identity = "timer-fixture"; return nil }); err != nil {
		t.Fatal(err)
	}
	oldGood := outcomeOfRecord(t, poolKeyA)
	stubSharedPreparationWorker(t)
	var once atomic.Bool
	held := make(chan struct{})
	previousHook := sharedDemandAdmissionHook
	sharedDemandAdmissionHook = func(d preparationDemand) {
		if d.Key != poolKeyA || !once.CompareAndSwap(false, true) {
			return
		}
		// What the scheduler's record update does: hold scheduler-state.lock for a moment.
		// Waiting, bounded: the scheduler's own record update can hold it at this moment too.
		lock, err := pauseDemandStateLock(t, preparationStateLockPath())
		if err != nil {
			t.Errorf("hold scheduler-state.lock as its record update would: %v", err)
			close(held)
			return
		}
		close(held)
		go func() { time.Sleep(4 * sharedDemandPoll); lock.Release() }()
	}
	t.Cleanup(func() { sharedDemandAdmissionHook = previousHook })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	drain := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard, nil, drain) }()
	ticks <- pf.now
	select {
	case <-held:
	case <-time.After(testsupport.ReadinessBudget(t)):
		t.Fatal("background worker did not reach its admission")
	}
	waitFor(t, "the timer's first admission to settle", func() bool {
		snapshot, err := readPreparationSnapshot(poolKeyA)
		return err == nil && (snapshot.Phase == "admitted" || snapshot.Phase == "busy" || snapshot.Phase == "failed")
	})
	if snapshot, _ := readPreparationSnapshot(poolKeyA); snapshot.Phase != "admitted" || snapshot.ServingEntry == "" {
		t.Fatalf("a short scheduler-state hold refused the timer's admission: %+v", snapshot)
	}
	if outcomeOfRecord(t, poolKeyA) == oldGood {
		t.Fatal("the admitted timer advance did not move Good")
	}
	close(drain)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testsupport.ReadinessBudget(t)):
		cancel()
		<-done
		t.Fatal("background scheduler did not drain")
	}
}
