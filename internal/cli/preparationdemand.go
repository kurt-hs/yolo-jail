package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

type preparationRequest struct {
	ID           string
	RegisteredAt time.Time
	Keys         []preparationRecord
	Boundaries   map[string]packsrc.CheckStart
	demands      map[string]*sharedDemandRequest
}

// Manual authorization changes automatic timing, not user selection or inputs.
func selectedFreshPreparation(ctx context.Context) ([]packload.Fork, error) {
	sel := selectHostPacks(func(e config.PackEntry) (*packload.Pack, error) {
		return resolveConfiguredPackWithSpec(e, config.ResolvePackSpec{Ctx: ctx, NoWait: true, Detached: true})
	}, config.UserScopeSelection())
	if problems := sel.problems(); len(problems) > 0 {
		return nil, fmt.Errorf("fresh preparation selection unavailable: %v — run `yolo check --no-build` and retry", problems)
	}
	return append(packload.PatchedTrees(sel.packs), packload.Forks(sel.packs)...), nil
}

func newPreparationRequest(ctx context.Context, keys []packload.Fork, runtime, platform string) (*preparationRequest, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	req := &preparationRequest{ID: hex.EncodeToString(nonce), RegisteredAt: time.Now(), Boundaries: map[string]packsrc.CheckStart{}}
	local := *patchedAdvanceStore(true)
	local.Ctx = ctx
	store := &local
	for _, f := range keys {

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

func registerPreparationDemand(ctx context.Context, req *preparationRequest) (func(), error) {
	req.demands = make(map[string]*sharedDemandRequest)
	release := func() {
		for _, r := range req.demands {
			r.release(false)
		}
	}
	for _, sub := range req.Keys {
		boundary, identity, err := req.Boundary(sub.Key)
		if err != nil {
			release()
			return nil, err
		}
		d := preparationDemand{Key: sub.Key, Runtime: sub.Runtime, Platform: sub.Platform, Identity: identity, Fresh: true, CheckBoundary: boundary, Host: true}
		r, err := registerSharedDemand(ctx, d)
		if err != nil {
			release()
			return nil, err
		}
		req.demands[sub.Key] = r
	}
	return release, nil
}
func waitFreshPreparation(ctx context.Context, req run.PreparationRequest, key string) (advanceResult, error) {
	if err := ctx.Err(); err != nil {
		return advanceResult{}, err
	}
	request, ok := req.(*preparationRequest)
	if !ok {
		return advanceResult{}, errors.New("fresh request is not attached; retry --update")
	}
	demand := request.demands[key]
	if demand == nil {
		return advanceResult{}, errors.New("fresh requester lease missing; retry --update")
	}
	result, err := demand.wait(ctx)
	if err != nil {
		return advanceResult{}, err
	}
	res := result.advance()
	if res.failed {
		if res.pending != nil {
			return res, res.pending
		}
		return res, fmt.Errorf("fresh preparation failed: %s — repair and retry --update", res.freshFailure())
	}
	if err := request.Guard(key, func() error { return nil }); err != nil {
		return advanceResult{}, err
	}
	return res, qualifyPreparationResultContext(ctx, request, key, res)
}
func qualifyPreparationResult(req *preparationRequest, key string, res advanceResult) error {
	return qualifyPreparationResultContext(context.Background(), req, key, res)
}
func qualifyPreparationResultContext(ctx context.Context, req *preparationRequest, key string, res advanceResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	boundary, identity, err := req.Boundary(key)
	if err != nil {
		return err
	}
	record, err := patchedAdvanceStore(true).LoadCheckRecord(key)
	if err != nil {
		return err
	}
	if res.failed || record.Check == nil || record.Check.FetchErr != "" || record.Check.Problem != "" || !record.Fresh.Qualifies(boundary, identity) {
		return errors.New("fresh check did not qualify the explicit request; repair and retry --update, or launch without --update for ordinary recovery")
	}
	return req.Guard(key, func() error { return nil })
}
