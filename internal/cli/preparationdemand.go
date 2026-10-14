package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// A requester holds a kernel lease, not a PID claim. Worker ownership is the
// existing scheduler's lease. Request expiry bounds abandoned/unknown demands;
// an explicit retry creates a new boundary, never renews historical freshness.
const preparationRequestLifetime = 30 * time.Minute

type preparationDemand struct {
	Request   preparationRequest
	ExpiresAt time.Time
}
type preparationAnswer struct {
	Fresh        preparationFreshness
	Error        string
	PatchFailure *packsrc.PatchFailure
	Good         *packsrc.GoodBuild
	// AppleBuildPending preserves an Apple Container required-build admission
	// refusal (appleBuildPending) across the worker/launch boundary: the
	// worker's settle absorbs the refusal into a retained result, so without
	// this marker the waiter could only rebuild an untyped error from Error
	// and the launch's FreshFailure would never see the pending reason.
	AppleBuildPending *applePendingRefusal `json:",omitempty"`
}

// applePendingRefusal is the JSON half of an appleBuildPending: the reason
// the launch names and the capture it names it for.
type applePendingRefusal struct {
	Reason  string
	Capture string
}

// applePendingOf returns the required-build admission refusal carried by the
// worker's hook or the plain preparation's returned error, nil when neither
// is one. The patched advance's settle serves the good build and retains the
// refusal, so the hook's capture is the only typed copy on that path; the
// plain preparation returns the gate's error directly.
func applePendingOf(errs ...error) *applePendingRefusal {
	for _, err := range errs {
		var pending *appleBuildPending
		if errors.As(err, &pending) {
			return &applePendingRefusal{Reason: pending.reason, Capture: pending.capture}
		}
	}
	return nil
}

func demandPath(id string) string { return filepath.Join(preparationRoot(), "requests", id+".json") }
func demandLeasePath(id string) string {
	return filepath.Join(preparationRoot(), "requests", id+".lease")
}
func answerPath(id, key string) string {
	return filepath.Join(preparationRoot(), "answers", id, preparationKeySlug(key)+".json")
}

