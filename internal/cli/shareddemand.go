package cli

// Request leases belong to launch processes; production belongs to the detached host worker.
// The kernel releases a lease on requester exit. A PID or an old request file is never demand.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/notty"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

const sharedDemandPoll = 50 * time.Millisecond

var errSharedPreparationForegroundOnly = errors.New("the user-scope selection does not authorize detached preparation for this declaration")

type preparationDemand struct {
	Key, Runtime, Platform, Identity string
	AfterSeq                         uint64
	SubscriptionGeneration           uint64
	Fresh, Host, Background          bool
}

type preparationDemandResult struct {
	Delivery                                                entrypoint.ForkDelivery
	Built, Failed, Bypassed, Lost, FellShort                bool
	OperationError                                          string
	PatchFailure                                            *packsrc.PatchFailure
	Good                                                    *packsrc.GoodBuild
	From, To, FailWhat, FailErr, Skipped, Retained, Problem string
	RetryAt                                                 time.Time
}

func demandResult(r advanceResult) preparationDemandResult {
	return preparationDemandResult{r.delivery, r.built, r.failed, r.bypassed, r.lost, r.fellShort, r.operationError, r.patchFailure, r.good, r.from, r.to, r.failWhat, r.failErr, r.skipped, r.retained, r.problem, r.retryAt}
}
func (r preparationDemandResult) advance() advanceResult {
	return advanceResult{delivery: r.Delivery, built: r.Built, failed: r.Failed, bypassed: r.Bypassed, lost: r.Lost, fellShort: r.FellShort, operationError: r.OperationError, patchFailure: r.PatchFailure, good: r.Good, from: r.From, to: r.To, failWhat: r.FailWhat, failErr: r.FailErr, skipped: r.Skipped, retained: r.Retained, problem: r.Problem, retryAt: r.RetryAt}
}
func sharedDemandDir(key, rt, platform string) string {
	sum := sha256.Sum256([]byte(key + "\x00" + rt + "\x00" + platform))
	return filepath.Join(paths.BackgroundAdvanceDir(), "demands", hex.EncodeToString(sum[:]))
}
func writeDemandJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".publish-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func withDemandState(ctx context.Context, dir string, wait bool, fn func() error) error {
	mode := pidlock.NoWait
	if wait {
		mode = pidlock.Mode{Wait: true, Cancel: ctx.Done()}
	}
	lock, err := pidlock.Acquire(filepath.Join(dir, "state.lock"), mode, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	if err = ctx.Err(); err != nil {
		return err
	}
	return fn()
}

