package cli

import (
	"context"
	"errors"
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

func freshPlainServing(f packload.Fork, sub preparationDemand) (advanceResult, error) {
	record, err := patchedAdvanceStore(true).LoadCheckRecord(f.Key())
	if err != nil {
		return advanceResult{}, err
	}
	return freshPlainServingRecord(f, sub, record)
}

func freshPlainServingRecord(f packload.Fork, sub preparationDemand, record *packsrc.CheckRecord) (advanceResult, error) {
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

func runFreshPlainPreparation(ctx context.Context, f packload.Fork, sub preparationDemand, guard func(func() error) error, w io.Writer) (advanceResult, error) {
	store := *patchedAdvanceStore(true)
	store.Ctx = ctx
	boundary, identity := sub.CheckBoundary, sub.Identity
	// A guarded completed winner can satisfy concurrent requests registered before
	// its start. Exact pin/build validation below remains independently mandatory.
	var reused advanceResult
	var completed bool
	err := store.WithCheckRecord(f.Key(), nil, func(record *packsrc.CheckRecord, readErr error, _ func() error) (bool, error) {
		if readErr != nil {
			return false, readErr
		}
		if record.Fresh.Qualifies(boundary, identity) && record.Check != nil && record.Check.FetchErr == "" && record.Check.Problem == "" {
			if res, e := freshPlainServingRecord(f, sub, record); e == nil {
				completed = true
				return false, store.WithFreshAuthority(f.Key(), boundary, record.Fresh, identity, func() error {
					return guard(func() error { reused = res; return nil })
				})
			}
		}
		return false, nil
	})
	if err != nil || completed {
		return reused, err
	}
	var begun packsrc.CheckStart
	publication := func(commit func() error) error {
		return store.WithFreshAuthority(f.Key(), boundary, &begun, identity, func() error { return guard(commit) })
	}
	pin, err := resolveFreshPlainProgram(ctx, f, func() error {
		var err error
		begun, err = store.BeginFreshCheck(f.Key(), identity, sub.Key, boundary, func(commit func() error) error { return guard(commit) })
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
	casAdmission := func(commit func() error) error {
		return store.WithCheckRecord(f.Key(), nil, func(r *packsrc.CheckRecord, readErr error, _ func() error) (bool, error) {
			if readErr != nil {
				return false, readErr
			}
			if r.Fresh == nil || r.Fresh.Epoch != begun.Epoch || r.Fresh.Seq != begun.Seq || r.Fresh.InputIdentity != identity {
				return false, errPreparationAdmissionStale
			}
			return false, publication(commit)
		})
	}
	var settleErr error
	entry, err := buildFork(b, buildMode{runtime: sub.Runtime, admitCtx: ctx, lock: pidlock.Mode{Wait: true, Bound: forkBuildWaitBound, Cancel: ctx.Done()}, packs: &store, admitGuard: casAdmission, sourceCheckoutCtx: ctx, sourceCheckoutNoWait: true, runJail: func(staging string, b forkBuild, s captureStreams) int {
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

// Plain forks use the same requester lease and independently owned worker as patched
// sources. Workspace-only declarations retain bounded foreground ownership.
func freshPlainPreparationForLaunch(ctx context.Context, f packload.Fork, req run.ForkBuildRequest, w io.Writer) entrypoint.ForkDelivery {
	refuse := func(err error) entrypoint.ForkDelivery {
		return entrypoint.ForkDelivery{FreshFailure: err.Error(), FreshError: err, Reason: err.Error()}
	}
	if err := req.FreshBoundaryErr[f.Key()]; err != nil {
		return refuse(err)
	}
	d := preparationDemand{Key: f.Key(), Runtime: req.Runtime, Platform: req.Platform, Identity: req.FreshIdentity[f.Key()], CheckBoundary: req.FreshBoundary[f.Key()], Fresh: true}
	_, release, err := resolveDemandFork(ctx, d)
	release()
	if errors.Is(err, packsrc.ErrLockHeld) {
		return refuse(err)
	}
	if err != nil && !errors.Is(err, errSharedPreparationForegroundOnly) {
		return refuse(err)
	}
	var res advanceResult
	if errors.Is(err, errSharedPreparationForegroundOnly) || !run.BackgroundBuildsOn(req.Runtime) {
		guard := func(commit func() error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			identity, err := freshPreparationIdentity(f, req.Platform)
			if err != nil {
				return err
			}
			if identity != d.Identity {
				return errPreparationAdmissionStale
			}
			return commit()
		}
		res, err = runFreshPlainPreparation(ctx, f, d, guard, w)
	} else {
		// Resolution proves user-scope ownership; never use a staged path as worker input.
		var result preparationDemandResult
		result, err = waitSharedDemandResult(ctx, d, false, nil)
		res = result.advance()
	}
	if err != nil {
		return refuse(err)
	}
	if res.failed {
		if res.pending != nil {
			return refuse(res.pending)
		}
		return refuse(errors.New(res.freshFailure()))
	}
	return res.delivery
}