func withPreparationDemands(ctx context.Context, noWait bool, fn func() error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	mode := pidlock.Mode{Wait: !noWait, Bound: 5 * time.Second}
	if ctx != nil {
		mode.Cancel = ctx.Done()
	}
	lock, err := pidlock.Acquire(filepath.Join(preparationRoot(), "requests.lock"), mode, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}
func registerPreparationDemand(ctx context.Context, req *preparationRequest) (func(), error) {
	if len(req.Keys) == 0 {
		return func() {}, nil
	}
	if err := ensurePreparationRoot(preparationRoot()); err != nil {
		return nil, err
	}
	lease, err := pidlock.Acquire(demandLeasePath(req.ID), pidlock.NoWait, nil)
	if err != nil {
		return nil, err
	}
	err = withPreparationDemands(ctx, false, func() error {
		return writePreparationJSON(demandPath(req.ID), preparationDemand{*req, time.Now().Add(preparationRequestLifetime)})
	})
	if err != nil {
		lease.Release()
		return nil, err
	}
	release := func() {
		_ = withPreparationDemands(context.Background(), false, func() error {
			err := os.Remove(demandPath(req.ID))
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		})
		lease.Release()
	}
	if err = startTreePreparation(ctx); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
func readPreparationDemands() ([]preparationDemand, error) {
	files, err := filepath.Glob(filepath.Join(preparationRoot(), "requests", "*.json"))
	if err != nil {
		return nil, err
	}
	var demands []preparationDemand
	for _, path := range files {
		var demand preparationDemand
		if err := readPreparationJSON(path, &demand); err != nil {
			return nil, fmt.Errorf("fresh request state unreadable: %w — inspect `yolo pack prepare status` and retry", err)
		}
		if demand.Request.ID == "" || demand.ExpiresAt.IsZero() {
			return nil, errors.New("fresh request authority incomplete; inspect scheduler requests and retry")
		}
		if time.Now().After(demand.ExpiresAt) {
			continue
		}
		f, err := os.OpenFile(demandLeasePath(demand.Request.ID), os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			continue
		}
		f.Close()
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, err
		}
		demands = append(demands, demand)
	}
	return demands, nil
}

// Demand guard authorizes any surviving request for these exact inputs, not
// merely the first request that woke the worker. A later waiter keeps the act alive.
type preparationDemandGuard struct {
	request *preparationRequest
	sub     preparationSubscription
}

func (g preparationDemandGuard) Boundary(key string) (packsrc.CheckStart, string, error) {
	return g.request.Boundary(key)
}
func (g preparationDemandGuard) Guard(key string, commit func() error) error {
	if err := g.request.Guard(key, func() error { return nil }); err != nil {
		return err
	}
	return withPreparationDemands(context.TODO(), true, func() error {
		demands, err := readPreparationDemands()
		if err != nil {
			return err
		}
		for _, d := range demands {
			for _, sub := range d.Request.Keys {
				if sub == g.sub {
					return commit()
				}
			}
		}
		return errors.New("fresh preparation has no remaining demand; retry --update")
	})
}

func runFreshPreparationDemands(ctx context.Context, w interface{ Write([]byte) (int, error) }) error {
	demands, err := readPreparationDemands()
	if err != nil {
		return err
	}
	for _, d := range demands {
		for _, sub := range d.Request.Keys {
			var prior preparationAnswer
			if readPreparationJSON(answerPath(d.Request.ID, sub.Key), &prior) == nil {
				continue
			}
			request := d.Request
			guard := preparationDemandGuard{&request, sub}
			keys, err := selectedFreshPreparation(ctx)
			var fork packload.Fork
			if err == nil {
				for _, f := range keys {
					if f.Key() == sub.Key {
						fork = f
						break
					}
				}
				if fork.Key() != sub.Key {
					err = errors.New("fresh preparation owner removed; retry with current selection")
				}
			}
			var res advanceResult
			keyCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-keyCtx.Done():
						return
					case <-ticker.C:
						if err := guard.Guard(sub.Key, func() error { return nil }); err != nil && !errors.Is(err, pidlock.ErrHeld) && !errors.Is(err, packsrc.ErrLockHeld) {
							cancel()
							return
						}
					}
				}
			}()
			if err == nil {
				err = guard.Guard(sub.Key, func() error { return nil })
			}
			var requiredBuildErr error
			if err == nil {
				if !fork.Patched() && !fork.IsTree() {
					res, err = runFreshPlainPreparation(keyCtx, fork, &request, sub, guard, w)
				} else {
					res = advancePatchedFork(fork, advanceOptions{freshCheck: true, preparation: guard, platform: sub.Platform, runtime: sub.Runtime, launch: true, ctx: keyCtx, operationCtx: keyCtx, out: w, errw: w, requiredBuildFailure: func(hookErr error) { requiredBuildErr = hookErr }, admitGuard: func(commit func() error) error {
						rec, e := patchedAdvanceStore(true).LoadCheckRecord(sub.Key)
						if e != nil {
							return e
						}
						boundary, identity, e := request.Boundary(sub.Key)
						if e != nil {
							return e
						}
						return patchedAdvanceStore(true).WithFreshAuthority(sub.Key, boundary, rec.Fresh, identity, func() error { return guard.Guard(sub.Key, commit) })
					}})
				}
				if err == nil {
					err = qualifyPreparationResultContext(keyCtx, &request, sub.Key, res)
				}
			}
			cancel()
			<-done
			answer := preparationAnswer{Fresh: preparationFreshness{RequestID: request.ID, Key: sub.Key, InputIdentity: sub.InputIdentity, ServingEntry: res.delivery.Key}, Good: res.good, PatchFailure: res.patchFailure}
			if err != nil {
				answer.Error = err.Error()
			}
			if pending := applePendingOf(requiredBuildErr, err); pending != nil {
				answer.AppleBuildPending = pending
			}
			if err = writePreparationJSON(answerPath(request.ID, sub.Key), answer); err != nil {
				return err
			}
		}
	}
	return nil
}
func qualifyPreparationResult(req *preparationRequest, key string, res advanceResult) error {
	return qualifyPreparationResultContext(context.Background(), req, key, res)
}
func qualifyPreparationResultContext(ctx context.Context, req *preparationRequest, key string, res advanceResult) error {
	if res.failed || res.fellShort || res.skipped != "" || res.retained != "" || res.operationError != "" || res.problem != "" || res.patchFailure != nil || res.delivery.Key == "" {
		return fmt.Errorf("fresh preparation failed (%s%s%s); repair the reported cause and retry --update, or launch without --update", res.operationError, res.problem, res.delivery.Reason)
	}
	boundary, identity, err := req.Boundary(key)
	if err != nil {
		return err
	}
	store := *patchedAdvanceStore(true)
	store.Ctx = ctx
	return store.WithCheckRecord(key, nil, func(rec *packsrc.CheckRecord, readErr error, _ func() error) (bool, error) {
		if readErr != nil {
			return false, readErr
		}
		if !rec.Fresh.Qualifies(boundary, identity) || rec.Check == nil || rec.Check.FetchErr != "" || rec.Check.Problem != "" {
			return false, errors.New("no successful qualifying fresh check; retry --update or launch without --update")
		}
		return false, store.WithFreshAuthority(key, boundary, rec.Fresh, identity, func() error { return req.Guard(key, func() error { return nil }) })
	})
}