// demandLeaseHeld is deliberately tri-state: unreadable is not an expired requester.
func demandLeaseHeld(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

type livePreparationDemand struct {
	path string
	preparationDemand
}

// Called only in the short state transaction. Expired requests are removed only after a
// positive unlocked witness; absent/unreadable leases refuse dispatch, never cancel live work.
func livePreparationDemands(dir string) ([]livePreparationDemand, error) {
	files, err := filepath.Glob(filepath.Join(dir, "request-*.json"))
	if err != nil {
		return nil, err
	}
	var live []livePreparationDemand
	for _, path := range files {
		held, err := demandLeaseHeld(path + ".lease")
		if err != nil {
			return nil, err
		}
		if !held {
			_ = os.Remove(path)
			_ = os.Remove(path + ".result")
			_ = os.Remove(path + ".lease")
			continue
		}
		if _, err := os.Stat(path + ".result"); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		var d preparationDemand
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
		if sharedDemandDir(d.Key, d.Runtime, d.Platform) != dir {
			return nil, errors.New("preparation demand binding changed — preserve the demand record and retry `yolo --update`")
		}
		live = append(live, livePreparationDemand{path, d})
	}
	return live, nil
}

var sharedPreparationSpawn = func(ctx context.Context, d preparationDemand) error {
	if flag.Lookup("test.v") != nil {
		return errBackgroundFromTest
	}
	if err := os.MkdirAll(filepath.Dir(backgroundAdvanceLog()), 0700); err != nil {
		return err
	}
	environment, err := sharedPreparationEnvironment(ctx)
	if err != nil {
		return err
	}
	f, err := notty.OpenDetachLog(backgroundAdvanceLog())
	if err != nil {
		return err
	}
	defer f.Close()
	argv := backgroundAdvanceArgv([]string{d.Key}, d.Runtime, d.Platform)
	argv = append(argv, "--shared-demand")
	cmd := exec.Command(run.SelfExecPath(), argv...)
	cmd.Env = environment
	cmd.Stdin = nil
	cmd.Stdout = f
	cmd.Stderr = f
	return notty.StartDetached(cmd)
}

func (a *advance) waitSharedPreparation(ctx context.Context) advanceResult {
	o := a.o
	if ctx.Err() != nil {
		return a.freshRefusal("the preparation requester was interrupted", ctx.Err())
	}
	identity, err := freshPreparationIdentity(a.f, o.platform)
	if err != nil {
		return a.freshRefusal("could not register shared preparation", err)
	}
	if o.freshCheck && identity != o.freshIdentity {
		return a.freshRefusal("selected inputs changed after registration", errPreparationAdmissionStale)
	}
	d := preparationDemand{Key: a.f.Key(), Runtime: o.runtime, Platform: o.platform, Identity: identity, AfterSeq: o.freshAfterSeq, Fresh: o.freshCheck, Host: o.host, Background: o.background, SubscriptionGeneration: o.sharedSubscriptionGeneration}
	if d.Runtime == "" {
		d.Runtime = captureRuntime()
	}
	probe := d
	probe.Identity = ""
	selected, drop, selectionErr := resolveDemandFork(ctx, probe)
	var selectedIdentity string
	if selectionErr == nil {
		selectedIdentity, selectionErr = freshPreparationIdentity(selected, d.Platform)
	}
	drop()
	if errors.Is(selectionErr, errSharedPreparationForegroundOnly) || selectionErr == nil && selectedIdentity != d.Identity {
		a.dim("%s: foreground-only preparation; the user selection does not authorize a detached worker for these inputs — select this declaration in the user config to share preparation", a.f.Label())
		foreground := o
		foreground.sharedForegroundOnly = true
		foreground.ctx = ctx
		return advancePatchedFork(a.f, foreground)
	}
	if selectionErr != nil && !errors.Is(selectionErr, packsrc.ErrLockHeld) {
		return a.freshRefusal("could not authorize shared preparation", selectionErr)
	}
	dir := sharedDemandDir(d.Key, d.Runtime, d.Platform)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return a.freshRefusal("could not create requester lease", err)
	}
	var path string
	var previousWorkerError []byte
	var lease *pidlock.Lock
	err = withDemandState(ctx, dir, true, func() error {
		previousWorkerError, _ = os.ReadFile(filepath.Join(dir, "worker-error.json"))
		f, e := os.CreateTemp(dir, "request-*.json")
		if e != nil {
			return e
		}
		path = f.Name()
		_ = f.Close()
		lease, e = pidlock.Acquire(path+".lease", pidlock.NoWait, nil)
		if e != nil {
			_ = os.Remove(path)
			return e
		}
		return writeDemandJSON(path, d)
	})
	if lease != nil {
		defer func() {
			lease.Release()
			// Remove only this detached request under the same transaction the worker reads.
			// Cleanup is bounded and never waits on an artifact/check/build lock.
			cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer stop()
			_ = withDemandState(cleanupCtx, dir, true, func() error {
				_ = os.Remove(path)
				_ = os.Remove(path + ".result")
				_ = os.Remove(path + ".lease")
				return nil
			})
			if o.sharedAutomatic && ctx.Err() != nil {
				// Scheduler stop drains a producer only when no other requester owns it.
				// A fresh launch's detach is never made to wait for that teardown.
				ticker := time.NewTicker(sharedDemandPoll)
				defer ticker.Stop()
				for {
					var live []livePreparationDemand
					err := withDemandState(cleanupCtx, dir, false, func() error { var e error; live, e = livePreparationDemands(dir); return e })
					if err == nil && len(live) > 0 {
						break
					}
					if !pidlock.Held(filepath.Join(dir, "worker.lock")) {
						break
					}
					select {
					case <-cleanupCtx.Done():
						return
					case <-ticker.C:
					}
				}
			}
		}()
	}
	if err != nil {
		return a.freshRefusal("could not attach preparation demand", err)
	}
	if o.freshCheck {
		_ = notifyPreparationScheduler()
	}
	// Launch processes never acquire the production/key/build lock. Registration can finish even
	// while transport, a build or another request's STARTED transaction is paused.
	if err = sharedPreparationSpawn(ctx, d); err != nil {
		return a.freshRefusal("the owned preparation worker could not start", err)
	}
	a.dim("%s: waiting for pid %d's shared preparation — interrupting detaches this requester", a.f.Label(), pidlock.Holder(filepath.Join(dir, "worker.lock")))
	ticker := time.NewTicker(sharedDemandPoll)
	defer ticker.Stop()
	waitBound := time.NewTimer(forkBuildWaitBound)
	defer waitBound.Stop()
	seenWorker := false
	for {
		if ctx.Err() != nil {
			return a.freshRefusal("the preparation requester was interrupted", ctx.Err())
		}
		b, e := os.ReadFile(path + ".result")
		if e == nil {
			var result preparationDemandResult
			if e = json.Unmarshal(b, &result); e != nil {
				return a.freshRefusal("could not read shared preparation outcome", e)
			}
			if ctx.Err() != nil {
				return a.freshRefusal("the preparation requester was interrupted", ctx.Err())
			}
			r := result.advance()
			if r.failed {
				return r
			}
			// Re-read guarded serving authority and record THIS launch's hand, not the worker's hand.
			if current, e := freshPreparationIdentity(a.f, o.platform); e != nil || current != d.Identity {
				return a.freshRefusal("selected inputs changed before handoff", errPreparationAdmissionStale)
			}
			handoffOptions := o
			handoffOptions.operationCtx = ctx
			handoff, early := newAdvance(a.f, handoffOptions)
			if early != nil {
				return *early
			}
			handed := handoff.finish(nil, forkBuild{}, 0, nil, r.delivery.Reason)
			if handed.failed {
				return handed
			}
			r.delivery = handed.delivery
			r.good = handed.good
			return r
		}
		if !errors.Is(e, os.ErrNotExist) {
			return a.freshRefusal("could not read shared preparation outcome", e)
		}
		workerError, readErr := os.ReadFile(filepath.Join(dir, "worker-error.json"))
		if readErr == nil && string(workerError) != string(previousWorkerError) {
			var failure struct{ Error string }
			if e = json.Unmarshal(workerError, &failure); e != nil {
				return a.freshRefusal("the owned worker outcome is unreadable", e)
			}
			return a.freshRefusal("the owned preparation worker stopped", errors.New(failure.Error))
		}
		held, probeErr := demandLeaseHeld(filepath.Join(dir, "worker.lock"))
		seenWorker = seenWorker || probeErr == nil && held
		if probeErr == nil && !held {
			if seenWorker {
				return a.freshRefusal("the owned preparation worker exited without a usable outcome", errors.New("worker stopped"))
			}
		}
		select {
		case <-ctx.Done():
			return a.freshRefusal("the preparation requester was interrupted", ctx.Err())
		case <-waitBound.C:
			return a.freshRefusal("waiting for owned preparation timed out", pidlock.ErrTimedOut)
		case <-ticker.C:
		}
	}
}

