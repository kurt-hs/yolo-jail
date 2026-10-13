package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

type preparationRequest struct {
	ID            string
	RegisteredAt  time.Time
	Keys          []preparationSubscription
	AfterCheckSeq map[string]uint64
	Boundaries    map[string]packsrc.CheckStart
}

type preparationFreshness struct {
	RequestID, Key, InputIdentity, ServingEntry string
	CheckStartedAt                              time.Time
	Err                                         error
}

// Manual authorization changes automatic timing, not user selection or inputs.
func selectedFreshPreparation(ctx context.Context) ([]packload.Fork, error) {
	sel := selectHostPacks(func(e config.PackEntry) (*packload.Pack, error) {
		return resolveConfiguredPackWithSpec(e, config.ResolvePackSpec{Ctx: ctx, NoWait: true, Detached: true})
	}, config.UserScopeSelection())
	if problems := sel.problems(); len(problems) > 0 {
		return nil, fmt.Errorf("fresh preparation selection unavailable: %v — run `yolo check --no-build` and retry", problems)
	}
	return append(packload.PatchedTrees(sel.packs), packload.PatchedForks(sel.packs)...), nil
}

func newPreparationRequest(ctx context.Context, keys []packload.Fork, runtime, platform string) (*preparationRequest, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	req := &preparationRequest{ID: hex.EncodeToString(nonce), RegisteredAt: time.Now(), AfterCheckSeq: map[string]uint64{}, Boundaries: map[string]packsrc.CheckStart{}}
	store := patchedAdvanceStore(true)
	store.Ctx = ctx
	for _, f := range keys {
		if !f.Patched() {
			continue
		}
		if _, ok := req.Boundaries[f.Key()]; ok {
			continue
		}
		sub, err := treePreparationSubscription(f, runtime, platform)
		if err != nil {
			return nil, err
		}
		boundary, err := store.CheckBoundary(f.Key())
		if err != nil {
			return nil, err
		}
		req.Keys = append(req.Keys, sub)
		req.AfterCheckSeq[f.Key()] = boundary.Seq
		req.Boundaries[f.Key()] = boundary
	}

	return req, nil
}
func (r *preparationRequest) Boundary(key string) (packsrc.CheckStart, string, error) {
	for _, sub := range r.Keys {
		if sub.Key == key {
			return r.Boundaries[key], sub.InputIdentity, nil
		}
	}
	return packsrc.CheckStart{}, "", errors.New("fresh request no longer names this input; retry --update")
}
func (r *preparationRequest) Guard(key string, commit func() error) error {
	current, err := selectedFreshPreparation(context.Background())
	if err != nil {
		return err
	}
	for _, sub := range r.Keys {
		if sub.Key == key {
			for _, f := range current {
				if f.Key() == key {
					now, err := treePreparationSubscription(f, sub.Runtime, sub.Platform)
					if err != nil {
						return err
					}
					if now.InputIdentity == sub.InputIdentity {
						return commit()
					}
				}
			}
		}
	}
	return errors.New("fresh preparation inputs or authorization changed; retry `yolo --update -- <command>` with the current selection")
}
