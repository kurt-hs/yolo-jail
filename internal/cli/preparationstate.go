package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

type preparationSubscription struct {
	Key, Runtime, Platform, InputIdentity string
	Generation                            uint64
}

// Preparation snapshots describe scheduling, never independently certify admission.
type preparationSnapshot struct {
	Key, Runtime, Platform, InputIdentity, Phase, ServingEntry, Reason, Log string
	Generation                                                              uint64
	At, RetryAt                                                             time.Time
	WorkerIdentity                                                          string
}

type preparationWorkerRecord struct {
	Identity, ExecutablePath, ExecutableDigest string
	PID                                        int // Diagnostic only. Neither control nor recovery signals this PID.
}
type preparationLiveness string

const (
	preparationGone         preparationLiveness = "gone"
	preparationAlive        preparationLiveness = "alive"
	preparationUnknown      preparationLiveness = "unknown"
	preparationControlBound                     = time.Second
)

type preparationCommand struct{ Identity, Operation string }
type preparationResponse struct{ Identity, Phase, Error string }

// Control credentials and owner records must never live in a shared directory.
func ensurePreparationRoot(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("preparation state directory is not private; select a private scheduler directory before starting")
	}
	return nil
}

func writePreparationJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".preparation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// probePreparationWorker never creates, rewrites or removes state. Only a free
// kernel lease proves gone; a held lease needs authenticated incarnation proof.
func probePreparationWorker(root string) (preparationWorkerRecord, preparationLiveness, error) {
	var record preparationWorkerRecord
	f, err := os.OpenFile(filepath.Join(root, "worker.lock"), os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return record, preparationGone, nil
	}
	if err != nil {
		return record, preparationUnknown, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return record, preparationGone, nil
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		return record, preparationUnknown, err
	}
	data, err := os.ReadFile(filepath.Join(root, "worker.json"))
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	if err != nil || record.Identity == "" {
		return record, preparationUnknown, fmt.Errorf("preparation owner unproven; retry status or inspect scheduler state before starting: %v", err)
	}
	err = controlPreparationWorker(context.Background(), root, record.Identity, "status")
	if err != nil {
		return record, preparationUnknown, err
	}
	return record, preparationAlive, nil
}

