package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

const preparationSchedulerVerb = "preparation-scheduler"

func preparationRoot() string { return filepath.Join(paths.BackgroundAdvanceDir(), "scheduler") }

// Identity includes current user authorization and the declaration/series bytes,
// never a staged path. A policy/credential declaration edit conservatively invalidates work.
func treePreparationSubscription(f packload.Fork, rt, platform string) (preparationSubscription, error) {
	cfg, err := os.ReadFile(paths.UserConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return preparationSubscription{}, err
	}
	series, err := f.ReadSeries()
	if err != nil {
		return preparationSubscription{}, err
	}
	f.Root = ""
	data, err := json.Marshal(struct {
		Fork   packload.Fork
		Series string
		Config []byte
	}{f, series.Digest, cfg})
	if err != nil {
		return preparationSubscription{}, err
	}
	sum := sha256.Sum256(data)
	return preparationSubscription{Key: f.Key(), Runtime: rt, Platform: platform, InputIdentity: hex.EncodeToString(sum[:])}, nil
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

func currentPreparationTree(ctx context.Context, expected preparationSubscription) (packload.Fork, error) {
	trees, err := selectedPreparationTrees(ctx)
	if err != nil {
		return packload.Fork{}, err
	}
	for _, f := range trees {
		if f.Key() != expected.Key {
			continue
		}
		current, err := treePreparationSubscription(f, expected.Runtime, expected.Platform)
		if err != nil {
			return packload.Fork{}, err
		}
		if current.InputIdentity == expected.InputIdentity {
			return f, nil
		}
	}
	return packload.Fork{}, errPreparationGeneration
}

// registerTreePreparation is shared by fresh jail and host launches. Callers pass
// their own platform and owner-filtered vector; this function independently authorizes it.
func registerTreePreparation(trees []packload.Fork, req run.TreeBuildRequest, w io.Writer) error {
	return registerPreparationForks(trees, req.Runtime, req.Platform, w)
}

// registerPreparationForks preserves the exact owner declaration for tree/program adapters.
func registerPreparationForks(trees []packload.Fork, runtime, platform string, w io.Writer) error {
	if len(trees) == 0 || config.InJail() || !run.BackgroundChecksOn(runtime, platform) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	selected, err := selectedPreparationTrees(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, f := range trees {
		want[f.Key()] = true
	}
	var subs []preparationSubscription
	for _, f := range selected {
		if !want[f.Key()] {
			continue
		}
		sub, err := treePreparationSubscription(f, runtime, platform)
		if err != nil {
			return err
		}
		subs = append(subs, sub)
	}
	if len(subs) == 0 {
		return nil
	}
	if _, err = registerPreparationSubscriptions(preparationRoot(), subs); err != nil {
		return err
	}
	if err = startTreePreparation(ctx); err != nil {
		return err
	}
	fmt.Fprintf(w, "Built artifacts: ongoing next-launch preparation registered; log %s; `yolo pack prepare status` shows progress\n", backgroundAdvanceLog())
	return nil
}

// The spawn lock closes the free-lease/start race and bounds repeated failed
// starts. It is distinct from the worker lifetime and subscription transactions.
func startTreePreparation(ctx context.Context) error {
	root := preparationRoot()
	if err := ensurePreparationRoot(root); err != nil {
		return err
	}
	lk, err := pidlock.Acquire(filepath.Join(root, "spawn.lock"), pidlock.Mode{Wait: true, Bound: time.Second}, nil)
	if err != nil {
		return err
	}
	defer lk.Release()
	record, live, err := probePreparationWorker(root)
	if live == preparationUnknown {
		return err
	}
	if live == preparationAlive {
		digest, err := preparationExecutableDigest(run.SelfExecPath())
		if err != nil {
			return err
		}
		if digest == record.ExecutableDigest && record.ExecutablePath == run.SelfExecPath() {
			return controlPreparationWorker(ctx, root, record.Identity, "wake")
		}
		if err := controlPreparationWorker(ctx, root, record.Identity, "replace"); err != nil {
			return err
		}
		if err := waitPreparationGone(ctx, root); err != nil {
			return err
		}
	}
	stamp := filepath.Join(root, "spawn-at.json")
	var at time.Time
	if b, e := os.ReadFile(stamp); e == nil {
		if e = json.Unmarshal(b, &at); e != nil {
			return fmt.Errorf("preparation spawn stamp corrupt: %w — inspect scheduler state before starting", e)
		}
		if age := time.Since(at); age >= 0 && age < 5*time.Second {
			return nil
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if err := writePreparationJSON(stamp, time.Now()); err != nil {
		return err
	}
	return startBackgroundAdvance([]string{"internal", preparationSchedulerVerb}, backgroundAdvanceLog())
}

func waitPreparationGone(ctx context.Context, root string) error {
	// Stop includes existing owned-child 5s grace; no raw PID takeover on timeout.
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, live, _ := probePreparationWorker(root)
		if live == preparationGone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("preparation teardown still owns its lease; retry `yolo pack prepare status` then start")
		case <-ticker.C:
		}
	}
}

func runPreparationScheduler(ctx context.Context, ticks <-chan time.Time, w io.Writer) error {
	root := preparationRoot()
	return runPreparationWorker(ctx, root, run.SelfExecPath(), ticks, func(ctx context.Context) error {
		subscriptions, err := readPreparationSubscriptions(root)
		if err != nil {
			fmt.Fprintln(w, err)
			return err
		}
		subscriptions, err = reconcilePreparationSubscriptions(ctx, root, subscriptions)
		if err != nil {
			for _, sub := range subscriptions {
				publishPreparationInactive(root, sub, err)
			}
			fmt.Fprintln(w, err)
			return err
		}
		for _, sub := range subscriptions {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !run.BackgroundChecksOn(sub.Runtime, sub.Platform) {
				continue
			}
			f, err := currentPreparationTree(ctx, sub)
			if err != nil {
				publishPreparationInactive(root, sub, err)
				continue
			}
			runPreparationTree(ctx, root, sub, f, w)
		}
		return nil
	})
}

// Reconcile only keys a launch already registered. A current input edit advances
// its generation; removal/policy-off retains inspectable inactive evidence.
func reconcilePreparationSubscriptions(ctx context.Context, root string, values map[string]preparationSubscription) (map[string]preparationSubscription, error) {
	trees, err := selectedPreparationTrees(ctx)
	if err != nil {
		return values, err
	}
	var selected []preparationSubscription
	for _, sub := range values {
		for _, f := range trees {
			if f.Key() != sub.Key {
				continue
			}
			current, err := treePreparationSubscription(f, sub.Runtime, sub.Platform)
			if err != nil {
				return values, err
			}
			if current.InputIdentity != sub.InputIdentity {
				selected = append(selected, current)
			}
		}
	}
	if len(selected) == 0 {
		return values, nil
	}
	updated, err := registerPreparationSubscriptions(root, selected)
	if err != nil {
		return values, err
	}
	for _, sub := range updated {
		values[preparationSubscriptionID(sub)] = sub
	}
	return values, nil
}

func publishPreparationInactive(root string, sub preparationSubscription, err error) {
	_ = publishPreparationSnapshot(root, preparationSnapshot{Key: sub.Key, Runtime: sub.Runtime, Platform: sub.Platform, InputIdentity: sub.InputIdentity, Generation: sub.Generation, Phase: "inactive", Reason: err.Error(), At: time.Now()})
}

func runPreparationTree(ctx context.Context, root string, sub preparationSubscription, f packload.Fork, w io.Writer) {
	record, err := readPreparationOwner(root)
	if err != nil {
		publishPreparationInactive(root, sub, err)
		return
	}
	keyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-keyCtx.Done():
				return
			case <-ticker.C:
				if _, err := currentPreparationTree(keyCtx, sub); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	guard := func(commit func() error) error {
		if err := keyCtx.Err(); err != nil {
			return err
		}
		if _, err := currentPreparationTree(keyCtx, sub); err != nil {
			return err
		}
		return withPreparationGeneration(root, sub, func() error {
			owner, err := readPreparationOwner(root)
			if err != nil {
				return err
			}
			digest, err := preparationExecutableDigest(record.ExecutablePath)
			if err != nil || digest != record.ExecutableDigest || owner.Identity != record.Identity {
				return errors.New("preparation owner changed; start with the selected binary and retry")
			}
			if err := keyCtx.Err(); err != nil {
				return err
			}
			return commit()
		})
	}
	var outcome backgroundOutcome
	var servingEntry string
	ran := false
	runTreesUnder(keyCtx, []packload.Fork{f}, sub.Runtime, w, w, func(_ int, f packload.Fork, lane treeLane) {
		withBackgroundKey(f.Key(), f.Label(), lane.errw, func(o *backgroundOutcome) {
			res := advancePatchedFork(f, lane.options(advanceOptions{platform: sub.Platform, runtime: sub.Runtime, launch: true, background: true, checkOnly: !run.BackgroundBuildsOn(sub.Runtime), admitGuard: guard}))
			backgroundResult(o, res, keyCtx.Err() != nil)
			outcome = *o
			servingEntry = res.delivery.Key
			ran = true
		})
	})
	cancel()
	<-watchDone
	// Read-only outcome publication survives notice consumption via existing evidence.
	if !ran {
		return
	} // a busy key cannot relabel historical outcome as current work
	_ = publishPreparationSnapshot(root, preparationSnapshot{Key: sub.Key, Runtime: sub.Runtime, Platform: sub.Platform, InputIdentity: sub.InputIdentity, Generation: sub.Generation, WorkerIdentity: record.Identity, Phase: outcome.State, ServingEntry: servingEntry, Reason: outcome.Error, Log: outcome.Log, At: time.Unix(outcome.At, 0), RetryAt: time.Unix(outcome.RetryAt, 0)})
}

func readPreparationOwner(root string) (preparationWorkerRecord, error) {
	var record preparationWorkerRecord
	b, err := os.ReadFile(filepath.Join(root, "worker.json"))
	if err == nil {
		err = json.Unmarshal(b, &record)
	}
	return record, err
}

func runInternalPreparationScheduler(args []string) int {
	if len(args) != 0 || config.InJail() {
		fmt.Fprintln(os.Stderr, "preparation scheduler runs on the host without arguments — use `yolo pack prepare start` on the host")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), backgroundSignals...)
	defer stop()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	// Wake the real worker once on start; subsequent work is timer/control driven.
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	go func() {
		for {
			select {
			case t := <-ticker.C:
				select {
				case ticks <- t:
				default:
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	if err := runPreparationScheduler(ctx, ticks, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func packPrepare(args []string, out, errw io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errw, "usage: yolo pack prepare start|stop|status")
		return 2
	}
	if config.InJail() {
		fmt.Fprintln(errw, "preparation belongs to the host — run `yolo pack prepare "+args[0]+"` on the host")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := preparationRoot()
	switch args[0] {
	case "status":
		record, live, err := probePreparationWorker(root)
		fmt.Fprintf(out, "Preparation: %s (incarnation %s)\n", live, record.Identity)
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		subscriptions, err := readPreparationSubscriptions(root)
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		for _, sub := range subscriptions {
			snap, err := readPreparationSnapshotFor(root, sub)
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(out, "%s %s/%s: registered\n", sub.Key, sub.Runtime, sub.Platform)
				continue
			}
			if err != nil {
				fmt.Fprintln(errw, err)
				return 1
			}
			fmt.Fprintf(out, "%s %s/%s: %s %s %s\n", sub.Key, sub.Runtime, sub.Platform, snap.Phase, snap.ServingEntry, snap.Reason)
		}
	case "start":
		subs, err := readPreparationSubscriptions(root)
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		subs, err = reconcilePreparationSubscriptions(ctx, root, subs)
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		eligible := false
		for _, sub := range subs {
			if run.BackgroundChecksOn(sub.Runtime, sub.Platform) {
				if _, err := currentPreparationTree(ctx, sub); err == nil {
					eligible = true
					break
				}
			}
		}
		if !eligible {
			fmt.Fprintln(out, "No currently authorized preparation subscriptions — launch with next-launch updates to register selected keys")
			return 0
		}
		if err := startTreePreparation(ctx); err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
	case "stop":
		record, live, err := probePreparationWorker(root)
		if live == preparationGone {
			fmt.Fprintln(out, "Preparation is stopped")
			return 0
		}
		if err == nil {
			err = controlPreparationWorker(ctx, root, record.Identity, "stop")
		}
		if err == nil {
			err = waitPreparationGone(ctx, root)
		}
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		fmt.Fprintln(out, "Preparation stopped; `yolo pack prepare start` resumes authorized subscriptions")
	default:
		fmt.Fprintln(errw, "usage: yolo pack prepare start|stop|status")
		return 2
	}
	return 0
}