// resolveDemandFork obtains executable authority only from the selected user declarations, not
// from a request, staged manifest, or persistent result. Contention is unknown authorization.
func resolveDemandFork(ctx context.Context, d preparationDemand) (packload.Fork, func(), error) {
	resolver, release := preparationScopedResolver(ctx)
	var contention error
	sel := selectHostPacks(func(e config.PackEntry) (*packload.Pack, error) {
		p, err := resolver(e)
		if errors.Is(err, packsrc.ErrLockHeld) {
			contention = err
		}
		return p, err
	}, config.UserScopeSelection())
	if contention != nil {
		return packload.Fork{}, release, contention
	}
	if problems := sel.problems(); len(problems) > 0 {
		return packload.Fork{}, release, fmt.Errorf("current user selection is incomplete — repair %s, then retry `yolo --update`", paths.UserConfigPath())
	}
	forks := append(packload.PatchedForks(sel.packs), packload.PatchedTrees(sel.packs)...)
	for _, f := range forks {
		if f.Key() == d.Key {
			identity, err := freshPreparationIdentity(f, d.Platform)
			if err != nil {
				return f, release, err
			}
			platform := strings.SplitN(d.Platform, "/", 2)
			if len(platform) != 2 || !(packdecl.Install{Platforms: f.Platforms}).SupportsPlatform(platform[0], platform[1]) {
				return f, release, errPreparationAdmissionStale
			}
			if d.Identity != "" && identity != d.Identity {
				return f, release, errPreparationAdmissionStale
			}
			if d.SubscriptionGeneration != 0 {
				state, e := loadPreparationState()
				if e != nil {
					return f, release, e
				}
				idx := preparationRecordIndex(state, d.Key, d.Runtime, d.Platform)
				if idx < 0 || !state.Subscriptions[idx].Active || state.Subscriptions[idx].Generation != d.SubscriptionGeneration {
					return f, release, errPreparationAdmissionStale
				}
			}
			if !d.Fresh && d.Background && treeUpdateTiming(f) != timingNextLaunch {
				return f, release, errPreparationAdmissionStale
			}
			return f, release, nil
		}
	}
	return packload.Fork{}, release, errSharedPreparationForegroundOnly
}

