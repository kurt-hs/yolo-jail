package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestPreparationOutputRecipeEditCancelsPausedChild(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	var once sync.Once
	pf.child = func(ctx context.Context, _ forkBuild) int {
		once.Do(func() { close(started) })
		<-ctx.Done()
		close(cancelled)
		return 1
	}
	_, _ = pf.launch(t, "podman", nil)
	before, _ := readPreparationSnapshot(poolKeyA)
	ctx, stop := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- pf.now
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		stop()
		<-done
		t.Fatal("child never started")
	}
	manifest := filepath.Join(pf.packs, "treepool", "pack.json")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, strings.ReplaceAll(string(b), `"source":`, `"produces":["different-output"],"source":`))
	ticks <- pf.now
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		stop()
		<-done
		t.Fatal("output-only recipe edit did not cancel stale child")
	}
	stop()
	<-done
	state, err := loadPreparationState()
	i := preparationRecordIndex(state, poolKeyA, "podman", patchedTestPlatform)
	if err != nil || i < 0 || state.Subscriptions[i].Generation <= before.Generation || state.Subscriptions[i].ServingEntry != "" {
		t.Fatalf("stale admission survived recipe edit: before=%+v state=%+v err=%v", before, state, err)
	}
	if retained := state.Subscriptions[i].Retained; retained == nil || retained.Generation != before.Generation {
		t.Fatalf("old diagnostic was relabelled with the new generation: %+v", state.Subscriptions[i])
	}
}

func TestPreparationScopedFilteredSnapshotsRetireOnlyOwnedCopies(t *testing.T) {
	pf := newPoolFixture(t)
	manifest := filepath.Join(pf.home, ".config", "yolo-jail", "config.jsonc")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, strings.Replace(string(b), `"name":"treepool"`, `"name":"treepool","exclude":["unused"]`, 1))
	records := []preparationRecord{{Key: poolKeyA, Runtime: "podman", Platform: patchedTestPlatform}}
	unrelated, err := packload.ProcessPackDir("unrelated-reader")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadDir(packload.ProcessTreeLocation())
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]bool{}
	for _, entry := range baseline {
		previous[entry.Name()] = true
	}
	for i := 0; i < 8; i++ {
		current, release, err := scopedPreparationTrees(context.Background(), records)
		if err != nil {
			release()
			t.Fatal(err)
		}
		f := current[poolKeyA+"\x00podman\x00"+patchedTestPlatform]
		if _, err := f.ReadSeries(); err != nil {
			release()
			t.Fatal(err)
		}
		release()
		if _, err := os.Stat(unrelated); err != nil {
			t.Fatalf("scope removed another reader's tree: %v", err)
		}
	}
	entries, err := os.ReadDir(packload.ProcessTreeLocation())
	if err != nil {
		t.Fatal(err)
	}
	// The process lease and unrelated reader remain, but the eight scoped snapshots do not.
	for _, entry := range entries {
		if !previous[entry.Name()] {
			t.Fatalf("superseded snapshot retained: %s", entry.Name())
		}
	}
}
