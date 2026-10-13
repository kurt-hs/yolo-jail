package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

func preparationForkRequest(f packload.Fork, commit, runtime string) run.ForkBuildRequest {
	return run.ForkBuildRequest{Pins: []packload.ForkPin{{Fork: f, Commit: commit, Ref: "main"}},
		Platform: patchedTestPlatform, Runtime: runtime, Workspace: "/ws", Interrupt: &run.ActInterrupt{}}
}

func setForkNextLaunch(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(paths.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.TrimSuffix(strings.TrimSpace(string(data)), "}") + `,"agent_updates":{"forkpack":"next-launch"}}`
	if err := os.WriteFile(paths.UserConfigPath(), []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitPreparation(t *testing.T, key string, ready func(preparationSnapshot) bool) preparationSnapshot {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		snapshot, err := readPreparationSnapshot(key)
		if err == nil && ready(snapshot) {
			return snapshot
		}
		select {
		case <-deadline:
			t.Fatalf("preparation %s did not reach expected state; last snapshot %+v", key, snapshot)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestPlainForkPreparationTimerFillsMissingArtifactAtStandingPin(t *testing.T) {
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)

	var builds atomic.Int32
	previousChild := forkBuildChild
	forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, streams captureStreams,
		color bool) (int, bool) {
		builds.Add(1)
		return forkBuildRunJail(staging, b, streams, color), false
	}
	t.Cleanup(func() { forkBuildChild = previousChild })
	var seen run.Options
	withFakeCaptureJail(t, fakeBuildJail(t, &seen, probetoolBuilt))

	beforePin, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	pinned, ok := beforePin.Get(f.Key())
	if !ok || pinned.Commit != forkTestCommit {
		t.Fatalf("fixture pin = %+v, want the recorded standing pin %s", pinned, forkTestCommit)
	}
	var launchLog strings.Builder
	req := preparationForkRequest(f, pinned.Commit, "podman")
	req.Platform = "linux/arm64"
	deliveries, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxBuilds: 1}, &launchLog, false)
	if deliveries[f.Bin].Key == "" || builds.Load() != 1 {
		t.Fatalf("cold launch did not build its required pinned program once: deliveries=%+v builds=%d\n%s",
			deliveries, builds.Load(), launchLog.String())
	}
	if entry, _, err := resolveForkBuild(&capture.Store{Dir: paths.CapturesDir()}, f.Bin, req.Platform,
		f.Source, pinned.Commit, forkBuild{Fork: f, Commit: pinned.Commit, Platform: req.Platform}.recipe()); err != nil || entry.Key != deliveries[f.Bin].Key {
		t.Fatalf("launch artifact did not resolve through the regular pinned-program selector: entry=%+v err=%v delivery=%+v",
			entry, err, deliveries[f.Bin])
	}
	if snapshot, err := readPreparationSnapshot(f.Key()); err != nil || snapshot.Generation == 0 || snapshot.InputIdentity == "" {
		t.Fatalf("runBuildSlot did not register the selected plain fork: %+v err=%v", snapshot, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, io.Discard) }()
	ticks <- time.Now()
	waitPreparation(t, f.Key(), func(snapshot preparationSnapshot) bool {
		return snapshot.Phase == "unchanged" && snapshot.ServingEntry == deliveries[f.Bin].Key
	})
	if builds.Load() != 1 {
		t.Fatalf("an unchanged admitted artifact was rebuilt: %d builds", builds.Load())
	}

	store := &capture.Store{Dir: paths.CapturesDir()}
	if err := os.RemoveAll(store.EntryDir(deliveries[f.Bin].Key)); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now().Add(time.Second)
	waitPreparation(t, f.Key(), func(snapshot preparationSnapshot) bool {
		return snapshot.Phase == "admitted" && snapshot.ServingEntry != ""
	})
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Fatalf("timer did not fill exactly one missing pinned artifact: builds=%d", builds.Load())
	}
	afterPin, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	after, ok := afterPin.Get(f.Key())
	if !ok || after.Commit != pinned.Commit || after.Source != pinned.Source {
		t.Fatalf("timer moved the standing pin: before=%+v after=%+v", pinned, after)
	}
}

