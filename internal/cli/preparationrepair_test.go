package cli

import (
	"context"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPreparationIdentityIncludesCompleteRecipeAndPlatforms(t *testing.T) {
	newPoolFixture(t)
	f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
	before, err := preparationInputIdentity(f, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	changed := f
	changed.Produces = append(append([]string(nil), f.Produces...), "extra-output")
	after, err := preparationInputIdentity(changed, "podman", patchedTestPlatform)
	if err != nil || before == after {
		t.Fatalf("output-only recipe edit retained identity: %s %s %v", before, after, err)
	}
	changed = f
	changed.Platforms = []string{"unsupported-platform"}
	after, err = preparationInputIdentity(changed, "podman", patchedTestPlatform)
	if err != nil || before == after {
		t.Fatalf("platform edit retained identity: %s %s %v", before, after, err)
	}
}

func TestPreparationRefreshRetainsFailureBackoff(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	_, _ = pf.launch(t, "podman", nil)
	records, err := preparationSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	current, err := currentPreparationTrees(records)
	if err != nil {
		t.Fatal(err)
	}
	reason := "concrete build failure"
	retry := pf.now.Add(123456789)
	for _, r := range records {
		if err := preparationUpdateRecord(r.Key, r.Runtime, r.Platform, func(rec *preparationRecord) { rec.Phase = "failed"; rec.Reason = reason; rec.RetryAt = retry }); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range refreshPreparationSubscriptions(records, current) {
		if r.Phase != "failed" || r.Reason != reason || !r.RetryAt.Equal(retry) {
			t.Fatalf("ordinary refresh erased failure: %+v", r)
		}
	}
}

func TestPreparationCorruptStateDrainsOwnedChild(t *testing.T) {
	preparationExitDrainsOwnedChild(t, func(pf *poolFixture) {
		if err := os.WriteFile(preparationStatePath(), []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPreparationInvalidConfigDrainsOwnedChild(t *testing.T) {
	preparationExitDrainsOwnedChild(t, func(pf *poolFixture) {
		writeFile(t, filepath.Join(pf.home, ".config", "yolo-jail", "config.jsonc"), "{")
	})
}

func TestPreparationBinaryMismatchDrainsOwnedChild(t *testing.T) {
	preparationExitDrainsOwnedChild(t, func(pf *poolFixture) {
		exe := filepath.Join(t.TempDir(), "replaced-worker")
		writeFile(t, exe, "replacement")
		if err := withPreparationState(func(state *preparationState) error {
			state.Worker.Executable, state.Worker.BinaryDigest = exe, "old-binary-digest"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func preparationExitDrainsOwnedChild(t *testing.T, mutate func(*poolFixture)) {
	t.Helper()
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	started, cancelled, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	pf.child = func(ctx context.Context, _ forkBuild) int {
		once.Do(func() { close(started) })
		<-ctx.Done()
		close(cancelled)
		<-cleaned
		return 1
	}
	_, _ = pf.launch(t, "podman", nil)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("child never started")
	}
	mutate(pf)
	ticks <- pf.now
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		close(cleaned)
		t.Fatal("loop error did not cancel child")
	}
	select {
	case err := <-done:
		close(cleaned)
		t.Fatalf("returned before cleanup: %v", err)
	default:
	}
	close(cleaned)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("loop failure was not reported")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not drain")
	}
}
