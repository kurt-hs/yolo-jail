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

func TestFreshCheckBoundaryWaitsForStartTransactionAndCancellationIsLocal(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	const owner = "pack/program"
	entered, release := make(chan struct{}), make(chan struct{})
	writer := make(chan error, 1)
	go func() {
		writer <- s.withCheckStart(owner, func(start *CheckStart) error {
			start.Seq = 5
			close(entered)
			<-release
			return saveCheckStart(filepath.Join(s.Dir, "checks", "start-"+mirrorSlug(owner)+".json"), *start)
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	local := *s
	local.Ctx = ctx
	canceled := make(chan error, 1)
	go func() { _, err := local.CheckBoundary(owner); canceled <- err }()
	other := make(chan struct {
		start CheckStart
		err   error
	}, 1)
	go func() {
		start, err := s.CheckBoundary(owner)
		other <- struct {
			start CheckStart
			err   error
		}{start, err}
	}()
	cancel()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled boundary = %v", err)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("cancelled fresh boundary remained blocked")
	}
	close(release)
	if err := <-writer; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-other:
		if got.err != nil || got.start.Seq != 5 || got.start.Epoch == "" {
			t.Fatalf("remaining boundary = %+v", got)
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
	beforeStart, err := u.store.CheckBoundary(w.Owner)
	if err != nil {
		t.Fatal(err)
	}
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
	afterStart, err := u.store.CheckBoundary(w.Owner)
	if err != nil || afterStart.Epoch != beforeStart.Epoch || afterStart.Seq != beforeStart.Seq+1 {
		t.Errorf("NoWait did not record exactly one started attempt: before=%+v after=%+v err=%v", beforeStart, afterStart, err)
	}
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
