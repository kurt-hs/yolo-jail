package cli

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

func TestPreparationFreshUnchangedDoesNotForceRebuild(t *testing.T) {
	fx, _, _, good, _, _ := firstAdvance(t)
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	f := fx.fork(t)
	req, err := newPreparationRequest(context.Background(), []packload.Fork{f}, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	before := fx.record(t).Seq
	builds := fx.child
	r := advancePatchedFork(f, advanceOptions{freshCheck: true, freshIdentity: req.Keys[0].InputIdentity, freshBoundary: req.Boundaries[f.Key()], platform: patchedTestPlatform, runtime: "podman", launch: true, ctx: context.Background(), out: io.Discard, errw: io.Discard, admitGuard: func(commit func() error) error { return req.Guard(f.Key(), commit) }})
	if err := qualifyPreparationResult(req, f.Key(), r); err != nil {
		t.Fatal(err)
	}
	if fx.record(t).Seq != before+1 || fx.child != builds || r.delivery.Key != good.delivery.Key {
		t.Fatalf("fresh check rebuilt unchanged serving artifact: checks %d/%d builds %d/%d delivery %s/%s", fx.record(t).Seq, before, fx.child, builds, r.delivery.Key, good.delivery.Key)
	}
	beforeForce := len(fx.builds)
	forced := advancePatchedFork(f, advanceOptions{force: true, platform: patchedTestPlatform, runtime: "podman", out: io.Discard, errw: io.Discard})
	if !forced.built || len(fx.builds) != beforeForce+1 {
		t.Fatal("capture force stopped rebuilding")
	}

}
func TestPreparationFreshEditedInputsNeverQualify(t *testing.T) {
	fx, _, _, _, _, _ := firstAdvance(t)
	f := fx.fork(t)
	req, err := newPreparationRequest(context.Background(), []packload.Fork{f}, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	if err = req.Guard(f.Key(), func() error { t.Fatal("stale request committed"); return nil }); err == nil || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("edit was not actionable: %v", err)
	}
}
func TestPreparationFreshEmptyDoesNotSpawn(t *testing.T) {
	old := startBackgroundAdvance
	defer func() { startBackgroundAdvance = old }()
	startBackgroundAdvance = func([]string, string) error { t.Fatal("empty request spawned"); return nil }
	req, err := newPreparationRequest(context.Background(), nil, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	release, err := registerPreparationDemand(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestPreparationUpdateParsersKeepCommandArgv(t *testing.T) {
	for _, flag := range []string{"-u", "--update"} {
		o := run.Options{}
		got := parseRunArgs([]string{"run", flag, "--", "bash", "--update"}, &o)
		if !o.Update || got.misuse != nil || len(o.Args) != 2 || o.Args[1] != "--update" {
			t.Fatalf("run did not parse %s: %+v %+v", flag, got, o.Args)
		}
		h, ok := parseHostExecFlags([]string{flag}, io.Discard)
		if !ok || !h.update {
			t.Fatalf("host did not parse %s", flag)
		}
	}
}
func TestPreparationCancelledWaiterStopsWithoutFreshness(t *testing.T) {
	fx, _, _, _, _, _ := firstAdvance(t)
	f := fx.fork(t)
	req, err := newPreparationRequest(context.Background(), []packload.Fork{f}, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	r, err := waitFreshPreparation(ctx, req, f.Key())
	if err != context.Canceled || r.delivery.Key != "" || time.Since(start) > time.Second {
		t.Fatalf("cancelled waiter claimed freshness: %+v %v", r, err)
	}
}

func TestPreparationSharedDemandSurvivesFirstWaiterCancellation(t *testing.T) {
	fx, _, _, good, _, _ := firstAdvance(t)
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":false}`)
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 25: "new release"})
	f := fx.fork(t)
	stubSpawn(t)
	first, err := newPreparationRequest(context.Background(), []packload.Fork{f}, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	releaseFirst, err := registerPreparationDemand(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	started, resume := make(chan struct{}), make(chan struct{})
	old := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
		close(started)
		select {
		case <-ctx.Done():
			return 130, false
		case <-resume:
		}
		return old(ctx, bound, staging, b, s, color)
	}
	defer func() { forkBuildChild = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- time.Now()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatal("scheduler production dispatcher never began explicit build")
	}
	second, err := newPreparationRequest(context.Background(), []packload.Fork{f}, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond, err := registerPreparationDemand(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()
	releaseFirst()
	close(resume)
	waitCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	result, err := waitFreshPreparation(waitCtx, second, f.Key())
	cancel()
	<-done
	if err != nil {
		t.Fatalf("second demand lost when first waiter cancelled: %v", err)
	}
	if result.delivery.Key == good.delivery.Key || fx.child != 1 {
		t.Fatalf("shared build lost/rebuilt: entry %s child builds %d", result.delivery.Key, fx.child)
	}
}

func TestPreparationUpdateHostPublicCallsiteAndHelp(t *testing.T) {
	dist, _ := floorHostFixture(t, `,"agent_updates":false`)
	got := captureHostExec(t)
	var output syncBuffer
	if rc := hostExec(nil, []string{"floorcli"}, io.Discard, &output, nil); rc != 0 || !got.execed {
		t.Fatalf("initial readiness: rc %d exec=%v output %s", rc, got.execed, output.String())
	}
	dist.Publish("floorcli-pkg", "1.0.1", "bin=floorcli", "fail")
	before := len(dist.NpmCalls("install"))
	got = captureHostExec(t)
	output = syncBuffer{}
	if rc := hostExec([]string{"-u"}, []string{"floorcli", "--update"}, io.Discard, &output, nil); rc != 1 || len(dist.NpmCalls("install")) != before+1 || got.execed || !strings.Contains(output.String(), "fresh preparation") {
		t.Fatalf("host flag lost before production preparation: rc %d installs=%v exec=%v output %s", rc, dist.NpmCalls("install"), got.execed, output.String())
	}
	if rc := hostExec([]string{"--update", "--help"}, nil, io.Discard, &output, nil); rc != 0 || len(dist.NpmCalls("install")) != before+1 || got.execed {
		t.Fatal("help prepared host inputs or executed the target")
	}
}

func TestPreparationFreshFailedCheckRejectsCompatibleFallback(t *testing.T) {
	fx, _, _, good, _, _ := firstAdvance(t)
	f := fx.fork(t)
	req, err := newPreparationRequest(context.Background(), []packload.Fork{f}, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	old := patchedAdvanceStore
	defer func() { patchedAdvanceStore = old }()
	patchedAdvanceStore = func(launch bool) *packsrc.Store {
		s := old(launch)
		local := *s
		local.Git = "/bin/false"
		return &local
	}
	r := advancePatchedFork(f, advanceOptions{freshCheck: true, freshIdentity: req.Keys[0].InputIdentity, freshBoundary: req.Boundaries[f.Key()], platform: patchedTestPlatform, runtime: "podman", launch: true, ctx: context.Background(), out: io.Discard, errw: io.Discard})
	if !r.failed || r.delivery.Key != "" {
		t.Fatalf("fresh failed source delivered old Good: %+v", r)
	}
	ordinary := advancePatchedFork(f, advanceOptions{platform: patchedTestPlatform, runtime: "podman", launch: true, ctx: context.Background(), out: io.Discard, errw: io.Discard})
	if ordinary.delivery.Key != good.delivery.Key {
		t.Fatal("ordinary compatible fallback vanished")
	}
	if err = qualifyPreparationResult(req, f.Key(), r); err == nil || !strings.Contains(err.Error(), "without --update") {
		t.Fatalf("failed freshness advertised fallback as fresh: %v", err)
	}
}