// The worker reads yolo's own usable host-floor copy; no requester-supplied path or receipt
// can establish installed bytes. This preserves unchanged freshness when its store copy is gone.
func sharedInstalledCopy(ctx context.Context, d preparationDemand) *installedCopy {
	resolver, release := preparationScopedResolver(ctx)
	defer release()
	selected := selectHostPacks(resolver, config.UserScopeSelection())
	if len(selected.problems()) > 0 || ctx.Err() != nil {
		return nil
	}
	programs := floorPrograms(selected.packs)
	floor := newHostFloor(io.Discard, programs)
	for _, p := range programs {
		if p.Install.IsPatchedFork() && floorForkBuild(p, "").Fork.Key() == d.Key {
			status := floor.Status(p)
			if status.Disposition == hostfloor.Provisioned {
				return floorServingCopy(status.Record)
			}
		}
	}
	return nil
}

// Only positively authorized live requesters can admit. Unknown authorization keeps the
// child alive for a later read, but is never permission to publish its result.
func authorizedPreparationDemands(ctx context.Context, dir string, identity string) (map[string]bool, error) {
	var live []livePreparationDemand
	err := withDemandState(ctx, dir, false, func() error { var e error; live, e = livePreparationDemands(dir); return e })
	if err != nil {
		return nil, err
	}
	authorized := map[string]bool{}
	var unknown error
	for _, r := range live {
		if r.Identity == identity {
			_, release, e := resolveDemandFork(ctx, r.preparationDemand)
			release()
			if e == nil {
				authorized[r.path] = true
			} else if errors.Is(e, packsrc.ErrLockHeld) {
				unknown = e
			}
		}
	}
	if len(authorized) > 0 {
		return authorized, nil
	}
	if unknown != nil {
		return nil, unknown
	}
	return nil, errPreparationAdmissionStale
}