func plainForkPreparationFixture(t *testing.T) (packload.Fork, preparationRecord, string, string, packsrc.Addr) {
	t.Helper()
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)

	repo := filepath.Join(t.TempDir(), "upstream")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	writeFile(t, filepath.Join(repo, "Makefile"), "install:\n\techo built\n")
	git("config", "user.email", "test@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("add", "Makefile")
	git("commit", "-m", "fixture")
	commit := git("rev-parse", "HEAD")
	source := "git+file://" + filepath.ToSlash(repo) + "?ref=main"
	manifest := filepath.Join(f.Root, "pack.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), forkTestSource, source, 1)
	if updated == string(data) {
		t.Fatal("fixture source address was not present in the fork declaration")
	}
	writeFile(t, manifest, updated)
	f = packload.Forks(selectConfiguredHostPacks().packs)[0]
	lock, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	lock.Set(packsrc.ForkLockEntry{Key: f.Key(), Source: source, Ref: "main", Commit: commit})
	if err := lock.Save(forkLockPath()); err != nil {
		t.Fatal(err)
	}
	if err := registerForkPreparation([]packload.ForkPin{{Fork: f, Commit: commit, Ref: "main"}},
		preparationForkRequest(f, commit, "podman"), io.Discard, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readPreparationSnapshot(f.Key())
	if err != nil {
		t.Fatal(err)
	}
	record := preparationRecord{Key: f.Key(), Runtime: "podman", Platform: patchedTestPlatform,
		Kind: preparationPlainProgramKind, Generation: snapshot.Generation, PinCommit: commit}
	addr, err := packsrc.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	return f, record, commit, source, addr
}

func TestPlainForkPreparationHeldMirrorDoesNotBlockStopOrAdmission(t *testing.T) {
	f, record, commit, _, addr := plainForkPreparationFixture(t)
	sum := sha256.Sum256([]byte(addr.Repo))
	lockPath := filepath.Join(paths.PacksDir(), "locks", filepath.Base(addr.Repo)+"-"+hex.EncodeToString(sum[:6])+".lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lockFile.Close()
		t.Fatal(err)
	}
	releaseMirror := func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}
	released := false
	defer func() {
		if !released {
			releaseMirror()
		}
	}()

	started := make(chan struct{}, 1)
	previousCheckout := forkCheckout
	forkCheckout = func(store *packsrc.Store, a packsrc.Addr, gotCommit string) (string, error) {
		if store.Ctx == nil || !store.NoWait {
			t.Errorf("timer checkout store policy = Ctx:%v NoWait:%v, want task context and no-wait",
				store.Ctx != nil, store.NoWait)
		}
		started <- struct{}{}
		resolved, err := store.Materialize(a, gotCommit)
		if err != nil {
			return "", err
		}
		return resolved.Root, nil
	}
	t.Cleanup(func() { forkCheckout = previousCheckout })
	previousFetch := forkFetchCommit
	forkFetchCommit = func(store *packsrc.Store, a packsrc.Addr, gotCommit string) error {
		if store.Ctx == nil || !store.NoWait {
			t.Errorf("timer fetch store policy = Ctx:%v NoWait:%v, want task context and no-wait",
				store.Ctx != nil, store.NoWait)
		}
		return store.FetchForkCommit(a, gotCommit, nil)
	}
	t.Cleanup(func() { forkFetchCommit = previousFetch })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runPlainForkPreparation(ctx, record, f, io.Discard) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		releaseMirror()
		released = true
		<-done
		t.Fatal("timer never reached its source checkout")
	}
	cancel() // scheduler stop/revocation while the missing-source mirror is held
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("held mirror returned %v", err)
		}
	case <-time.After(750 * time.Millisecond):
		releaseMirror()
		released = true
		<-done
		t.Fatal("stopping the timer did not drain its held-mirror source checkout promptly")
	}
	releaseMirror()
	released = true
	if child, _, resolveErr := resolveForkBuild(&capture.Store{Dir: paths.CapturesDir()}, f.Bin,
		patchedTestPlatform, f.Source, commit, (forkBuild{Fork: f, Commit: commit, Platform: patchedTestPlatform}).recipe()); resolveErr == nil {
		t.Errorf("busy timer source was admitted as %s (%v)", child.Key, resolveErr)
	}
	outcome := outcomeOf(t, f.Key())
	if outcome.State != bgSkipped && outcome.State != bgInterrupted {
		t.Errorf("held-mirror shutdown outcome = %+v, want busy skip or interruption", outcome)
	}
}

