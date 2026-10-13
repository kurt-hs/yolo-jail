package packsrc

// nowait_test.go pins a NoWait store (store.go, docs/design/pi-extension-store-builds.md §6.2 rule
// 5, XB-D19): the background advance's mirror and record locks are a try, never a wait, and a check
// that meets a held mirror lock leaves its record as it was.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// A HELD LOCK FAILS AT ONCE WITH ErrLockHeld under NoWait, for the mirror's lock and the record's,
// and the same store with NoWait unset takes each once it is free. Red if either lock stops reading
// NoWait.
func TestANoWaitStoreReturnsErrLockHeld(t *testing.T) {
	s := &Store{Dir: t.TempDir(), NoWait: true}
	const repo, owner = "https://example.invalid/r.git", "pack/ext"
	for _, c := range []struct {
		name string
		path string
		take func() (func(), error)
	}{
		{"mirror", filepath.Join(s.Dir, "locks", mirrorSlug(repo)+".lock"), func() (func(), error) { return s.lockMirror(repo, nil) }},
		{"record", filepath.Join(s.Dir, "locks", "check-"+mirrorSlug(owner)+".lock"), func() (func(), error) {
			return s.lockCheckRecord(owner, nil)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			hold, err := flockPath(c.path, "the lock", nil)
			if err != nil {
				t.Fatal(err)
			}
			type taken struct {
				unlock func()
				err    error
			}
			done := make(chan taken, 1)
			go func() { u, err := c.take(); done <- taken{u, err} }()
			select {
			case got := <-done:
				if !errors.Is(got.err, ErrLockHeld) || got.unlock != nil {
					t.Fatalf("a held %s lock under NoWait = %v, want ErrLockHeld", c.name, got.err)
				}
			case <-time.After(5 * time.Second):
				hold() // let the waiter through, so the test ends
				t.Fatalf("a held %s lock under NoWait was waited for", c.name)
			}
			hold()
			unlock, err := c.take()
			if err != nil {
				t.Fatalf("a free %s lock under NoWait: %v", c.name, err)
			}
			unlock()
		})
	}
}

func TestFreshCheckBoundaryWaitsForStartedCheckAndCanBeCancelled(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Git: "/nonexistent/git", Getenv: noStagedTree}
	const owner = "pack/program"
	if err := s.WithCheckRecord(owner, nil, func(r *CheckRecord, _ error, _ func() error) (bool, error) {
		r.StartedSeq = 4
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	unlock, err := s.lockCheckRecord(owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	boundary := make(chan struct {
		seq uint64
		err error
	}, 1)
	go func() {
		seq, err := s.FreshCheckBoundary(owner)
		boundary <- struct {
			seq uint64
			err error
		}{seq, err}
	}()
	select {
	case got := <-boundary:
		unlock()
		t.Fatalf("request boundary escaped the STARTED transaction: %+v", got)
	case <-time.After(30 * time.Millisecond):
	}
	// Update while retaining the already-held flock; this is CheckPatched's persisted STARTED event.
	r, err := s.LoadCheckRecord(owner)
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	r.StartedSeq = 5
	if err := s.saveCheckRecord(r); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	select {
	case got := <-boundary:
		if got.err != nil || got.seq != 5 {
			t.Fatalf("request after started check boundary = %d, %v; want 5", got.seq, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fresh request did not acquire the record after its check completed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unlock, err = s.lockCheckRecord(owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled := make(chan error, 1)
	waiting := make(chan struct{})
	go func() {
		_, err := s.freshCheckBoundaryContext(ctx, owner, func(string) { close(waiting) })
		canceled <- err
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		unlock()
		t.Fatal("fresh boundary did not wait on the held record")
	}
	other := make(chan struct {
		seq uint64
		err error
	}, 1)
	go func() {
		seq, err := s.FreshCheckBoundary(owner)
		other <- struct {
			seq uint64
			err error
		}{seq, err}
	}()
	cancel()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled request lock wait = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		unlock()
		t.Fatal("cancelled fresh boundary remained blocked")
	}
	unlock()
	select {
	case got := <-other:
		if got.err != nil || got.seq != 5 {
			t.Fatalf("remaining fresh request boundary = %d, %v; want 5", got.seq, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling one request detached another waiter")
	}
}

// A NoWait CHECK THAT MEETS A HELD MIRROR LOCK fetches nothing and leaves the completed check
// unchanged; only the started-attempt identity advances, and the lock is reported.
func TestANoWaitCheckThatMeetsAHeldMirrorLeavesTheRecord(t *testing.T) {
	u := newPatchedUpstream(t)
	w := u.want(t, "main", "")
	u.check(t, w, false)
	beforeRecord, err := u.store.LoadCheckRecord(w.Owner)
	if err != nil {
		t.Fatal(err)
	}
	_, addr, _, err := w.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	hold, err := flockPath(filepath.Join(u.store.Dir, "locks", mirrorSlug(addr.Repo)+".lock"), "the mirror", nil)
	if err != nil {
		t.Fatal(err)
	}
	bg := *u.store
	bg.NoWait = true
	u.now = u.now.Add(2 * time.Hour)
	done := make(chan CheckResult, 1)
	go func() { done <- bg.CheckPatched(w, CheckOptions{Now: func() time.Time { return u.now }}) }()
	var res CheckResult
	select {
	case res = <-done:
		hold()
	case <-time.After(5 * time.Second):
		hold()
		<-done
		t.Fatal("the NoWait check waited for the held mirror lock")
	}
	if !errors.Is(res.Err, ErrLockHeld) || res.Ran {
		t.Fatalf("the check = %+v, want ErrLockHeld and nothing run", res)
	}
	afterRecord, err := u.store.LoadCheckRecord(w.Owner)
	if err != nil {
		t.Fatal(err)
	}
	if afterRecord.StartedSeq != beforeRecord.StartedSeq+1 || afterRecord.StartedInputs != beforeRecord.StartedInputs {
		t.Errorf("NoWait did not record exactly one started attempt: before=%+v after=%+v", beforeRecord, afterRecord)
	}
	beforeRecord.StartedSeq, beforeRecord.StartedAt, beforeRecord.StartedInputs = 0, 0, CheckInputs{}
	afterRecord.StartedSeq, afterRecord.StartedAt, afterRecord.StartedInputs = 0, 0, CheckInputs{}
	beforeStable, _ := json.Marshal(beforeRecord)
	afterStable, _ := json.Marshal(afterRecord)
	if !bytes.Equal(beforeStable, afterStable) {
		t.Errorf("the completed check record changed:\nbefore %s\nafter  %s", beforeStable, afterStable)
	}
}

// A SIBLING IN THE SAME PROCESS is still a held lock: NoWait must not introduce a process-mutex
// wait, which would also ignore cancellation. The pending sibling retries at the next launch.
func TestANoWaitTrySkipsItsOwnProcessesSibling(t *testing.T) {
	s := &Store{Dir: t.TempDir(), NoWait: true}
	const repo = "https://example.invalid/r.git"
	first, err := s.lockMirror(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	done := make(chan error, 1)
	go func() {
		unlock, err := s.lockMirror(repo, nil)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrLockHeld) {
			t.Errorf("the sibling's try = %v, want ErrLockHeld", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NoWait waited for its own process's sibling")
	}
}
