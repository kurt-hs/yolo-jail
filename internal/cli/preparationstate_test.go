package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

func TestPreparationRegistrationGenerationAndSnapshotGuard(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scheduler")
	s := preparationSubscription{Key: "owner/tree", Runtime: "podman", Platform: "linux-amd64", InputIdentity: "input-one"}
	first, err := registerPreparationSubscriptions(root, []preparationSubscription{s, s})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Generation != 1 {
		t.Fatalf("%+v", first)
	}
	again, err := registerPreparationSubscriptions(root, []preparationSubscription{s})
	if err != nil || again[0] != first[0] {
		t.Fatalf("%+v %v", again, err)
	}
	snap := preparationSnapshot{Key: s.Key, Runtime: s.Runtime, Platform: s.Platform, InputIdentity: s.InputIdentity, Generation: first[0].Generation, Phase: "checking", WorkerIdentity: "lease-one"}
	if err := publishPreparationSnapshot(root, snap); err != nil {
		t.Fatal(err)
	}
	s.InputIdentity = "input-two"
	second, err := registerPreparationSubscriptions(root, []preparationSubscription{s})
	if err != nil || second[0].Generation != 2 {
		t.Fatalf("%+v %v", second, err)
	}
	if err := publishPreparationSnapshot(root, snap); !errors.Is(err, errPreparationGeneration) {
		t.Fatalf("stale snapshot admitted: %v", err)
	}
	if retained, err := readPreparationSnapshotFor(root, second[0]); !errors.Is(err, errPreparationGeneration) || retained.Generation != 1 {
		t.Fatalf("stale snapshot misrepresented or erased: %+v %v", retained, err)
	}
	committed := false
	if err := withPreparationGeneration(root, first[0], func() error { committed = true; return nil }); !errors.Is(err, errPreparationGeneration) || committed {
		t.Fatalf("stale admission: %v %v", committed, err)
	}
	if err := withPreparationGeneration(root, second[0], func() error { committed = true; return nil }); err != nil || !committed {
		t.Fatalf("current admission: %v %v", committed, err)
	}
}
func TestPreparationGenerationGuardNeverWaits(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scheduler")
	s := preparationSubscription{Key: "a", Runtime: "podman", Platform: "linux-amd64", InputIdentity: "one"}
	got, err := registerPreparationSubscriptions(root, []preparationSubscription{s})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := pidlock.Acquire(filepath.Join(root, "subscriptions.lock"), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := withPreparationGeneration(root, got[0], func() error { t.Fatal("busy guard committed"); return nil }); !errors.Is(err, pidlock.ErrHeld) {
		t.Fatalf("%v", err)
	}
}
func TestPreparationCorruptSubscriptionNeverResets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scheduler")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "subscriptions.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := registerPreparationSubscriptions(root, []preparationSubscription{{Key: "a", Runtime: "podman", Platform: "linux", InputIdentity: "one"}})
	if err == nil {
		t.Fatal("corrupt metadata reset")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("corrupt state overwritten")
	}
}

func TestPreparationConcurrentRegistrationCoalesces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scheduler")
	s := preparationSubscription{Key: "a", Runtime: "podman", Platform: "linux-amd64", InputIdentity: "one"}
	done := make(chan error, 8)
	for range 8 {
		go func() {
			got, err := registerPreparationSubscriptions(root, []preparationSubscription{s})
			if err == nil && (len(got) != 1 || got[0].Generation != 1) {
				err = errors.New("duplicate registration advanced generation")
			}
			done <- err
		}()
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
