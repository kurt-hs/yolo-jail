package packsrc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithForkLockGuardedPublishesOnlyInsideGuard(t *testing.T) {
	dir, path := t.TempDir(), filepath.Join(t.TempDir(), ForkLockName)
	refusal := errors.New("revoked")
	mutate := func(l *ForkLock) (bool, error) { l.Set(ForkLockEntry{Key: "a/tool", Commit: "one"}); return true, nil }
	guard := func(save func() error) error { return refusal }
	if err := WithForkLockGuarded(context.Background(), dir, path, nil, mutate, guard); !errors.Is(err, refusal) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected guard wrote pin", err)
	}
	inside := false
	if err := WithForkLockGuarded(context.Background(), dir, path, nil, mutate, func(save func() error) error {
		inside = true
		if err := save(); err != nil {
			return err
		}
		l, err := LoadForkLock(path)
		if err != nil || l.Forks["a/tool"].Commit != "one" {
			t.Errorf("save not in guard: %+v %v", l, err)
		}
		return nil
	}); err != nil || !inside {
		t.Fatal(err, inside)
	}
	if err := WithForkLockGuarded(context.Background(), dir, path, nil, func(*ForkLock) (bool, error) { return false, nil }, func(func() error) error { t.Error("unchanged pins invoked save guard"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestWithForkLockGuardedCancelsWhileHeldAndPreservesOtherOwner(t *testing.T) {
	dir, path := t.TempDir(), filepath.Join(t.TempDir(), ForkLockName)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- WithForkLock(dir, path, nil, func(l *ForkLock) (bool, error) {
			close(entered)
			<-release
			l.Set(ForkLockEntry{Key: "other/tool", Commit: "other"})
			return true, nil
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := WithForkLockGuarded(ctx, dir, path, nil, func(*ForkLock) (bool, error) { t.Error("entered cancelled lock"); return false, nil }, func(save func() error) error { return save() })
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock ignored cancellation: %v", err)
	}
	if err := WithForkLockGuarded(context.Background(), dir, path, nil, func(l *ForkLock) (bool, error) { l.Set(ForkLockEntry{Key: "a/tool", Commit: "a"}); return true, nil }, func(save func() error) error { return save() }); err != nil {
		t.Fatal(err)
	}
	l, err := LoadForkLock(path)
	if err != nil || len(l.Forks) != 2 || l.Forks["other/tool"].Commit != "other" {
		t.Fatalf("lost other owner: %+v %v", l, err)
	}
}

func TestStoreContextCancelsMirrorLockWait(t *testing.T) {
	dir := t.TempDir()
	holder := &Store{Dir: dir}
	unlock, err := holder.lockMirror("repo", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = (&Store{Dir: dir, Ctx: ctx}).lockMirror("repo", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("mirror lock ignored cancellation", err)
	}
}
