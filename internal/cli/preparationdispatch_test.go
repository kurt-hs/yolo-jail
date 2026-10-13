package cli

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

func TestPreparationTimerAdvancesWithoutAnotherLaunch(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubSpawn(t)
	if _, out := pf.launch(t, "podman", &run.ActInterrupt{}); out == "" {
		t.Fatal("no launch report")
	}
	builds := pf.countBuilds()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	deadline := time.After(10 * time.Second)
	for {
		complete := builds.Load() >= 2
		for _, key := range []string{poolKeyA, poolKeyB} {
			got, err := readBackgroundOutcome(backgroundOutcomePath(key))
			complete = complete && err == nil && got.To != ""
		}
		if complete {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("registered timer did not admit outcomes")
		case err := <-done:
			t.Fatalf("scheduler stopped: %v", err)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	subs, err := readPreparationSubscriptions(preparationRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range subs {
		before, err := readPreparationSnapshotFor(preparationRoot(), sub)
		if err != nil {
			t.Fatal(err)
		}
		f, err := currentPreparationTree(context.Background(), sub)
		if err != nil {
			t.Fatal(err)
		}
		noteBackgroundOutcome(f, richtext.Printer{W: io.Discard})
		after, err := readPreparationSnapshotFor(preparationRoot(), sub)
		if err != nil || before != after || after.ServingEntry == "" || after.WorkerIdentity == "" {
			t.Fatalf("notice consumed diagnostic evidence: %+v %v", after, err)
		}
	}
}

func TestPreparationTimerReauthorizesBeforeAdmission(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubSpawn(t)
	pf.launch(t, "podman", &run.ActInterrupt{})
	old := outcomeOfRecord(t, poolKeyA)
	entered := make(chan struct{})
	release := make(chan struct{})
	pf.child = func(ctx context.Context, _ forkBuild) int { close(entered); <-release; return 0 }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatal("no owned child")
	}
	// Revoke while a child that ignores cancellation is paused; settle must recheck.
	pf.agentUpdates(t, `false`)
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		o, err := readBackgroundOutcome(backgroundOutcomePath(poolKeyA))
		if err == nil && o.State != bgRunning {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if current := outcomeOfRecord(t, poolKeyA); current != old {
		t.Fatalf("revoked worker admitted %s, previously %s", current, old)
	}
}

func outcomeOfRecord(t *testing.T, key string) string {
	t.Helper()
	record, err := patchedAdvanceStore(true).LoadCheckRecord(key)
	if err != nil || record.Good == nil {
		t.Fatalf("record: %v %v", record, err)
	}
	return record.Good.Entry
}

func TestPreparationNoDueRegistrationSurvivesUntilLaterTimer(t *testing.T) {
	pf := newPoolFixture(t)
	pf.launch(t, "podman", &run.ActInterrupt{})
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubSpawn(t)
	builds := pf.countBuilds()
	pf.launch(t, "podman", &run.ActInterrupt{})
	if builds.Load() != 0 {
		t.Fatal("no-due registration built")
	}
	pf.commit(t, "", map[int]string{5: "five"})
	pf.now = pf.now.Add(2 * time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	deadline := time.After(10 * time.Second)
	for builds.Load() < 2 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("idle registration lost later timer work")
		case <-time.After(time.Millisecond):
		}
	}
	// Cancellation may interrupt final publication; the first admitted result is enough
	// here, and the main timer fixture separately proves both outcomes admitted.
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPreparationGenerationEditWhileChildPausedCannotAdmit(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubSpawn(t)
	pf.launch(t, "podman", &run.ActInterrupt{})
	old := outcomeOfRecord(t, poolKeyA)
	subscriptions, err := readPreparationSubscriptions(preparationRoot())
	if err != nil {
		t.Fatal(err)
	}
	var original preparationSubscription
	for _, s := range subscriptions {
		if s.Key == poolKeyA {
			original = s
		}
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	pf.child = func(ctx context.Context, _ forkBuild) int { once.Do(func() { close(entered) }); <-release; return 0 }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatal("no child")
	}
	edited := original
	edited.InputIdentity = "newly-registered-input"
	if _, err := registerPreparationSubscriptions(preparationRoot(), []preparationSubscription{edited}); err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		o, err := readBackgroundOutcome(backgroundOutcomePath(poolKeyA))
		if err == nil && o.State != bgRunning {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if current := outcomeOfRecord(t, poolKeyA); current != old {
		t.Fatalf("old generation admitted %s", current)
	}
}
