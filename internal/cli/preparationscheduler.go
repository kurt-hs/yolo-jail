package cli

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
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/notty"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

const preparationSchedulerVerb = "preparation-scheduler"
const preparationTickInterval = time.Minute

var errPreparationWorkerFromTest = errors.New("a test binary does not self-exec the preparation scheduler")

// preparationSpawn is replaceable in in-process tests; it never self-execs the test binary.
var preparationSpawn = func() error {
	if flag.Lookup("test.v") != nil {
		return nil
	}
	return startPreparationScheduler()
}

func preparationInputIdentity(f packload.Fork, rt, platform string) (string, error) {
	series, err := f.ReadSeries()
	if err != nil {
		return "", err
	}
	identity := struct {
		Key, Pack, Owner, Follow, Recipe, Runtime, Platform string
		Platforms                                           []string
	}{f.Key(), f.Pack, f.Owner, f.Follow, run.PatchedRecipe(f, series.Digest), rt, platform, f.Platforms}
	b, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type preparationPatchInspection struct {
	failure *packsrc.PatchFailure
	err     error
}

func preparationEligible(f packload.Fork, req run.TreeBuildRequest) bool {
	return req.Build && f.IsTree() && treeUpdateTiming(f) == timingNextLaunch && run.BackgroundBuildsOn(req.Runtime)
}

// registerTreePreparation persists the selected tree set after pool registration, even when no
// subscription is due. It records no executable source paths: the worker resolves the user config.
func registerTreePreparation(trees []packload.Fork, req run.TreeBuildRequest, w io.Writer) error {
	if !req.Build {
		return nil
	}
	inspections := make(map[string]preparationPatchInspection, len(trees))
	for _, f := range trees {
		if f.IsTree() {
			failure, inspectErr := preparationPatchFailure(context.Background(), f)
			inspections[f.Key()] = preparationPatchInspection{failure: failure, err: inspectErr}
		}
	}
	var spawn bool
	err := withPreparationState(func(state *preparationState) error {
		for i := range state.Subscriptions {
			r := &state.Subscriptions[i]
			if r.Runtime == req.Runtime && r.Platform == req.Platform {
				if _, selected := inspections[r.Key]; selected {
					continue // selected keys are updated only after their check-record authority is known
				}
				r.Active, r.Reason = false, "not selected for next-launch preparation by the current user config"
			}
		}
		for _, f := range trees {
			if !f.IsTree() {
				continue
			}
			key := f.Key()
			idx := preparationRecordIndex(*state, key, req.Runtime, req.Platform)
			if idx < 0 {
				state.Subscriptions = append(state.Subscriptions, preparationRecord{Key: key, Runtime: req.Runtime,
					Platform: req.Platform, Phase: "inactive"})
				idx = len(state.Subscriptions) - 1
			}
			r := &state.Subscriptions[idx]
			inspection := inspections[key]
			if inspection.err != nil {
				continue // contention, cancellation and unreadable authority are not a fresh result
			}
			if failure := inspection.failure; failure != nil {
				r.Active, r.Phase = false, "failed"
				r.Reason = failure.Error() + " — repair the patch failure before resuming `yolo pack prepare start`"
				r.UpdatedAt = preparationNow()
				continue
			}
			identity, identityErr := preparationInputIdentity(f, req.Runtime, req.Platform)
			if identityErr != nil {
				r.Active, r.Phase = false, "invalid"
				r.Reason = fmt.Sprintf("could not read the selected tree declaration (%v) — repair the pack, then run `yolo pack prepare start`", identityErr)
				continue
			}
			eligible := preparationEligible(f, req)
			if r.InputIdentity != identity {
				r.Generation++
				r.InputIdentity = identity
				r.Phase, r.ServingEntry, r.At, r.RetryAt = "pending", "", time.Time{}, time.Time{}
			}
			r.Active = eligible
			if eligible {
				r.Reason, r.Phase = "", "pending"
				spawn = true
			} else if treeUpdateTiming(f) != timingNextLaunch {
				r.Reason, r.Phase = "held", "inactive"
			} else {
				r.Reason, r.Phase = runtimeName(req.Runtime)+" does not support background tree preparation — use a fresh launch to prepare it", "unsupported"
			}
			r.UpdatedAt = preparationNow()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if spawn {
		if err := ensurePreparationScheduler(); err != nil {
			fmt.Fprintf(w, "⚠ next-launch tree preparation was recorded, but its host worker could not start (%v) — run `yolo pack prepare start` to retry\n", err)
			return err
		}
	}
	return nil
}

func preparationSchedulerArgv() []string { return []string{"internal", preparationSchedulerVerb} }

func startPreparationScheduler() error {
	if flag.Lookup("test.v") != nil {
		return errPreparationWorkerFromTest
	}
	if err := os.MkdirAll(filepath.Dir(backgroundAdvanceLog()), 0o700); err != nil {
		return err
	}
	f, err := notty.OpenDetachLog(backgroundAdvanceLog())
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := preparationSchedulerProcess()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, f, f
	return notty.StartDetached(cmd)
}

func preparationSchedulerProcess() *exec.Cmd {
	cmd := exec.Command(run.SelfExecPath(), preparationSchedulerArgv()...)
	cmd.Env = currentPreparationEnvironment()
	return cmd
}

var preparationStartLock = filepath.Join(paths.BackgroundAdvanceDir(), "scheduler-start.lock")

func ensurePreparationScheduler() error {
	lock, err := pidlock.Acquire(preparationStartLock, pidlock.Wait, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	if pidlock.Held(preparationWorkerLockPath()) {
		state, stateErr := loadPreparationState()
		if stateErr != nil {
			return stateErr
		}
		pid := pidlock.Holder(preparationWorkerLockPath())
		if state.Worker.PID != pid || state.Worker.Identity == "" {
			return fmt.Errorf("scheduler lease is held by pid %d, but no matching worker identity is recorded; state was retained and no process was signalled", pid)
		}
		return notifyPreparationScheduler()
	}
	if err := os.Remove(preparationStopPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := notifyPreparationScheduler(); err != nil {
		return err
	}
	if err := preparationSpawn(); err != nil {
		_ = os.Remove(preparationWakePath())
		return err
	}
	return nil
}

func notifyPreparationScheduler() error {
	if err := os.MkdirAll(filepath.Dir(preparationWakePath()), 0700); err != nil {
		return err
	}
	return os.WriteFile(preparationWakePath(), []byte("wake\n"), 0600)
}

func runPreparationSchedulerCommand() int {
	if config.InJail() {
		fmt.Fprintln(os.Stderr, "yolo internal preparation-scheduler: runs on the host only — run `yolo pack prepare start` on the host")
		return 2
	}
	lease, err := pidlock.Acquire(preparationWorkerLockPath(), pidlock.NoWait, nil)
	if err != nil {
		return 0
	}
	defer lease.Release()
	exe, _ := os.Executable()
	identity, digest := preparationExecutableIdentity(exe)
	if identity == "" {
		identity = fmt.Sprintf("pid:%d", os.Getpid())
	}
	if err := withPreparationState(func(state *preparationState) error {
		state.Worker = preparationWorker{Identity: identity, PID: os.Getpid(), Executable: exe, BinaryDigest: digest}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, backgroundSignals...)
	defer signal.Stop(signals)
	ticker := time.NewTicker(preparationTickInterval)
	defer ticker.Stop()
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	fmt.Fprintf(os.Stdout, "=== preparation scheduler started %s (yolo pid %d)\n", preparationNow().Format(time.RFC3339), os.Getpid())
	err = runPreparationScheduler(ctx, ticker.C, os.Stdout)
	_ = withPreparationState(func(state *preparationState) error {
		for i := range state.Subscriptions {
			record := &state.Subscriptions[i]
			if err != nil && !errors.Is(err, context.Canceled) && record.Active {
				record.Phase = "binary-mismatch"
				record.Reason = err.Error()
				continue
			}
			if record.Phase != "checking" {
				continue
			}
			record.Phase = "interrupted"
			record.At = preparationNow()
			record.Reason = "the owned worker stopped before this act settled; admitted build records remain authoritative — run `yolo pack prepare start` to resume"
		}
		if state.Worker.Identity == identity {
			state.Worker = preparationWorker{StoppedAt: preparationNow()}
		}
		return nil
	})
	_ = os.Remove(preparationStopPath())
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func preparationExecutableIdentity(exe string) (string, string) {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	return resolved + "#" + digest, digest
}

type preparationTask struct {
	cancel     context.CancelFunc
	generation uint64
	baseline   uint64
}
type preparationTaskResult struct {
	key, runtime, platform string
	generation             uint64
	baseline               uint64
	err                    error
}

// runPreparationScheduler is the timer production loop. Tests inject ticks into this exact loop.
func runPreparationScheduler(ctx context.Context, ticks <-chan time.Time, w io.Writer) error {
	done := make(chan preparationTaskResult, 32)
	more := make(chan struct{}, 1)
	tickEvents := make(chan time.Time)
	pumpCtx, stopPump := context.WithCancel(ctx)
	defer stopPump()
	go func() {
		defer close(tickEvents)
		for {
			select {
			case tick, ok := <-ticks:
				if !ok {
					return
				}
				select {
				case tickEvents <- tick:
				case <-pumpCtx.Done():
					return
				}
			case <-more:
				select {
				case tickEvents <- time.Time{}:
				case <-pumpCtx.Done():
					return
				}
			case <-pumpCtx.Done():
				return
			}
		}
	}()
	tasks := map[string]preparationTask{}
	var releaseSnapshot func()
	defer func() {
		if releaseSnapshot != nil {
			releaseSnapshot()
		}
	}()
	attempted := map[string]bool{}
	defer func() {
		for _, task := range tasks {
			task.cancel()
		}
		for range tasks {
			<-done
		}
	}()
	control := time.NewTicker(time.Second)
	defer control.Stop()
	keyID := func(r preparationRecord) string { return r.Key + "\x00" + r.Runtime + "\x00" + r.Platform }
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-done:
			id := result.key + "\x00" + result.runtime + "\x00" + result.platform
			delete(tasks, id)
			reprocess := true
			_ = preparationUpdateRecord(result.key, result.runtime, result.platform, func(r *preparationRecord) {
				if r.Generation != result.generation || !r.Active {
					return
				}
				r.At = preparationNow()
				if outcome, err := readBackgroundOutcome(backgroundOutcomePath(r.Key)); err == nil &&
					outcome.Generation > result.baseline && outcome.Key == r.Key && outcome.ProducerPID == os.Getpid() && outcome.SubscriptionGeneration == result.generation && outcome.Pid == 0 && outcome.State != bgRunning {
					phase := outcome.State
					switch phase {
					case bgMoved:
						phase = "admitted"
					case bgNothing:
						phase = "unchanged"
					case bgSkipped:
						phase = "busy"
					}
					r.Phase, r.ServingEntry = phase, outcome.To
					r.Log = outcome.Log
					r.Reason = outcome.Error
					if r.Reason == "" {
						r.Reason = outcome.Retained
					}
					r.RetryAt = time.Time{}
					if outcome.RetryAt > 0 {
						r.RetryAt = time.Unix(outcome.RetryAt, 0)
					}
				} else if result.err != nil {
					r.Phase, r.Reason = "failed", result.err.Error()
				} else {
					r.Phase, r.Reason = "busy", "another authorized advance owns this key; the next timer tick retries without changing its successful check stamp"
				}
			})
			if reprocess {
				select {
				case more <- struct{}{}:
				default:
				}
			}
		case <-control.C:
			if preparationStopRequested() {
				return nil
			}
			if err := preparationCheckBinaryIdentity(); err != nil {
				return err
			}
			if _, err := os.Stat(preparationWakePath()); err == nil {
				_ = os.Remove(preparationWakePath())
				select {
				case more <- struct{}{}:
				default:
				}
			}
		case tick, ok := <-tickEvents:
			if !ok {
				return nil
			}
			if !tick.IsZero() {
				attempted = map[string]bool{}
			}
			state, stateErr := preparationSubscriptions()
			if stateErr != nil {
				return stateErr
			}
			if len(state) == 0 {
				return nil
			}
			if releaseSnapshot != nil {
				releaseSnapshot()
			}
			current, release, configErr := scopedPreparationTrees(ctx, state)
			releaseSnapshot = release
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(configErr, packsrc.ErrLockHeld) {
				continue
			}
			if configErr != nil {
				for _, r := range state {
					if r.Active {
						_ = preparationUpdateRecord(r.Key, r.Runtime, r.Platform, func(rec *preparationRecord) {
							rec.Active = false
							rec.Phase = "invalid-config"
							rec.Reason = fmt.Sprintf("%v — repair %s, then run `yolo pack prepare start`", configErr, paths.UserConfigPath())
						})
					}
				}
				return configErr
			}
			state = refreshPreparationSubscriptionsUnder(ctx, state, current)
			eligible := false
			for _, record := range state {
				eligible = eligible || record.Active
			}
			if !eligible && len(tasks) == 0 {
				return nil
			}
			for id, task := range tasks {
				var matching bool
				for _, r := range state {
					if keyID(r) == id && r.Active && r.Generation == task.generation {
						matching = true
						break
					}
				}
				if !matching {
					task.cancel()
				}
			}
			for _, r := range state {
				if !r.Active || !run.BackgroundBuildsOn(r.Runtime) || len(tasks) > 0 {
					continue
				}
				if !r.RetryAt.IsZero() && preparationNow().Before(r.RetryAt) {
					continue
				}
				id := keyID(r)
				if _, busy := tasks[id]; busy || attempted[id] {
					continue
				}
				f, ok := current[id]
				if !ok {
					continue
				}
				failure, inspectErr := preparationPatchFailure(ctx, f)
				if inspectErr != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					continue // unknown authority is retried; it must not revoke or refresh this subscription
				}
				if failure != nil {
					_ = preparationUpdateRecord(r.Key, r.Runtime, r.Platform, func(rec *preparationRecord) {
						rec.Active, rec.Phase = false, "failed"
						rec.Reason = failure.Error() + " — repair the patch failure before resuming `yolo pack prepare start`"
					})
					continue
				}
				advance, early := newAdvance(f, advanceOptions{platform: r.Platform, runtime: r.Runtime, background: true, launch: true})
				if early != nil {
					_ = preparationUpdateRecord(r.Key, r.Runtime, r.Platform, func(rec *preparationRecord) {
						rec.Phase = "unresolved"
						rec.Reason = early.problem + " — run `yolo pack status " + r.Key + "` and repair the selected declaration"
					})
					continue
				}
				if !advance.wantsBackground() {
					continue
				}
				baseline := uint64(0)
				if old, err := readBackgroundOutcome(backgroundOutcomePath(r.Key)); err == nil {
					baseline = old.Generation
				}
				attempted[id] = true
				taskCtx, cancel := context.WithCancel(ctx)
				tasks[id] = preparationTask{cancel: cancel, generation: r.Generation, baseline: baseline}
				_ = preparationUpdateRecord(r.Key, r.Runtime, r.Platform, func(rec *preparationRecord) {
					rec.Phase, rec.Reason, rec.At, rec.RetryAt = "checking", "", preparationNow(), time.Time{}
					rec.Log = backgroundAdvanceLog()
				})
				go func(r preparationRecord, baseline uint64) {
					var out strings.Builder
					runBackgroundAdvanceUnder(taskCtx, backgroundArgs{runtime: r.Runtime, platform: r.Platform, keys: []string{r.Key}, schedulerGeneration: r.Generation}, &out)
					if out.Len() > 0 {
						_, _ = io.WriteString(w, out.String())
					}
					cancel()
					done <- preparationTaskResult{key: r.Key, runtime: r.Runtime, platform: r.Platform, generation: r.Generation, baseline: baseline}
				}(r, baseline)
			}

		}
	}
}

func currentPreparationTrees(records []preparationRecord) (map[string]packload.Fork, error) {
	// Compatibility for short-lived callers. Persistent readers use an explicitly owned scope.
	return preparationTreesWithResolver(records, resolveConfiguredPack)
}

func scopedPreparationTrees(ctx context.Context, records []preparationRecord) (map[string]packload.Fork, func(), error) {
	resolver, release := preparationScopedResolver(ctx)
	current, err := preparationTreesWithResolver(records, resolver)
	return current, release, err
}

func preparationTreesWithResolver(records []preparationRecord, resolver func(config.PackEntry) (*packload.Pack, error)) (map[string]packload.Fork, error) {
	var contention error
	sel := selectHostPacks(func(e config.PackEntry) (*packload.Pack, error) {
		p, err := resolver(e)
		if errors.Is(err, packsrc.ErrLockHeld) {
			contention = err
		}
		return p, err
	}, config.UserScopeSelection())
	if contention != nil {
		return nil, contention
	}
	if problems := sel.problems(); len(problems) > 0 {
		var lines []string
		for _, problem := range problems {
			lines = append(lines, problem.Name+": "+problem.Reason)
		}
		return nil, fmt.Errorf("the current user pack selection is incomplete (%s)", strings.Join(lines, "; "))
	}
	want := map[string]bool{}
	for _, record := range records {
		want[record.Key+"\x00"+record.Runtime+"\x00"+record.Platform] = true
	}
	out := map[string]packload.Fork{}
	for _, f := range packload.PatchedTrees(sel.packs) {
		for id := range want {
			parts := strings.SplitN(id, "\x00", 3)
			if len(parts) == 3 && parts[0] == f.Key() {
				out[id] = f
			}
		}
	}
	return out, nil
}

func refreshPreparationSubscriptions(records []preparationRecord, current map[string]packload.Fork) []preparationRecord {
	return refreshPreparationSubscriptionsUnder(context.Background(), records, current)
}

func refreshPreparationSubscriptionsUnder(ctx context.Context, records []preparationRecord, current map[string]packload.Fork) []preparationRecord {
	for _, old := range records {
		id := old.Key + "\x00" + old.Runtime + "\x00" + old.Platform
		f, ok := current[id]
		var failure *packsrc.PatchFailure
		var inspectErr error
		if ok {
			failure, inspectErr = preparationPatchFailure(ctx, f)
		}
		_ = preparationUpdateRecord(old.Key, old.Runtime, old.Platform, func(r *preparationRecord) {
			if inspectErr != nil {
				return // unknown check-record authority cannot overwrite prior failure, freshness or backoff
			}
			if !ok {
				r.Active = false
				r.Phase = "unresolved"
				r.Reason = "the user-configured pack selection does not authorize this tree for background preparation; select its owner pack or run `yolo capture " + r.Key + "`"
				return
			}
			identity, err := preparationInputIdentity(f, r.Runtime, r.Platform)
			if err != nil {
				r.Active = false
				r.Phase = "invalid"
				r.Reason = fmt.Sprintf("%v — repair the selected pack, then run `yolo pack prepare start`", err)
				return
			}
			if failure != nil {
				r.Active = false
				r.Phase = "failed"
				r.Reason = failure.Error() + " — repair the patch failure before resuming `yolo pack prepare start`"
				return
			}
			if identity != r.InputIdentity {
				r.Generation++
				r.InputIdentity = identity
				r.Phase = "pending"
				r.At = time.Time{}
				r.ServingEntry = ""
				r.Reason = ""
				r.RetryAt = time.Time{}
			}
			if treeUpdateTiming(f) != timingNextLaunch {
				r.Active = false
				r.Phase = "held"
				r.Reason = "the current `agent_updates` policy does not select next-launch preparation — change the user config to resume"
				return
			}
			if !run.BackgroundBuildsOn(r.Runtime) {
				r.Active = false
				r.Phase = "unsupported"
				r.Reason = runtimeName(r.Runtime) + " does not support background tree preparation — use a fresh launch to prepare it"
				return
			}
			if !r.Active {
				r.Reason = ""
			}
			r.Active = true
		})
	}
	updated, _ := preparationSubscriptions()
	return updated
}

func preparationStopRequested() bool {
	contents, err := os.ReadFile(preparationStopPath())
	if err != nil {
		return false
	}
	state, err := loadPreparationState()
	if err != nil {
		return false
	}
	return state.Worker.PID == os.Getpid() && state.Worker.Identity != "" && strings.TrimSpace(string(contents)) == state.Worker.Identity
}

var errPreparationAdmissionStale = errors.New("tree preparation inputs or selection changed before admission — the current selection will be retried")

func preparationAdmissionGuard(key, rt, platform string, generation uint64) func(func() error) error {
	return preparationAdmissionGuardUnder(context.Background(), key, rt, platform, generation)
}

func preparationAdmissionGuardUnder(ctx context.Context, key, rt, platform string, generation uint64) func(func() error) error {
	return func(commit func() error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Nonwaiting resolution happens before acquiring the subscription guard.
		current, release, err := scopedPreparationTrees(ctx, []preparationRecord{{Key: key, Runtime: rt, Platform: platform}})
		defer release()
		if err != nil {
			return errPreparationAdmissionStale
		}
		id := key + "\x00" + rt + "\x00" + platform
		f, ok := current[id]
		if !ok || treeUpdateTiming(f) != timingNextLaunch || !run.BackgroundBuildsOn(rt) {
			return errPreparationAdmissionStale
		}
		failure, inspectErr := preparationPatchFailure(ctx, f)
		if inspectErr != nil {
			return inspectErr // held is retryable; unknown and cancellation are not revocation
		}
		if failure != nil {
			return errPreparationAdmissionStale
		}
		identity, err := preparationInputIdentity(f, rt, platform)
		if err != nil {
			return errPreparationAdmissionStale
		}
		lock, err := pidlock.Acquire(preparationStateLockPath(), pidlock.NoWait, nil)
		if err != nil {
			if errors.Is(err, pidlock.ErrHeld) {
				return packsrc.ErrLockHeld
			}
			return err
		}
		defer lock.Release()
		state, err := loadPreparationState()
		if err != nil {
			return err
		}
		i := preparationRecordIndex(state, key, rt, platform)
		if i < 0 {
			return errPreparationAdmissionStale
		}
		record := state.Subscriptions[i]
		if !record.Active || record.Generation != generation {
			return errPreparationAdmissionStale
		}
		if record.InputIdentity != identity {
			return errPreparationAdmissionStale
		}
		if preparationStopRequested() {
			return errPreparationAdmissionStale
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return commit()
	}
}

func preparationCheckBinaryIdentity() error {
	state, err := loadPreparationState()
	if err != nil {
		return err
	}
	if state.Worker.BinaryDigest == "" || state.Worker.Executable == "" {
		return nil
	}
	_, digest := preparationExecutableIdentity(state.Worker.Executable)
	if digest == "" || digest != state.Worker.BinaryDigest {
		return errors.New("yolo binary changed while the preparation worker was running — its work was cancelled; run `yolo pack prepare start` to restart with the selected binary")
	}
	return nil
}
