package packsrc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFreshCheckBoundaryExcludesInFlightAndSharesFollowup(t *testing.T) {
	u := newPatchedUpstream(t)
	w := u.want(t, "main", "")
	before, err := u.store.CheckBoundary(w.Owner)
	if err != nil {
		t.Fatal(err)
	}
	bin, started, release := blockingGit(t)
	blocked := &Store{Dir: u.store.Dir, Git: bin, Getenv: noStagedTree}
	done := make(chan CheckResult, 1)
	go func() {
		done <- blocked.CheckPatched(w, CheckOptions{Force: true, InputIdentity: "recipe", RequestID: "first", FreshAfter: &before})
	}()
	deadline := time.After(5 * time.Second)
	for !started() {
		select {
		case <-deadline:
			release()
			t.Fatal("check never began")
		case <-time.After(time.Millisecond):
		}
	}
	after, err := u.store.CheckBoundary(w.Owner)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if after.Seq <= before.Seq {
		release()
		t.Fatal("transport began before start was committed")
	}
	release()
	first := <-done
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	if first.Record.Fresh.Qualifies(after, "recipe") {
		t.Fatal("in-flight check satisfied later request")
	}
	next := u.store.CheckPatched(w, CheckOptions{InputIdentity: "recipe", RequestID: "second", FreshAfter: &after})
	if next.Err != nil || !next.Ran || !next.Record.Fresh.Qualifies(after, "recipe") {
		t.Fatalf("followup: %+v", next)
	}
	shared := u.store.CheckPatched(w, CheckOptions{InputIdentity: "recipe", RequestID: "third", FreshAfter: &after})
	if shared.Err != nil || shared.Ran {
		t.Fatalf("qualifying winner was not reused: %+v", shared)
	}
}

func TestCheckBoundaryCancellation(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Ctx = ctx
	if _, err := s.CheckBoundary("owner/bin"); err == nil {
		t.Fatal("cancelled boundary succeeded")
	}
}

func TestFreshCheckLedgerResetCorruptAndRollbackFailClosed(t *testing.T) {
	u := newPatchedUpstream(t)
	w := u.want(t, "main", "")
	boundary, err := u.store.CheckBoundary(w.Owner)
	if err != nil {
		t.Fatal(err)
	}
	checked := u.store.CheckPatched(w, CheckOptions{InputIdentity: "recipe", RequestID: "request", FreshAfter: &boundary})
	if checked.Err != nil {
		t.Fatal(checked.Err)
	}
	path := filepath.Join(u.store.Dir, "checks", "start-"+mirrorSlug(w.Owner)+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = u.store.CheckBoundary(w.Owner); err == nil {
		t.Fatal("corrupt authority became fresh")
	}
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = saveCheckStart(path, boundary); err != nil {
		t.Fatal(err)
	}
	if _, err = u.store.CheckBoundary(w.Owner); err == nil {
		t.Fatal("rolled back authority became fresh")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reused := u.store.CheckPatched(w, CheckOptions{InputIdentity: "recipe", RequestID: "request", FreshAfter: &boundary})
	if reused.Err == nil {
		t.Fatal("reset authority reused a historical qualifying record")
	}
}

func TestCheckBoundaryLockWaitIsIndependentlyCancellable(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	entered, release := make(chan struct{}), make(chan struct{})
	owner := make(chan error, 1)
	go func() {
		owner <- s.withCheckStart("owner/bin", func(*CheckStart) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiter := *s
	waiter.Ctx = ctx
	done := make(chan error, 1)
	go func() { _, err := waiter.CheckBoundary("owner/bin"); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled lock wait succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("boundary wait ignored cancellation")
	}
	close(release)
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
}
