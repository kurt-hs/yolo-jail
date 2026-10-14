package packsrc

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Only the contended wait consults the context: an uncontended pack-store lock is
// still taken under a cancelled ctx, so an advance a Ctrl-C ended can read its check
// record or clear the attempt's stamp (PF-D25). The wait itself stays interruptible,
// because a pack-store flock wait is what PF-D25 interrupts (PF-D38).
func TestFlockPathContextCancelledTakesUncontendedOnly(t *testing.T) {
	cancelled := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}

	uncontended, err := flockPathContext(cancelled(), filepath.Join(t.TempDir(), "record.json"), "record", nil)
	if err != nil {
		t.Fatalf("cancelled ctx refused an uncontended lock: %v", err)
	}
	uncontended()

	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	holder, err := flockPath(path, "record", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder()
	waited := false
	done := make(chan error, 1)
	go func() {
		_, err := flockPathContext(cancelled(), path, "record", func(string) { waited = true })
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("contended wait under cancelled ctx = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("contended wait under cancelled ctx blocked instead of failing")
	}
	if !waited {
		t.Error("contended wait never reported it was waiting: took the wrong branch")
	}
}
