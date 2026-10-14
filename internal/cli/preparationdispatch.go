package cli

import (
	"context"
	"fmt"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"io"
)

func treePreparationSubscription(f packload.Fork, rt, platform string) (preparationRecord, error) {
	identity, err := freshPreparationIdentity(f, platform)
	return preparationRecord{Key: f.Key(), Runtime: rt, Platform: platform, InputIdentity: identity}, err
}

// Presence in subscriptions is not authorization. Every dispatcher and settle
// resolves the current user selection with nonwaiting source locks.
func selectedPreparationTrees(ctx context.Context) ([]packload.Fork, error) {
	sel := selectHostPacks(func(e config.PackEntry) (*packload.Pack, error) {
		return resolveConfiguredPackWithSpec(e, config.ResolvePackSpec{Ctx: ctx, NoWait: true, Detached: true})
	}, config.UserScopeSelection())
	if p := sel.problems(); len(p) > 0 {
		return nil, fmt.Errorf("preparation selection unavailable: %v — run `yolo check --no-build` and retry", p)
	}
	var trees []packload.Fork
	for _, f := range append(packload.PatchedTrees(sel.packs), packload.PatchedForks(sel.packs)...) {
		timing := treeUpdateTiming(f)
		if !f.IsTree() {
			timing = forkUpdateTiming(f)
		}
		if timing == timingNextLaunch && run.PatchedForkHold(f) == "" {
			trees = append(trees, f)
		}
	}
	return trees, nil
}

func currentPreparationTree(ctx context.Context, expected preparationRecord) (packload.Fork, error) {
	trees, err := selectedPreparationTrees(ctx)
	if err != nil {
		return packload.Fork{}, err
	}
	for _, f := range trees {
		if f.Key() != expected.Key {
			continue
		}
		identity, err := preparationInputIdentity(f, expected.Runtime, expected.Platform)
		if !f.IsTree() {
			identity, err = preparationForkIdentity(f, expected.PinCommit, expected.Runtime, expected.Platform)
		}
		if err != nil {
			return packload.Fork{}, err
		}
		if identity == expected.InputIdentity {
			return f, nil
		}
	}
	return packload.Fork{}, errPreparationAdmissionStale
}

func registerPreparationForks(forks []packload.Fork, runtime, platform string, w io.Writer) error {
	var trees []packload.Fork
	var pins []packload.ForkPin
	for _, f := range forks {
		if f.IsTree() {
			trees = append(trees, f)
		} else {
			pins = append(pins, packload.ForkPin{Fork: f})
		}
	}
	if err := registerHostTreePreparation(trees, w); err != nil {
		return err
	}
	return registerForkPreparation(pins, run.ForkBuildRequest{Runtime: runtime, Platform: platform}, w, false)
}