// The expected random lease incarnation, not a PID or build stamp, targets control.
func controlPreparationWorker(ctx context.Context, root, expectedIdentity, op string) error {
	if expectedIdentity == "" {
		return errors.New("preparation identity missing; read status before control")
	}
	deadline := time.Now().Add(preparationControlBound)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := svcendpoint.DialLocal(filepath.Join(root, "control.endpoint"), time.Until(deadline))
	if err != nil {
		return fmt.Errorf("preparation control unavailable; retry status, do not replace an unproven owner: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	if err = json.NewEncoder(conn).Encode(preparationCommand{expectedIdentity, op}); err != nil {
		return err
	}
	var response preparationResponse
	if err = json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&response); err != nil {
		return err
	}
	if response.Identity != expectedIdentity {
		return errors.New("preparation incarnation changed; reread status before control")
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

var errPreparationGeneration = errors.New("preparation inputs changed; register current selection and retry")

func preparationSubscriptionID(s preparationSubscription) string {
	data, _ := json.Marshal([]string{s.Key, s.Runtime, s.Platform})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readPreparationSubscriptions(root string) (map[string]preparationSubscription, error) {
	values := make(map[string]preparationSubscription)
	data, err := os.ReadFile(filepath.Join(root, "subscriptions.json"))
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("preparation subscriptions corrupt; repair scheduler subscriptions before starting: %w", err)
	}
	if values == nil {
		return nil, errors.New("preparation subscriptions invalid; repair scheduler subscriptions before starting")
	}
	for id, s := range values {
		if s.Key == "" || s.Runtime == "" || s.Platform == "" || s.InputIdentity == "" || s.Generation == 0 || id != preparationSubscriptionID(s) {
			return nil, errors.New("preparation subscription invalid; repair scheduler subscriptions before starting")
		}
	}
	return values, nil
}

// Registration retains unmentioned subscriptions for inspection. Eligibility is
// reauthorized by the adapter, not implied by presence in this persistent map.
// It never takes a build/record lock, and never holds this transaction over work.
func registerPreparationSubscriptions(root string, selected []preparationSubscription) ([]preparationSubscription, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	if err := ensurePreparationRoot(root); err != nil {
		return nil, err
	}
	lock, err := pidlock.Acquire(filepath.Join(root, "subscriptions.lock"), pidlock.Mode{Wait: true, Bound: time.Second}, nil)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	values, err := readPreparationSubscriptions(root)
	if err != nil {
		return nil, err
	}
	unique := make(map[string]preparationSubscription)
	for _, s := range selected {
		if s.Key == "" || s.Runtime == "" || s.Platform == "" || s.InputIdentity == "" {
			return nil, errors.New("preparation inputs incomplete; resolve authorized declarations before registering")
		}
		id := preparationSubscriptionID(s)
		if prior, ok := unique[id]; ok && prior.InputIdentity != s.InputIdentity {
			return nil, errors.New("conflicting preparation inputs; resolve selection before registering")
		}
		unique[id] = s
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]preparationSubscription, 0, len(ids))
	for _, id := range ids {
		s := unique[id]
		prior, exists := values[id]
		s.Generation = prior.Generation
		if !exists || prior.InputIdentity != s.InputIdentity {
			if s.Generation == ^uint64(0) {
				return nil, errors.New("preparation generation exhausted; inspect scheduler metadata before retrying")
			}
			s.Generation++
		}
		values[id] = s
		result = append(result, s)
	}
	if err = writePreparationJSON(filepath.Join(root, "subscriptions.json"), values); err != nil {
		return nil, err
	}
	return result, nil
}

// withPreparationGeneration is the nonwaiting admission prerequisite. Invoke
// inside the existing record commit callback (build -> record -> subscription).
// It validates registered identity only; the caller must ALSO reauthorize policy
// and current declarations before settle. Contention must retain the candidate.
func withPreparationGeneration(root string, expected preparationSubscription, commit func() error) error {
	lock, err := pidlock.Acquire(filepath.Join(root, "subscriptions.lock"), pidlock.NoWait, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	values, err := readPreparationSubscriptions(root)
	if err != nil {
		return err
	}
	current, ok := values[preparationSubscriptionID(expected)]
	if !ok || current != expected {
		return errPreparationGeneration
	}
	return commit()
}

func publishPreparationSnapshot(root string, snapshot preparationSnapshot) error {
	expected := preparationSubscription{Key: snapshot.Key, Runtime: snapshot.Runtime, Platform: snapshot.Platform, InputIdentity: snapshot.InputIdentity, Generation: snapshot.Generation}
	return withPreparationGeneration(root, expected, func() error {
		return writePreparationJSON(filepath.Join(root, "snapshots", preparationSubscriptionID(expected)+".json"), snapshot)
	})
}

// readPreparationSnapshotFor is read-only and runtime/platform-qualified. A key
// alone cannot distinguish a host selection from a jail selection.
func readPreparationSnapshotFor(root string, selected preparationSubscription) (preparationSnapshot, error) {
	var snapshot preparationSnapshot
	data, err := os.ReadFile(filepath.Join(root, "snapshots", preparationSubscriptionID(selected)+".json"))
	if err != nil {
		return snapshot, err
	}
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, fmt.Errorf("preparation snapshot corrupt; inspect scheduler snapshot before retrying: %w", err)
	}
	if snapshot.Key != selected.Key || snapshot.Runtime != selected.Runtime || snapshot.Platform != selected.Platform || snapshot.InputIdentity == "" || snapshot.Generation == 0 {
		return snapshot, errors.New("preparation snapshot identity invalid; inspect scheduler snapshot before retrying")
	}
	if selected.Generation != 0 && (snapshot.Generation != selected.Generation || snapshot.InputIdentity != selected.InputIdentity) {
		return snapshot, errPreparationGeneration
	}
	return snapshot, nil
}
