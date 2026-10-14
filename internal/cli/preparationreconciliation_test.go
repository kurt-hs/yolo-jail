package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"path/filepath"
)

type preparationSubscription = preparationRecord

var errPreparationGeneration = errPreparationAdmissionStale

func preparationSubscriptionID(s preparationRecord) string {
	data, _ := json.Marshal([]string{s.Key, s.Runtime, s.Platform})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func readPreparationSubscriptions(root string) (map[string]preparationRecord, error) {
	state, err := loadPreparationStateAt(root)
	if err != nil {
		return nil, err
	}
	result := map[string]preparationRecord{}
	for _, sub := range state.Subscriptions {
		result[preparationSubscriptionID(sub)] = sub
	}
	return result, nil
}
func registerPreparationSubscriptions(root string, selected []preparationRecord) ([]preparationRecord, error) {
	var result []preparationRecord
	err := withPreparationStateAt(root, func(state *preparationState) error {
		seen := map[string]bool{}
		for _, sub := range selected {
			id := preparationSubscriptionID(sub)
			if seen[id] {
				continue
			}
			seen[id] = true
			i := preparationRecordIndex(*state, sub.Key, sub.Runtime, sub.Platform)
			if i < 0 {
				state.Subscriptions = append(state.Subscriptions, sub)
				i = len(state.Subscriptions) - 1
			}
			r := &state.Subscriptions[i]
			if r.Generation == 0 || r.InputIdentity != sub.InputIdentity {
				r.Generation++
				r.InputIdentity = sub.InputIdentity
			}
			result = append(result, *r)
		}
		return nil
	})
	return result, err
}
func withPreparationGeneration(root string, sub preparationRecord, commit func() error) error {
	lock, err := pidlock.Acquire(filepath.Join(root, "scheduler-state.lock"), pidlock.NoWait, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := loadPreparationStateAt(root)
	if err != nil {
		return err
	}
	i := preparationRecordIndex(state, sub.Key, sub.Runtime, sub.Platform)
	if i < 0 || state.Subscriptions[i].Generation != sub.Generation || state.Subscriptions[i].InputIdentity != sub.InputIdentity {
		return errPreparationGeneration
	}
	return commit()
}
func publishPreparationSnapshot(root string, snapshot preparationSnapshot) error {
	return withPreparationStateAt(root, func(state *preparationState) error {
		i := preparationRecordIndex(*state, snapshot.Key, snapshot.Runtime, snapshot.Platform)
		if i < 0 || state.Subscriptions[i].Generation != snapshot.Generation || state.Subscriptions[i].InputIdentity != snapshot.InputIdentity {
			return errPreparationGeneration
		}
		r := &state.Subscriptions[i]
		r.Phase = snapshot.Phase
		r.ServingEntry = snapshot.ServingEntry
		r.Reason = snapshot.Reason
		r.Log = snapshot.Log
		r.At = snapshot.At
		r.RetryAt = snapshot.RetryAt
		state.Worker.Identity = snapshot.WorkerIdentity
		return nil
	})
}
func readPreparationSnapshotFor(root string, sub preparationRecord) (preparationSnapshot, error) {
	state, err := loadPreparationStateAt(root)
	if err != nil {
		return preparationSnapshot{}, err
	}
	i := preparationRecordIndex(state, sub.Key, sub.Runtime, sub.Platform)
	if i < 0 {
		return preparationSnapshot{}, errPreparationGeneration
	}
	snapshot := preparationRecordSnapshot(state.Subscriptions[i], state.Worker.Identity)
	if snapshot.Generation != sub.Generation || snapshot.InputIdentity != sub.InputIdentity {
		return snapshot, errPreparationGeneration
	}
	return snapshot, nil
}
func legacyPreparationEnvironment(in []string, names []string) []string {
	providers := jsonx.NewOrderedMap()
	for _, name := range names {
		entry := jsonx.NewOrderedMap()
		entry.Set("api_key_env_name", name)
		providers.Set(name, entry)
	}
	return preparationEnvironment(in, providers, nil)
}
