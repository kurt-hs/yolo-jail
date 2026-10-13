package cli

// treebackground_test.go pins THE NEXT-LAUNCH MODE (treedelivery.go and the preparation scheduler;
// docs/design/pi-extension-store-builds.md §7.4, XB-D17 to XB-D21): timing precedence, durable tree
// registration, timer-driven advancement, Apple Container's demotion and the next launch's report.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// agentUpdates rewrites the pool fixture's user config with `agent_updates` set to value, JSON.
func (pf *poolFixture) agentUpdates(t *testing.T, value string) {
	t.Helper()
	dir := filepath.Join(pf.packs, "treepool")
	writeFile(t, filepath.Join(pf.home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[`+
		`{"source":"file://`+dir+`","name":"treepool"}],"agent_updates":`+value+`}`)
}

// countBuilds makes every build of the fixture count, and returns the count.
func (pf *poolFixture) countBuilds() *atomic.Int32 {
	var n atomic.Int32
	pf.child = func(context.Context, forkBuild) int { n.Add(1); return 0 }
	return &n
}

// spawns stands in for the detached spawn of the background advance, recording each argv.
type spawns struct {
	mu    sync.Mutex
	argvs [][]string
	logs  []string
}

func stubSpawn(t *testing.T) *spawns {
	t.Helper()
	s := &spawns{}
	prev := startBackgroundAdvance
	startBackgroundAdvance = func(argv []string, log string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.argvs, s.logs = append(s.argvs, argv), append(s.logs, log)
		return nil
	}
	t.Cleanup(func() { startBackgroundAdvance = prev })
	return s
}

// keys are the --key values of spawn i, sorted.
func (s *spawns) keys(i int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, a := range s.argvs[i] {
		if k, ok := strings.CutPrefix(a, "--key="); ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (s *spawns) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.argvs)
}

// THE TIMING READER (XB-D18, the precedence decision): the owning agent pack's own entry, then the
// contributing pack's, then "*"; a hold from either pack is at the launch. Red if treeUpdateTiming
// stops reading `agent_updates`.
func TestTreeUpdateTimingReadsOwnerThenContributingPack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	tree := packload.Fork{Pack: "matt", Owner: "pi", Bin: "ext", Into: ".pi/ext/ext"}
	ownerless := packload.Fork{Pack: "matt", Bin: "ext", Into: ".tool/ext/ext"}
	for _, c := range []struct {
		value string
		f     packload.Fork
		want  updateTiming
	}{
		{`{"pi":"next-launch"}`, tree, timingNextLaunch},
		{`{"matt":"next-launch"}`, tree, timingNextLaunch},
		{`{"pi":"launch","matt":"next-launch"}`, tree, timingAtLaunch},
		{`{"*":"next-launch","matt":true}`, tree, timingAtLaunch},
		{`{"matt":false,"pi":"next-launch"}`, tree, timingAtLaunch},
		{`"next-launch"`, tree, timingNextLaunch},
		{`{"matt":"next-launch"}`, ownerless, timingNextLaunch},
		{`{"pi":"next-launch"}`, ownerless, timingAtLaunch},
	} {
		writeFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), `{"agent_updates":`+c.value+`}`)
		if got := treeUpdateTiming(c.f); got != c.want {
			t.Errorf("agent_updates %s, %s (owner %q): timing %d, want %d", c.value, c.f.Key(), c.f.Owner, got, c.want)
		}
	}
}

// A NEXT-LAUNCH KEY DUE FOR A CHECK is handed its good build and registered for the periodic worker;
// the launch itself checks and builds nothing.
func TestANextLaunchKeyDueForACheckRegistersForPreparation(t *testing.T) {
	pf := newPoolFixture(t)
	first, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if first[poolKeyA].Dir == "" || first[poolKeyB].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "", map[int]string{5: "five"})
	pf.now = pf.now.Add(2 * time.Hour)
	builds := pf.countBuilds()
	stubPreparationWorkerStart(t)
	got, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if builds.Load() != 0 || strings.Contains(out, "checking extension") {
		t.Errorf("a next-launch key was checked or built in front (%d builds):\n%s", builds.Load(), out)
	}
	if got[poolKeyA].Entry != first[poolKeyA].Entry || got[poolKeyB].Entry != first[poolKeyB].Entry {
		t.Errorf("the keys were not handed their good builds: %+v", got)
	}
	state, err := preparationSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2 {
		t.Fatalf("registered subscriptions = %+v, want both selected keys", state)
	}
	for _, r := range state {
		if !r.Active || r.Generation == 0 {
			t.Errorf("subscription is not active and versioned: %+v", r)
		}
	}
}

// A NEXT-LAUNCH KEY WITH NOTHING DUE still starts the idle scheduler and records its subscription,
// but the first timer tick performs no check or build.
func TestANextLaunchKeyWithNothingDueRegistersIdleWorker(t *testing.T) {
	pf := newPoolFixture(t)
	if first, out := pf.launch(t, "podman", &run.ActInterrupt{}); first[poolKeyA].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.now = pf.now.Add(time.Minute)
	builds := pf.countBuilds()
	stubPreparationWorkerStart(t)
	_, _ = pf.launch(t, "podman", &run.ActInterrupt{})
	state, err := preparationSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2 || builds.Load() != 0 {
		t.Fatalf("idle registration state=%+v builds=%d", state, builds.Load())
	}
	for _, r := range state {
		if !r.Active {
			t.Errorf("no-due key was not retained: %+v", r)
		}
	}
}

// A KEY THAT TOOK ITS FALLBACK is registered just like every selected next-launch tree; a key with
// neither build nor fallback remains a cold foreground build. The worker independently reauthorizes.
func TestAFallbackKeyIsRegisteredForTheScheduler(t *testing.T) {
	pf := newPoolFixture(t)
	writeFile(t, filepath.Join(pf.packs, "treepool", "pack.json"), `{"name":"treepool","contributes":[`+
		`{"kind":"files","into":".tool/ext/ext-a","source":"git+file://`+pf.repo+`?ref=main","fallback":"git:example.com/ext-a"},`+
		`{"kind":"files","into":".tool/ext/ext-b","source":"git+file://`+pf.repo+`?ref=main"}]}`)
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	got, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if got[poolKeyA].Dir != "" || !strings.Contains(got[poolKeyA].Reason, "a background advance builds it") {
		t.Errorf("the fallback key was handed %+v", got[poolKeyA])
	}
	if got[poolKeyB].Dir == "" {
		t.Errorf("the key with neither a build nor a fallback was not built in front: %+v\n%s", got[poolKeyB], out)
	}
	state, err := preparationSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2 {
		t.Errorf("scheduler registration omitted selected trees: %+v", state)
	}
}

// A KEY ANOTHER BACKGROUND ADVANCE RUNS NOW is said to be running and not handed a second one. Red if
// the launch ignores the key's background lock.
func TestAKeyWhoseBackgroundAdvanceRunsIsNotStartedAgain(t *testing.T) {
	pf := newPoolFixture(t)
	if first, out := pf.launch(t, "podman", &run.ActInterrupt{}); first[poolKeyA].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "", map[int]string{5: "five"})
	pf.now = pf.now.Add(2 * time.Hour)
	lk, err := pidlock.Acquire(backgroundKeyLock(poolKeyA), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()
	if err := writeBackgroundOutcome(backgroundOutcome{Key: poolKeyA, Label: "extension " + poolKeyA, State: bgRunning,
		Pid: os.Getpid(), At: pf.now.Unix(), Log: backgroundAdvanceLog()}); err != nil {
		t.Fatal(err)
	}
	stubPreparationWorkerStart(t)
	_, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if !strings.Contains(out, "extension "+poolKeyA+": the background advance started at ") ||
		!strings.Contains(out, "is still checking and building it") {
		t.Errorf("the running advance is not said:\n%s", out)
	}
	if _, err := os.Stat(backgroundOutcomePath(poolKeyA)); err != nil {
		t.Errorf("a running advance's record was claimed: %v", err)
	}
	if !pidlock.Held(backgroundKeyLock(poolKeyA)) {
		t.Error("registration disturbed the running key's background lock")
	}
}

// ON APPLE CONTAINER the next-launch mode is the at-launch wait (XB-D21), said: the key is checked and
// built in front and no background advance starts. Red if the runtime gate is dropped.
func TestOnAppleContainerANextLaunchKeyUpdatesAtTheLaunch(t *testing.T) {
	pf := newPoolFixture(t)
	first, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if first[poolKeyA].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "", map[int]string{5: "five"})
	pf.now = pf.now.Add(2 * time.Hour)
	stubPreparationWorkerStart(t)
	got, out := pf.launch(t, "container", &run.ActInterrupt{})
	if got[poolKeyA].Entry == first[poolKeyA].Entry {
		t.Errorf("the key was not updated at the launch:\n%s", out)
	}
	if !strings.Contains(out, "on Apple Container this yolo runs no background advance — it updates at this launch") {
		t.Errorf("the demotion is not said:\n%s", out)
	}
	if snapshot, err := readPreparationSnapshot(poolKeyA); err != nil || snapshot.Phase != "unsupported" {
		t.Errorf("Apple Container preparation outcome = %+v err=%v, want visible unsupported state", snapshot, err)
	}
}

// THE BACKGROUND ADVANCE'S MOVE IS SAID AT THE NEXT LAUNCH, ONCE, and that launch runs the new build:
// a real background advance builds both keys for the next launch, and the launch after it says so;
// the one after that says nothing. Red if the launch stops reading the outcome record, or stops
// claiming it.
func TestTheNextLaunchReportsTheBackgroundMoveOnce(t *testing.T) {
	pf := newPoolFixture(t)
	first, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if first[poolKeyA].Dir == "" || first[poolKeyB].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "", map[int]string{5: "five"})
	pf.now = pf.now.Add(2 * time.Hour)
	var bg bytes.Buffer
	// Advance each key separately: simultaneous keys of this fixture share one repository, and a
	// NoWait background attempt intentionally skips a sibling holding its mirror lock.
	for _, key := range []string{poolKeyA, poolKeyB} {
		runBackgroundAdvanceUnder(context.Background(), backgroundArgs{runtime: "podman", platform: patchedTestPlatform,
			keys: []string{key}}, &bg)
	}
	got, out := pf.launch(t, "podman", &run.ActInterrupt{})
	for _, k := range []string{poolKeyA, poolKeyB} {
		if got[k].Entry == first[k].Entry {
			t.Errorf("%s: the next launch does not run the background advance's build:\n%s\n%s", k, bg.String(), out)
		}
		if !strings.Contains(out, "extension "+k+": the background advance at ") || !strings.Contains(out, "; available for this launch") {
			t.Errorf("%s: the move is not said:\n%s", k, out)
		}
	}
	if !strings.Contains(out, "(was ") {
		t.Errorf("the move line does not say what it moved from:\n%s", out)
	}
	_, again := pf.launch(t, "podman", &run.ActInterrupt{})
	if strings.Contains(again, "the background advance at ") {
		t.Errorf("the move was said twice:\n%s", again)
	}
}

// A BACKGROUND BUILD THAT FAILED is said at the next launch with its log and its retry time. Red if
// the failure's record or its line loses either.
func TestTheNextLaunchReportsAFailureWithItsLogAndRetry(t *testing.T) {
	pf := newPoolFixture(t)
	if first, out := pf.launch(t, "podman", &run.ActInterrupt{}); first[poolKeyB].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "", map[int]string{5: "five"})
	pf.now = pf.now.Add(2 * time.Hour)
	prev := forkBuildChild
	forkBuildChild = func(ctx context.Context, d time.Duration, staging string, b forkBuild, s captureStreams,
		color bool) (int, bool) {
		// THE BUILD LINE RUNS AND FAILS: the jail's boot is done, then its build exits 2.
		s.jailReady()
		writeFile(t, filepath.Join(staging, forkToolchainLeaf), "image-identity\n")
		fmt.Fprintln(s.errw, "npm ERR! the build line of ext-b failed")
		return 2, false
	}
	t.Cleanup(func() { forkBuildChild = prev })
	var bg bytes.Buffer
	runBackgroundAdvanceUnder(context.Background(), backgroundArgs{runtime: "podman", platform: patchedTestPlatform,
		keys: []string{poolKeyB}}, &bg)
	_, out := pf.launch(t, "podman", &run.ActInterrupt{})
	want := "⚠ extension " + poolKeyB + ": the background advance at "
	if !strings.Contains(out, want) || !strings.Contains(out, " could not build ") ||
		!strings.Contains(out, "its output is in "+backgroundAdvanceLog()) || !strings.Contains(out, "it is retried after ") ||
		!strings.Contains(out, "`yolo capture "+poolKeyB+"` now") {
		t.Errorf("the failure is not said with its log and retry:\n%s\nthe background advance said:\n%s", out, bg.String())
	}
}

// AN UNFINISHED BACKGROUND ADVANCE — its record running, its lock free, as a SIGKILL leaves it — is
// said, and this launch starts another though nothing else is due. Red if an unfinished record does
// not start one.
func TestAnUnfinishedBackgroundAdvanceIsSaidAndStartedAgain(t *testing.T) {
	pf := newPoolFixture(t)
	if first, out := pf.launch(t, "podman", &run.ActInterrupt{}); first[poolKeyA].Dir == "" {
		t.Fatalf("the first launch delivered %+v\n%s", first, out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.now = pf.now.Add(time.Minute)
	if err := writeBackgroundOutcome(backgroundOutcome{Key: poolKeyA, Label: "extension " + poolKeyA, State: bgRunning,
		Pid: 1, At: pf.now.Unix(), Log: backgroundAdvanceLog()}); err != nil {
		t.Fatal(err)
	}
	stubPreparationWorkerStart(t)
	_, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if !strings.Contains(out, "did not finish — its output is in "+backgroundAdvanceLog()+"; a launch with next-launch updates retries it") {
		t.Errorf("the unfinished advance is not said:\n%s", out)
	}
	state, err := preparationSubscriptions()
	active := false
	for _, record := range state {
		active = active || (record.Key == poolKeyA && record.Active)
	}
	if err != nil || !active {
		t.Errorf("the unfinished key was not retained as an active subscription: records=%+v err=%v", state, err)
	}
}

// destination (or fail to copy), and a held/edited recipe can select a different build.
func TestABackgroundMoveDoesNotClaimAnUndeliveredTreeRuns(t *testing.T) {
	pf := readyForABackgroundAdvance(t)
	var bg bytes.Buffer
	runBackgroundAdvanceUnder(context.Background(), bgArgs(poolKeyA), &bg)
	f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
	var out syncBuffer
	got := deliverTreesForLaunch(run.TreeBuildRequest{Trees: []packload.Fork{f}, Runtime: "podman",
		Platform: patchedTestPlatform, Build: true, Interrupt: &run.ActInterrupt{}}, &out, &out, false)
	if got[poolKeyA].Dir != "" || !strings.Contains(out.String(), "the background advance at ") {
		t.Fatalf("fixture did not report an undelivered background move: %+v\n%s", got, out.String())
	}
	if strings.Contains(out.String(), "this jail runs it") {
		t.Errorf("a launch that handed no tree claimed it runs the background build:\n%s", out.String())
	}
	_ = pf
}

// Exactly one fresh launch claims a completed outcome; the other launches must not repeat it.
func TestConcurrentLaunchesReportABackgroundOutcomeOnce(t *testing.T) {
	newPoolFixture(t)
	f := packload.PatchedTrees(selectConfiguredHostPacks().packs)[0]
	if err := writeBackgroundOutcome(backgroundOutcome{Key: f.Key(), State: bgMoved, At: time.Now().Unix(), To: "new-build"}); err != nil {
		t.Fatal(err)
	}
	var out syncBuffer
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			noteBackgroundOutcome(f, richtext.Printer{W: &out})
		}()
	}
	wg.Wait()
	if n := strings.Count(out.String(), "the background advance at "); n != 1 {
		t.Errorf("%d launches reported the same outcome, want one:\n%s", n, out.String())
	}
}

// A PATCHED-FORK FIXTURE whose first advance built and recorded the good build, its `agent_updates`
// rewritten to the next-launch mode for the fork's own pack, and a newer upstream with the clock
// stepped past the check's interval: a check is due for the fork key.
func forkWithAGoodBuild(t *testing.T) (*patchedAdvanceFixture, string) {
	t.Helper()
	fx := newPatchedAdvanceFixture(t, "")
	r, out, _ := fx.launch(t, "podman")
	if r.delivery.Key == "" {
		t.Fatalf("the fixture's first advance built nothing:\n%s", out)
	}
	fx.writeUserConfig(t, `,"agent_updates":{"forkpack":"next-launch"}`)
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen"})
	fx.later(2 * time.Hour)
	return fx, r.delivery.Key
}

// forkRequest is one launch's fork-build request for fx's patched fork.
func forkRequest(t *testing.T, fx *patchedAdvanceFixture, runtime string) run.ForkBuildRequest {
	t.Helper()
	return run.ForkBuildRequest{Pins: []packload.ForkPin{{Fork: fx.fork(t)}}, Platform: patchedTestPlatform,
		Runtime: runtime, Workspace: "/ws", Interrupt: &run.ActInterrupt{}}
}

func TestExplicitFreshRequestReachesTheProductionForkBuildSlot(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	initial, _, _ := fx.launch(t, "podman")
	if initial.delivery.Key == "" {
		t.Fatal("fixture did not establish its serving generation")
	}
	before := fx.record(t)
	req := forkRequest(t, fx, "podman")
	req.FreshUpdate = true
	var out syncBuffer
	got, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
	after := fx.record(t)
	if got["tool"].Key != initial.delivery.Key || len(fx.builds) != 1 {
		t.Fatalf("explicit unchanged freshness did not reuse the admitted artifact: delivery=%+v builds=%d\n%s",
			got["tool"], len(fx.builds), out.String())
	}
	if after.StartedSeq <= before.StartedSeq || after.Check == nil || after.Check.StartedSeq != after.StartedSeq {
		t.Fatalf("production BuildSlot did not perform a post-registration keyed check: before=%+v after=%+v", before, after)
	}
}

// A PATCHED FORK THAT UPDATES FOR THE NEXT LAUNCH is handed its good build with no check and no
// build, and the launch starts ONE background advance carrying its key. This drives the production
// pool path (runBuildSlot -> addForkKeys -> backgroundTreeAdvance -> startBackgroundAdvance): delete
// the fork arm's collection or its call site and no advance is started. Red if forkUpdateTiming stops
// reading `agent_updates`, if the fork arm advances the key in front, or if its key never reaches the
// spawn.
func TestAPatchedForkThatUpdatesForTheNextLaunchStartsOneBackgroundAdvance(t *testing.T) {
	fx, good := forkWithAGoodBuild(t)
	builds := len(fx.builds)
	sp := stubSpawn(t)
	req := forkRequest(t, fx, "podman")
	var out syncBuffer
	got, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
	if got["tool"].Key != good || got["tool"].Reason != "" {
		t.Errorf("the fork was not handed its good build: %+v", got["tool"])
	}
	if len(fx.builds) != builds {
		t.Errorf("a next-launch fork was checked or built in front (%d builds):\n%s", len(fx.builds)-builds, out.String())
	}
	if sp.count() != 1 || !slices.Equal(sp.keys(0), []string{"forkpack/tool"}) {
		t.Fatalf("%d background advances started for %v, want one for forkpack/tool:\n%s", sp.count(), sp.argvs, out.String())
	}
	if !strings.Contains(out.String(), "fork forkpack/tool: updates for the next launch — a background advance "+
		"is checking and building it now; its log is "+backgroundAdvanceLog()) {
		t.Errorf("the spawn is not said with its log:\n%s", out.String())
	}
}

// ON APPLE CONTAINER the next-launch mode is the at-launch wait (XB-D21, XB-D61), said: the fork key
// is checked and built in front and no background advance starts. Red if the runtime gate is
// dropped.
func TestOnAppleContainerANextLaunchForkUpdatesAtTheLaunch(t *testing.T) {
	fx, good := forkWithAGoodBuild(t)
	sp := stubSpawn(t)
	req := forkRequest(t, fx, "container")
	var out syncBuffer
	got, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &out, false)
	if sp.count() != 0 {
		t.Errorf("Apple Container started a background advance:\n%s", out.String())
	}
	if got["tool"].Key == good {
		t.Errorf("the fork was not updated at the launch:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "on Apple Container this yolo runs no background advance — it updates at this launch") {
		t.Errorf("the demotion is not said:\n%s", out.String())
	}
}