func TestPlainForkPreparationMissingCommitFetchContentionSkips(t *testing.T) {
	f, record, commit, source, addr := plainForkPreparationFixture(t)

	sum := sha256.Sum256([]byte(addr.Repo))
	mirrorLock := filepath.Join(paths.PacksDir(), "locks", filepath.Base(addr.Repo)+"-"+hex.EncodeToString(sum[:6])+".lock")
	if err := os.MkdirAll(filepath.Dir(mirrorLock), 0o755); err != nil {
		t.Fatal(err)
	}

	previousCheckout := forkCheckout
	lockHeld := make(chan struct{})
	var lockFile *os.File
	forkCheckout = func(store *packsrc.Store, a packsrc.Addr, gotCommit string) (string, error) {
		resolved, checkoutErr := store.Materialize(a, gotCommit)
		if checkoutErr != nil && lockFile == nil {
			var err error
			lockFile, err = os.OpenFile(mirrorLock, os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				t.Errorf("open mirror lock: %v", err)
				return "", checkoutErr
			}
			if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Errorf("hold mirror lock after missing-commit checkout: %v", err)
				_ = lockFile.Close()
				lockFile = nil
				return "", checkoutErr
			}
			close(lockHeld)
		}
		if checkoutErr != nil {
			return "", checkoutErr
		}
		return resolved.Root, nil
	}
	t.Cleanup(func() {
		forkCheckout = previousCheckout
		if lockFile != nil {
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			_ = lockFile.Close()
		}
	})
	previousFetch := forkFetchCommit
	fetchResult := make(chan error, 1)
	forkFetchCommit = func(store *packsrc.Store, a packsrc.Addr, gotCommit string) error {
		if store.Ctx == nil || !store.NoWait {
			t.Errorf("timer fetch store policy = Ctx:%v NoWait:%v, want task context and no-wait",
				store.Ctx != nil, store.NoWait)
		}
		err := previousFetch(store, a, gotCommit)
		fetchResult <- err
		return err
	}
	t.Cleanup(func() { forkFetchCommit = previousFetch })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runPlainForkPreparation(ctx, record, f, io.Discard) }()
	select {
	case <-lockHeld:
	case <-time.After(3 * time.Second):
		t.Fatal("timer did not perform a missing-commit checkout before the fetch contention")
	}
	select {
	case fetchErr := <-fetchResult:
		if !errors.Is(fetchErr, packsrc.ErrLockHeld) {
			t.Errorf("pinned-commit fetch error = %v, want errors.Is(ErrLockHeld)", fetchErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timer did not attempt the pinned-commit fetch after checkout failed")
	}
	select {
	case taskErr := <-done:
		if taskErr != nil {
			t.Errorf("busy timer source returned an error: %v", taskErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("busy timer source did not defer promptly")
	}
	outcome := outcomeOf(t, f.Key())
	if outcome.State != bgSkipped || outcome.RetryAt != 0 || !strings.Contains(outcome.Error, "source mirror is busy") {
		t.Errorf("busy pinned-commit fetch outcome = %+v, want a no-backoff skip/defer", outcome)
	}
	if entry, _, resolveErr := resolveForkBuild(&capture.Store{Dir: paths.CapturesDir()}, f.Bin,
		patchedTestPlatform, f.Source, commit, (forkBuild{Fork: f, Commit: commit, Platform: patchedTestPlatform}).recipe()); resolveErr == nil {
		t.Errorf("busy pinned-commit fetch was admitted as %s", entry.Key)
	}
	current, err := currentForkPin(f)
	if err != nil || current.Commit != commit || current.Fork.Source != source {
		t.Errorf("timer changed standing pin after busy fetch: pin=%+v err=%v", current, err)
	}
}

func TestPreparationForkIdentityIncludesPinAndRecipe(t *testing.T) {
	f := packload.Fork{Pack: "forkpack", Base: "basepack", Bin: "tool", Source: forkTestSource,
		Build: "make install", Produces: []string{".local/bin/tool"}}
	first, err := preparationForkIdentity(f, forkTestCommit, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := preparationForkIdentity(f, strings.Repeat("a", 40), "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	f.Build = "make install-v2"
	changedRecipe, err := preparationForkIdentity(f, forkTestCommit, "podman", patchedTestPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if first == moved || first == changedRecipe {
		t.Fatalf("fork identity did not cover pin/recipe: first=%s moved=%s recipe=%s", first, moved, changedRecipe)
	}
}

func TestForkPreparationRegistrationRendersRichTextAndEscapesValues(t *testing.T) {
	forkBuildHome(t)
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	f.Bin = "[red]spoof[/red]"
	pin := packload.ForkPin{Fork: f, Commit: forkTestCommit, Ref: "main"}
	req := preparationForkRequest(f, pin.Commit, "podman")

	var plain, colored bytes.Buffer
	if err := registerForkPreparation([]packload.ForkPin{pin}, req, &plain, false); err != nil {
		t.Fatal(err)
	}
	if err := registerForkPreparation([]packload.ForkPin{pin}, req, &colored, true); err != nil {
		t.Fatal(err)
	}
	for mode, out := range map[string]string{"plain": plain.String(), "colored": colored.String()} {
		if strings.Contains(out, "[dim]") || strings.Contains(out, "[/dim]") {
			t.Errorf("%s output leaked rich-text markup: %q", mode, out)
		}
		if strings.Contains(out, "\x1b[31mspoof") {
			t.Errorf("%s output interpreted a manifest-controlled label as color: %q", mode, out)
		}
		if !strings.Contains(richtext.Strip(out), "next-launch preparation is recorded") {
			t.Errorf("%s output lost the registration notice: %q", mode, out)
		}
	}
	if !strings.Contains(colored.String(), "\x1b[2m") {
		t.Errorf("color-enabled registration did not render its dim style: %q", colored.String())
	}
}

func TestPreparationAdmissionGuardRejectsRevokedOrEditedFork(t *testing.T) {
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)
	pin := packload.ForkPin{Fork: f, Commit: forkTestCommit, Ref: "main"}
	req := preparationForkRequest(f, pin.Commit, "podman")
	if err := registerForkPreparation([]packload.ForkPin{pin}, req, io.Discard, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readPreparationSnapshot(f.Key())
	if err != nil || snapshot.Generation == 0 {
		t.Fatalf("fork was not registered: %+v err=%v", snapshot, err)
	}

	guard := preparationAdmissionGuard(f.Key(), "podman", patchedTestPlatform, snapshot.Generation)
	committed := false
	if err := guard(func() error { committed = true; return nil }); err != nil || !committed {
		t.Fatalf("unchanged selected inputs failed admission: committed=%v err=%v", committed, err)
	}

	manifest := filepath.Join(f.Root, "pack.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"build":"make install"`, `"build":"make changed"`, 1)
	if edited == string(data) {
		t.Fatalf("fixture manifest did not contain the expected build recipe: %s", data)
	}
	if err := os.WriteFile(manifest, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	committed = false
	if err := guard(func() error { committed = true; return nil }); !errors.Is(err, errPreparationAdmissionStale) || committed {
		t.Fatalf("stale fork recipe admitted: committed=%v err=%v", committed, err)
	}
}

func TestPreparationAdmissionGuardRejectsManualForkPinMovement(t *testing.T) {
	forkBuildHome(t)
	f := packload.Forks(selectConfiguredHostPacks().packs)[0]
	setForkNextLaunch(t)
	stubPreparationWorkerStart(t)
	pin := packload.ForkPin{Fork: f, Commit: forkTestCommit, Ref: "main"}
	req := preparationForkRequest(f, pin.Commit, "podman")
	if err := registerForkPreparation([]packload.ForkPin{pin}, req, io.Discard, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readPreparationSnapshot(f.Key())
	if err != nil {
		t.Fatal(err)
	}

	lock, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	manualCommit := strings.Repeat("b", 40)
	lock.Set(packsrc.ForkLockEntry{Key: f.Key(), Source: f.Source, Ref: "main", Commit: manualCommit})
	if err := lock.Save(forkLockPath()); err != nil {
		t.Fatal(err)
	}
	committed := false
	guard := preparationAdmissionGuard(f.Key(), "podman", patchedTestPlatform, snapshot.Generation)
	if err := guard(func() error { committed = true; return nil }); !errors.Is(err, errPreparationAdmissionStale) || committed {
		t.Fatalf("an old pin's candidate survived manual pin movement: committed=%v err=%v", committed, err)
	}
	current, err := currentForkPin(f)
	if err != nil || current.Commit != manualCommit {
		t.Fatalf("timer changed or ignored the manually recorded pin: pin=%+v err=%v", current, err)
	}
}
