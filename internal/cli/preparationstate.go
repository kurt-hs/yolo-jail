package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

const preparationStateSchema = 1

type preparationSnapshot struct {
	Key, Runtime, Platform, InputIdentity, Phase, ServingEntry, Reason, Log string
	Generation                                                              uint64
	At, RetryAt                                                             time.Time
	WorkerIdentity                                                          string
}

type preparationWorker struct {
	Identity      string    `json:"identity,omitempty"`
	PID           int       `json:"pid,omitempty"`
	Executable    string    `json:"executable,omitempty"`
	BinaryDigest  string    `json:"binary_digest,omitempty"`
	StopRequested bool      `json:"stop_requested,omitempty"`
	StoppedAt     time.Time `json:"stopped_at,omitempty"`
}

type preparationRecord struct {
	Key, Runtime, Platform, Kind, InputIdentity, PinCommit string
	Generation                                             uint64
	Active                                                 bool
	Phase                                                  string
	ServingEntry                                           string
	Reason                                                 string
	Log                                                    string
	At, RetryAt                                            time.Time
	UpdatedAt                                              time.Time
	// Retained is diagnostic evidence from the previous declaration, not admission authority.
	Retained *preparationSnapshot `json:"retained,omitempty"`
}

type preparationState struct {
	Schema        int                 `json:"schema"`
	Subscriptions []preparationRecord `json:"subscriptions"`
	Worker        preparationWorker   `json:"worker"`
}

func preparationStatePath() string {
	return filepath.Join(paths.BackgroundAdvanceDir(), "scheduler.json")
}
func preparationStateLockPath() string {
	return filepath.Join(paths.BackgroundAdvanceDir(), "scheduler-state.lock")
}
func preparationWorkerLockPath() string {
	return filepath.Join(preparationRoot(), "worker.lock")
}

var preparationNow = time.Now

func preparationRoot() string { return paths.BackgroundAdvanceDir() }

func loadPreparationState() (preparationState, error) {
	return loadPreparationStateAt(preparationRoot())
}

func loadPreparationStateAt(root string) (preparationState, error) {
	var state preparationState
	b, err := os.ReadFile(filepath.Join(root, "scheduler.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return preparationState{Schema: preparationStateSchema}, nil
		}
		return state, err
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return state, fmt.Errorf("reading preparation scheduler state: %w — move %s aside only after preserving it, then run `yolo pack prepare start`", err, preparationStatePath())
	}
	if state.Schema != preparationStateSchema {
		return state, fmt.Errorf("preparation scheduler state schema %d is unsupported — preserve %s and update yolo before running `yolo pack prepare start`", state.Schema, preparationStatePath())
	}
	return state, nil
}

func writePreparationState(state preparationState) error {
	state.Schema = preparationStateSchema
	return writePreparationJSON(preparationStatePath(), state)
}

func withPreparationState(fn func(*preparationState) error) error {
	return withPreparationStateAt(preparationRoot(), fn)
}

func withPreparationStateAt(root string, fn func(*preparationState) error) error {
	if err := ensurePreparationRoot(root); err != nil {
		return err
	}
	lock, err := pidlock.Acquire(filepath.Join(root, "scheduler-state.lock"), pidlock.Wait, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := loadPreparationStateAt(root)
	if err != nil {
		return err
	}
	previous := append([]preparationRecord(nil), state.Subscriptions...)
	if err := fn(&state); err != nil {
		return err
	}
	for i := range state.Subscriptions {
		r := &state.Subscriptions[i]
		j := preparationRecordIndex(preparationState{Subscriptions: previous}, r.Key, r.Runtime, r.Platform)
		if j < 0 {
			continue
		}
		old := previous[j]
		if old.Generation == r.Generation && old.InputIdentity == r.InputIdentity {
			continue
		}
		if old.Phase != "" && old.Phase != "pending" && old.Phase != "inactive" {
			snapshot := preparationRecordSnapshot(old, state.Worker.Identity)
			r.Retained = &snapshot
		}
		// Never relabel an earlier observation with the newly registered generation.
		if r.Phase == old.Phase {
			r.Phase = "pending"
		}
		if r.ServingEntry == old.ServingEntry {
			r.ServingEntry = ""
		}
		if r.Reason == old.Reason {
			r.Reason = ""
		}
		if r.Log == old.Log {
			r.Log = ""
		}
		if r.At == old.At {
			r.At = time.Time{}
		}
		if r.RetryAt == old.RetryAt {
			r.RetryAt = time.Time{}
		}
	}
	state.Schema = preparationStateSchema
	return writePreparationJSON(filepath.Join(root, "scheduler.json"), state)
}

func preparationRecordIndex(state preparationState, key, runtime, platform string) int {
	for i := range state.Subscriptions {
		r := &state.Subscriptions[i]
		if r.Key == key && r.Runtime == runtime && r.Platform == platform {
			return i
		}
	}
	return -1
}

func preparationSnapshotFor(state preparationState, key string) (preparationSnapshot, bool) {
	var candidates []preparationRecord
	for _, r := range state.Subscriptions {
		if r.Key == key {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return preparationSnapshot{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Runtime != candidates[j].Runtime {
			return candidates[i].Runtime < candidates[j].Runtime
		}
		return candidates[i].Platform < candidates[j].Platform
	})
	return preparationRecordSnapshot(candidates[0], state.Worker.Identity), true
}

func preparationRecordSnapshot(r preparationRecord, workerIdentity string) preparationSnapshot {
	if r.Phase == "pending" && r.Retained != nil {
		return *r.Retained
	}
	return preparationSnapshot{Key: r.Key, Runtime: r.Runtime, Platform: r.Platform, InputIdentity: r.InputIdentity,
		Generation: r.Generation, Phase: r.Phase, ServingEntry: r.ServingEntry, Reason: r.Reason, Log: r.Log,
		At: r.At, RetryAt: r.RetryAt, WorkerIdentity: workerIdentity}
}

func readPreparationSnapshot(key string) (preparationSnapshot, error) {
	state, err := loadPreparationState()
	if err != nil {
		return preparationSnapshot{}, err
	}
	snapshot, _ := preparationSnapshotFor(state, key)
	return snapshot, nil
}

func preparationUpdateRecord(key, runtime, platform string, update func(*preparationRecord)) error {
	return withPreparationState(func(state *preparationState) error {
		i := preparationRecordIndex(*state, key, runtime, platform)
		if i < 0 {
			return fmt.Errorf("preparation subscription %s is not registered", key)
		}
		update(&state.Subscriptions[i])
		return nil
	})
}

func preparationSubscriptions() ([]preparationRecord, error) {
	state, err := loadPreparationState()
	if err != nil {
		return nil, err
	}
	return append([]preparationRecord(nil), state.Subscriptions...), nil
}

func sortPreparationRecords(records []preparationRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Key != records[j].Key {
			return records[i].Key < records[j].Key
		}
		if records[i].Runtime != records[j].Runtime {
			return records[i].Runtime < records[j].Runtime
		}
		return records[i].Platform < records[j].Platform
	})
}