// A sleeping scheduler can own timer demand without a concurrent dispatch goroutine. Its
// recorded selection alone is insufficient: require the kernel-held worker lease and the
// matching worker identity, then resolve/revalidate the current policy and declaration.
func sharedTimerDemand(ctx context.Context, d preparationDemand) (*preparationRecord, error) {
	held, err := demandLeaseHeld(preparationWorkerLockPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, nil
	}
	state, err := loadPreparationState()
	if err != nil {
		return nil, err
	}
	idx := preparationRecordIndex(state, d.Key, d.Runtime, d.Platform)
	if idx < 0 || !state.Subscriptions[idx].Active {
		return nil, nil
	}
	if state.Worker.Identity == "" || state.Worker.PID != pidlock.Holder(preparationWorkerLockPath()) {
		return nil, errors.New("scheduler demand ownership is unreadable")
	}
	stop, err := os.ReadFile(preparationStopPath())
	if err == nil && strings.TrimSpace(string(stop)) == state.Worker.Identity {
		return nil, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	r := state.Subscriptions[idx]
	automatic := d
	automatic.Fresh = false
	automatic.Background = true
	automatic.SubscriptionGeneration = r.Generation
	f, release, err := resolveDemandFork(ctx, automatic)
	defer release()
	if errors.Is(err, errPreparationAdmissionStale) || errors.Is(err, errSharedPreparationForegroundOnly) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	identity, err := preparationInputIdentity(f, r.Runtime, r.Platform)
	if err != nil {
		return nil, err
	}
	if identity != r.InputIdentity {
		return nil, nil
	}
	return &r, nil
}

// The caller already holds the artifact's check-record transaction. Do not reacquire that
// record to inspect a failure: the existing advance owns that check/replay/settle authority.
// Revalidate the independently owned subscription with a nonwaiting metadata transaction.
func commitSubscribedPreparation(ctx context.Context, d preparationDemand, commit func() error) error {
	f, release, err := resolveDemandFork(ctx, d)
	defer release()
	if err != nil {
		return err
	}
	identity, err := preparationInputIdentity(f, d.Runtime, d.Platform)
	if err != nil {
		return err
	}
	lock, err := pidlock.Acquire(preparationStateLockPath(), pidlock.NoWait, nil)
	if errors.Is(err, pidlock.ErrHeld) {
		return packsrc.ErrLockHeld
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := loadPreparationState()
	if err != nil {
		return err
	}
	idx := preparationRecordIndex(state, d.Key, d.Runtime, d.Platform)
	if idx < 0 {
		return errPreparationAdmissionStale
	}
	r := state.Subscriptions[idx]
	if !r.Active || r.Generation != d.SubscriptionGeneration || r.InputIdentity != identity {
		return errPreparationAdmissionStale
	}
	stop, e := os.ReadFile(preparationStopPath())
	if e == nil && state.Worker.Identity != "" && strings.TrimSpace(string(stop)) == state.Worker.Identity {
		return context.Canceled
	}
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return commit()
}

func demandAdmissionGuard(ctx context.Context, dir string, d preparationDemand) func(func() error) error {
	return func(commit func() error) error {
		authorized, err := authorizedPreparationDemands(ctx, dir, d.Identity)
		timer, timerErr := sharedTimerDemand(ctx, d)
		if err != nil && timer == nil {
			return err
		}
		if timerErr != nil && len(authorized) == 0 {
			return timerErr
		}
		return withDemandState(ctx, dir, false, func() error {
			live, err := livePreparationDemands(dir)
			if err != nil {
				return err
			}
			for _, r := range live {
				if authorized[r.path] && r.Identity == d.Identity && r.SubscriptionGeneration == 0 {
					return commit()
				}
			}
			for _, r := range live {
				if authorized[r.path] && r.Identity == d.Identity {
					return commitSubscribedPreparation(ctx, r.preparationDemand, commit)
				}
			}
			if timer != nil {
				automatic := d
				automatic.Fresh = false
				automatic.Background = true
				automatic.SubscriptionGeneration = timer.Generation
				return commitSubscribedPreparation(ctx, automatic, commit)
			}
			return context.Canceled
		})
	}
}

// runSharedPreparation uses the existing check/replay/build/admission pipeline in a process
// whose signal context is owned by the worker, never a launch. The short metadata lock is not
// held across resolution, transport, check/build locks, child teardown, or runtime inspection.
func runSharedPreparation(ctx context.Context, d preparationDemand, w io.Writer) (resultErr error) {
	dir := sharedDemandDir(d.Key, d.Runtime, d.Platform)
	owner, err := pidlock.Acquire(filepath.Join(dir, "worker.lock"), pidlock.NoWait, nil)
	if errors.Is(err, pidlock.ErrHeld) {
		return nil
	}
	if err != nil {
		return err
	}
	defer owner.Release()
	defer func() {
		if resultErr != nil {
			_ = writeDemandJSON(filepath.Join(dir, "worker-error.json"), struct {
				At    int64
				PID   int
				Error string
			}{time.Now().UnixNano(), os.Getpid(), resultErr.Error()})
		}
	}()
	for {
		var live []livePreparationDemand
		err = withDemandState(ctx, dir, true, func() error {
			var e error
			live, e = livePreparationDemands(dir)
			if e == nil && len(live) == 0 {
				owner.Release()
			}
			return e
		})
		if err != nil {
			return err
		}
		if len(live) == 0 {
			return nil
		}
		request := live[0].preparationDemand
		// Batch only identical declarations. A fresh request may join a check only if the STARTED
		// sequence is after its boundary. Requests arriving during transport get the next batch.
		for _, r := range live {
			if r.Identity == request.Identity && r.Fresh && (!request.Fresh || r.AfterSeq > request.AfterSeq) {
				request = r.preparationDemand
			}
		}
		f, release, resolveErr := resolveDemandFork(ctx, request)
		if errors.Is(resolveErr, packsrc.ErrLockHeld) {
			release()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sharedDemandPoll):
			}
			continue
		}
		var res advanceResult
		if resolveErr != nil {
			res = advanceResult{failed: true, problem: resolveErr.Error(), delivery: entrypoint.ForkDelivery{Reason: resolveErr.Error()}}
		} else {
			workCtx, cancel := context.WithCancel(ctx)
			monitorDone := make(chan struct{})
			go func() {
				defer close(monitorDone)
				ticker := time.NewTicker(sharedDemandPoll)
				defer ticker.Stop()
				for {
					select {
					case <-workCtx.Done():
						return
					case <-ticker.C:
					}
					var pending []livePreparationDemand
					e := withDemandState(workCtx, dir, false, func() error { var err error; pending, err = livePreparationDemands(dir); return err })
					if e != nil {
						continue
					} // lock contention/read failure is unknown, not zero demand
					keep := false
					for _, r := range pending {
						if r.Identity == request.Identity {
							keep = true
						}
					}
					timer, timerErr := sharedTimerDemand(workCtx, request)
					if !keep && timer == nil {
						if timerErr != nil {
							var syntax *json.SyntaxError
							if !errors.As(timerErr, &syntax) {
								continue
							} // unknown lease liveness is not zero demand
						}
						cancel()
						return
					}
					_, e = authorizedPreparationDemands(workCtx, dir, request.Identity)
					if e != nil && timer == nil && !errors.Is(e, packsrc.ErrLockHeld) {
						cancel()
						return
					}
				}
			}()
			opts := advanceOptions{runtime: request.Runtime, platform: request.Platform, launch: true, host: request.Host, background: request.Background, freshCheck: request.Fresh, freshIdentity: request.Identity, freshAfterSeq: request.AfterSeq, ctx: workCtx, out: w, errw: w, pool: newAdvancePool(request.Runtime), sharedOwned: true, admitGuard: demandAdmissionGuard(workCtx, dir, request)}
			if request.Host && !f.IsTree() {
				hostOnly := true
				for _, r := range live {
					if r.Identity == request.Identity && !r.Host {
						hostOnly = false
					}
				}
				if hostOnly {
					opts.installed = sharedInstalledCopy(workCtx, request)
				}
			}
			res = advancePatchedFork(f, opts)
			cancel()
			<-monitorDone
		}
		release()
		err = withDemandState(ctx, dir, true, func() error {
			pending, e := livePreparationDemands(dir)
			if e != nil {
				return e
			}
			for _, r := range pending {
				if r.Identity != request.Identity || r.Fresh && (!request.Fresh || r.AfterSeq > request.AfterSeq) {
					continue
				}
				if request.Host && !f.IsTree() && !r.Host && r.Fresh && res.delivery.Key == "" && !res.failed && res.skipped == "" {
					continue
				} // an installed host copy cannot fulfill a jail artifact demand
				if e = writeDemandJSON(r.path+".result", demandResult(res)); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
}

func runSharedPreparationCommand(a backgroundArgs, w io.Writer) int {
	if len(a.keys) != 1 {
		fmt.Fprintln(w, "shared preparation requires exactly one selected key — retry `yolo --update`")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), backgroundSignals...)
	defer stop()
	if err := runSharedPreparation(ctx, preparationDemand{Key: a.keys[0], Runtime: a.runtime, Platform: a.platform}, w); err != nil {
		fmt.Fprintf(w, "shared preparation stopped: %v — retry `yolo --update`\n", err)
		return 1
	}
	return 0
}
