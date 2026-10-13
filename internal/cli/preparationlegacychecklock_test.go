package cli

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// TestPreparationIdleRefreshCancellationDoesNotWaitForLegacyGoodRecordLock exercises the real
// scheduler tick against a held check-record lock. The good build is deliberately in the legacy
// series format that LoadPatchedRecord would migrate under a waiting flock.
func TestPreparationIdleRefreshCancellationDoesNotWaitForLegacyGoodRecordLock(t *testing.T) {
	pf, f, s, goodEntry := legacyPreparationFixture(t)
	if err := preparationUpdateRecord(f.Key(), "podman", patchedTestPlatform, func(r *preparationRecord) {
		r.Phase, r.Reason, r.RetryAt = "failed", "preserve this failure", pf.now.Add(time.Hour)
	}); err != nil {
		t.Fatal(err)
	}
	release := holdPreparationCheckRecord(t, f.Key())
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	// Let the synchronous idle refresh reach its record inspection before cancellation.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("scheduler cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("production scheduler refresh did not drain cancellation before the held check-record lock was released")
	}
	release()
	r, err := preparationRecordFor(f.Key(), "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if r.Phase != "failed" || r.Reason != "preserve this failure" || !r.RetryAt.Equal(pf.now.Add(time.Hour)) {
		t.Fatalf("contended idle refresh changed prior failure/backoff: %+v", r)
	}
	assertLegacyPreparationGood(t, f, s, goodEntry)
}

// TestPreparationAdmissionDefersLegacyRecordContentionAndReusesTheBuiltCandidate requires the real
// admission guard to decline a contended legacy record without publishing freshness or consuming
// the admitted candidate, then to revalidate successfully after the lock is released.
func TestPreparationAdmissionDefersLegacyRecordContentionAndReusesTheBuiltCandidate(t *testing.T) {
	pf, f, s, goodEntry := legacyPreparationFixture(t)
	builds := len(pf.builds)
	record, err := readPreparationSnapshot(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	release := holdPreparationCheckRecord(t, f.Key())
	type admissionResult struct {
		err       error
		committed bool
	}
	result := make(chan admissionResult, 1)
	go func() {
		committed := false
		err := preparationAdmissionGuardUnder(context.Background(), f.Key(), "podman", patchedTestPlatform, record.Generation)(func() error {
			committed = true
			return nil
		})
		result <- admissionResult{err: err, committed: committed}
	}()
	select {
	case got := <-result:
		if !errors.Is(got.err, packsrc.ErrLockHeld) || got.committed {
			release()
			t.Fatalf("contended admission = %+v, want ErrLockHeld without publication", got)
		}
	case <-time.After(time.Second):
		release()
		<-result
		t.Fatal("admission inspection waited for the legacy check-record lock")
	}
	afterBusy, err := patchedForkStore().LoadCheckRecord(f.Key())
	if err != nil {
		release()
		t.Fatal(err)
	}
	if afterBusy.Good == nil || afterBusy.Good.Entry != goodEntry || afterBusy.Good.Series != s.LegacyDigest {
		release()
		t.Fatalf("contended admission changed or lost the built candidate: %+v", afterBusy.Good)
	}
	release()
	committed := false
	err = preparationAdmissionGuardUnder(context.Background(), f.Key(), "podman", patchedTestPlatform, record.Generation)(func() error {
		committed = true
		return nil
	})
	if err != nil || !committed {
		t.Fatalf("admission after lock release = committed %t, err %v", committed, err)
	}
	assertLegacyPreparationGood(t, f, s, goodEntry)
	if got := len(pf.builds); got != builds {
		t.Fatalf("admission retry rebuilt instead of reusing the existing candidate: builds %d, before %d", got, builds)
	}
}

func legacyPreparationFixture(t *testing.T) (*treeFixture, packload.Fork, *packsrc.Series, string) {
	t.Helper()
	pf := newTreeFixture(t, `"f.txt"`)
	first, out := pf.deliver(t, true)
	if first.Dir == "" {
		t.Fatalf("fixture did not build its initial candidate: %+v\\n%s", first, out)
	}
	writeFile(t, filepath.Join(pf.home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[{"source":"file://`+
		pf.treeDir+`","name":"treepack"}],"agent_updates":{"treepack":"next-launch"}}`)
	stubPreparationWorkerStart(t)
	trees := packload.PatchedTrees(selectConfiguredHostPacks().packs)
	f := pf.tree(t)
	if err := registerTreePreparation(trees, run.TreeBuildRequest{Build: true, Runtime: "podman", Platform: patchedTestPlatform}, io.Discard); err != nil {
		t.Fatal(err)
	}
	s, err := f.ReadSeries()
	if err != nil {
		t.Fatal(err)
	}
	if s.LegacyDigest == "" || s.LegacyDigest == s.Digest {
		t.Fatalf("fixture series has no distinct legacy digest: %+v", s)
	}
	store := patchedForkStore()
	rec, err := store.LoadCheckRecord(f.Key())
	if err != nil || rec.Good == nil {
		t.Fatalf("fixture has no admitted good build: record=%+v err=%v", rec, err)
	}
	entry := rec.Good.Entry
	if err := store.WithCheckRecord(f.Key(), nil, func(r *packsrc.CheckRecord, readErr error, _ func() error) (bool, error) {
		if readErr != nil || r.Good == nil {
			return false, errors.New("fixture good build disappeared")
		}
		g := *r.Good
		g.Series = s.LegacyDigest
		g.Recipe = run.PatchedRecipe(f, s.LegacyDigest)
		r.Good = &g
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	return pf, f, s, entry
}

func holdPreparationCheckRecord(t *testing.T, key string) func() {
	t.Helper()
	entered, unblock, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- patchedForkStore().WithCheckRecord(key, nil, func(*packsrc.CheckRecord, error, func() error) (bool, error) {
			close(entered)
			<-unblock
			return false, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(unblock)
		t.Fatal("could not acquire the fixture's real check-record lock")
	}
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		close(unblock)
		if err := <-done; err != nil {
			t.Errorf("release check-record lock: %v", err)
		}
	}
}

func assertLegacyPreparationGood(t *testing.T, f packload.Fork, s *packsrc.Series, entry string) {
	t.Helper()
	rec, err := patchedForkStore().LoadCheckRecord(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Good == nil || rec.Good.Entry != entry || rec.Good.Series != s.LegacyDigest || rec.Good.Recipe != run.PatchedRecipe(f, s.LegacyDigest) {
		t.Fatalf("legacy good build was migrated, changed, or lost: %+v", rec.Good)
	}
}

func preparationRecordFor(key, runtime, platform string) (preparationRecord, error) {
	state, err := loadPreparationState()
	if err != nil {
		return preparationRecord{}, err
	}
	idx := preparationRecordIndex(state, key, runtime, platform)
	if idx < 0 {
		return preparationRecord{}, errors.New("preparation subscription missing")
	}
	return state.Subscriptions[idx], nil
}