func waitFreshPreparation(ctx context.Context, req run.PreparationRequest, key string) (advanceResult, error) {
	request, ok := req.(*preparationRequest)
	if !ok {
		return advanceResult{}, errors.New("fresh request missing; retry --update")
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return advanceResult{}, ctx.Err()
		}
		if time.Since(request.RegisteredAt) > preparationRequestLifetime {
			return advanceResult{}, errors.New("fresh request expired; retry --update")
		}
		var answer preparationAnswer
		err := readPreparationJSON(answerPath(request.ID, key), &answer)
		if err == nil {
			if answer.Error != "" {
				if answer.AppleBuildPending != nil {
					return advanceResult{patchFailure: answer.PatchFailure}, &appleBuildPending{
						reason:  answer.AppleBuildPending.Reason,
						capture: answer.AppleBuildPending.Capture,
					}
				}
				return advanceResult{patchFailure: answer.PatchFailure}, errors.New(answer.Error)
			}
			res := advanceResult{delivery: entrypoint.ForkDelivery{Key: answer.Fresh.ServingEntry}, good: answer.Good, patchFailure: answer.PatchFailure}
			// Consume the current admitted winner, not a pointer cached in the scheduler answer.
			selected, e := selectedFreshPreparation(ctx)
			if e != nil {
				return advanceResult{}, e
			}
			for _, sub := range request.Keys {
				if sub.Key == key {
					for _, f := range selected {
						if f.Key() == key {
							if !f.Patched() && !f.IsTree() {
								res, e = freshPlainServing(f, sub)
								if e != nil {
									return advanceResult{}, e
								}
							} else {
								res, _ = servingTreeOf(f, advanceOptions{platform: sub.Platform, runtime: sub.Runtime, out: io.Discard, errw: io.Discard, readOnlyInitialization: true}, "", false)
							}
						}
					}
				}
			}
			return res, qualifyPreparationResultContext(ctx, request, key, res)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return advanceResult{}, err
		}
		if time.Since(request.RegisteredAt) > 5*time.Second {
			_, live, e := probePreparationWorker(preparationRoot())
			if live != preparationAlive {
				return advanceResult{}, fmt.Errorf("fresh preparation worker unavailable (%v); run `yolo pack prepare start` and retry --update", e)
			}
		}
		select {
		case <-ctx.Done():
			return advanceResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func preparationKeySlug(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
func readPreparationJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
