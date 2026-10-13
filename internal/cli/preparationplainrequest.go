package cli

import (
	"context"
	"errors"
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

func freshPlainServing(f packload.Fork, sub preparationSubscription) (advanceResult, error) {
	record, err := patchedAdvanceStore(true).LoadCheckRecord(f.Key())
	if err != nil {
		return advanceResult{}, err
	}
	if record.Good == nil {
		return advanceResult{}, errors.New("plain source has no admitted preparation; retry --update")
	}
	pin := packload.LoadForkPins([]packload.Fork{f}, forkLockPath())[0]
	b := forkBuild{Fork: f, Commit: pin.Commit, Platform: sub.Platform}
	if pin.Commit == "" || record.Good.Commit != pin.Commit || record.Good.Recipe != b.recipe() {
		return advanceResult{}, errors.New("plain source pin or recipe changed; retry --update")
	}
	entry, _, err := resolveForkBuild(&capture.Store{Dir: paths.CapturesDir()}, f.Bin, sub.Platform, f.Source, pin.Commit, b.recipe())
	if err != nil {
		return advanceResult{}, err
	}
	if entry.Key != record.Good.Entry {
		return advanceResult{}, errors.New("plain source admission changed; retry --update")
	}
	return advanceResult{delivery: entrypoint.ForkDelivery{Key: entry.Key}, good: record.Good}, nil
}

func runFreshPlainPreparation(ctx context.Context, f packload.Fork, request *preparationRequest, sub preparationSubscription, guard preparationDemandGuard, w io.Writer) (advanceResult, error) {
	store := *patchedAdvanceStore(true)
	store.Ctx = ctx
	boundary, identity, err := request.Boundary(f.Key())
	if err != nil {
		return advanceResult{}, err
	}
	// A guarded completed winner can satisfy concurrent requests registered before
	// its start. Exact pin/build validation below remains independently mandatory.
	if record, e := store.LoadCheckRecord(f.Key()); e == nil && record.Fresh.Qualifies(boundary, identity) && record.Check != nil && record.Check.FetchErr == "" && record.Check.Problem == "" {
		if res, e := freshPlainServing(f, sub); e == nil {
			return res, qualifyPreparationResultContext(ctx, request, f.Key(), res)
		}
	}
	var begun packsrc.CheckStart
	publication := func(commit func() error) error {
		return store.WithFreshAuthority(f.Key(), boundary, &begun, identity, func() error { return guard.Guard(f.Key(), commit) })
	}
	pin, err := resolveFreshPlainProgram(ctx, f, func() error {
		var err error
		begun, err = store.BeginFreshCheck(f.Key(), identity, request.ID, boundary, func(commit func() error) error { return guard.Guard(f.Key(), commit) })
		return err
	}, publication, w)
	if err != nil {
		return advanceResult{}, err
	}
	// The resolver's pin is published under its own guard. Pair completed source
	// evidence only afterwards; a crash between the two never qualifies a request.
	err = store.WithCheckRecord(f.Key(), nil, func(r *packsrc.CheckRecord, readErr error, save func() error) (bool, error) {
		if readErr != nil {
			return false, readErr
		}
		return false, publication(func() error {
			r.Seq++
			r.Fresh = &begun
			r.Read, _, _, _ = f.CheckWant(nil).Inputs()
			r.Check = &packsrc.CheckFound{Seq: r.Seq, At: begun.StartedAt.Unix(), List: []packsrc.ListEntry{{Commit: pin.Commit}}}
			return save()
		})
	})
	if err != nil {
		return advanceResult{}, err
	}
	b := forkBuild{Fork: f, Commit: pin.Commit, Platform: sub.Platform}
	var settleErr error
	entry, err := buildFork(b, buildMode{runtime: sub.Runtime, lock: pidlock.Mode{Wait: true, Bound: forkBuildWaitBound, Cancel: ctx.Done()}, packs: &store, runJail: func(staging string, b forkBuild, s captureStreams) int {
		rc, _ := forkBuildChild(ctx, forkBuildWaitBound, staging, b, s, false)
		return rc
	}, settle: func(entry *capture.Entry, buildErr error) {
		if buildErr != nil || entry == nil {
			return
		}
		settleErr = store.WithCheckRecord(f.Key(), nil, func(r *packsrc.CheckRecord, readErr error, save func() error) (bool, error) {
			if readErr != nil {
				return false, readErr
			}
			if r.Fresh == nil || (r.Fresh.Epoch != begun.Epoch || r.Fresh.Seq != begun.Seq || r.Fresh.InputIdentity != begun.InputIdentity) {
				return false, errors.New("plain preparation check winner changed; retry --update")
			}
			return false, publication(func() error {
				r.Good = &packsrc.GoodBuild{Commit: pin.Commit, Recipe: b.recipe(), Entry: entry.Key, Seq: r.Seq, At: begun.StartedAt.Unix()}
				return save()
			})
		})
	}}, w, w, false)
	if err != nil {
		return advanceResult{}, err
	}
	if settleErr != nil {
		return advanceResult{}, settleErr
	}
	if entry == nil {
		return advanceResult{}, errors.New("plain preparation produced no admitted build; retry --update")
	}
	return freshPlainServing(f, sub)
}
