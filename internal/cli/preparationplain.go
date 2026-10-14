package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// resolveFreshPlainProgram is the explicit request's owner-scoped source check,
// not a timer resolver or a force-build. The shared worker supplies both hooks:
// check-start is recorded under forklock before mirror work; only authorized pin
// publication runs in commitGuard. Neither hook may wait for work needing forklock.
func resolveFreshPlainProgram(ctx context.Context, f packload.Fork, beforeCheck func() error, commitGuard func(func() error) error, w io.Writer) (packload.ForkPin, error) {
	pin := packload.ForkPin{Fork: f}
	if ctx == nil || beforeCheck == nil || commitGuard == nil {
		return pin, errors.New("fresh plain-program resolution requires worker context, check-start boundary and admission guard — retry the explicit update through the shared preparation worker")
	}
	if f.Patched() || f.IsTree() {
		return pin, errors.New("fresh plain-program resolution requires a plain program — use the selected owner's patched advance instead")
	}
	addr, err := packsrc.Parse(f.Source)
	if err != nil {
		return pin, err
	}
	store := &packsrc.Store{Dir: paths.PacksDir(), Ctx: ctx, Detached: true}
	err = packsrc.WithForkLockGuarded(ctx, store.Dir, forkLockPath(), func(line string) { fmt.Fprintln(w, line) }, func(lock *packsrc.ForkLock) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := beforeCheck(); err != nil {
			return false, err
		}
		// Refresh's Force bypasses source stamps only. Inspect its fetch failure rather
		// than certifying the cached fallback that legacy Sync intentionally permits.
		outcomes, err := store.Refresh([]packsrc.RefreshPack{{Name: f.Key(), Source: f.Source}}, packsrc.RefreshOptions{Force: true})
		if err != nil {
			return false, err
		}
		if len(outcomes) != 1 {
			return false, errors.New("source refresh did not return the requested program")
		}
		outcome := outcomes[0]
		if outcome.FetchErr != nil {
			return false, outcome.FetchErr
		}
		if outcome.Err != nil {
			return false, outcome.Err
		}
		if outcome.Commit == "" {
			return false, errors.New("source refresh returned no commit")
		}
		if err := ensureForkCheckout(store, addr, outcome.Commit); err != nil {
			return false, err
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		previous, _ := lock.Get(f.Key())
		next := packsrc.ForkLockEntry{Key: f.Key(), Source: f.Source, Ref: addr.Ref, Commit: outcome.Commit}
		pin.Commit = outcome.Commit
		lock.Set(next)
		return previous != next, nil
	}, commitGuard)
	if err != nil {
		pin.Commit = ""
		return pin, fmt.Errorf("fresh source check for %s failed: %w — run `yolo pack update` on the host after repairing the cause, then retry the explicit launch update", f.Key(), err)
	}
	return pin, nil
}

// HOST uses the same strict resolver and current user-declaration authority as
// plain preparation. Its existing floor builder still owns host-platform delivery.
func freshHostPlainSource(ctx context.Context, f packload.Fork, platform string, w io.Writer) error {
	store := *patchedAdvanceStore(true)
	store.Ctx = ctx
	identities, boundaries, errs := map[string]string{}, map[string]packsrc.CheckStart{}, map[string]error{}
	if err := registerFreshPreparation(ctx, &store, f, platform, identities, boundaries, errs); err != nil {
		return err
	}
	d := preparationDemand{Key: f.Key(), Platform: platform, Identity: identities[f.Key()], CheckBoundary: boundaries[f.Key()], Fresh: true, Host: true}
	guard := func(commit func() error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, release, err := resolveDemandFork(ctx, d)
		defer release()
		if err != nil {
			return err
		}
		return commit()
	}
	var begun packsrc.CheckStart
	finish := func(commit func() error) error {
		return store.WithCheckRecord(f.Key(), nil, func(_ *packsrc.CheckRecord, readErr error, _ func() error) (bool, error) {
			if readErr != nil {
				return false, readErr
			}
			return false, store.WithFreshAuthority(f.Key(), d.CheckBoundary, &begun, d.Identity, func() error { return guard(commit) })
		})
	}
	_, err := resolveFreshPlainProgram(ctx, f, func() error {
		var err error
		begun, err = store.BeginFreshCheck(f.Key(), d.Identity, f.Key(), d.CheckBoundary, guard)
		return err
	}, finish, w)
	if err != nil {
		return err
	}
	// An unchanged pin skips publication. Every successful result still needs
	// finish-time authority before HOST can ensure or execute the floor copy.
	return finish(func() error { return nil })
}
