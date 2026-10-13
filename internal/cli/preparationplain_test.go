package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

func selectedPlainProgram(t *testing.T) packload.Fork {
	t.Helper()
	forks := packload.Forks(selectConfiguredHostPacks().packs)
	if len(forks) != 1 || forks[0].Patched() {
		t.Fatalf("not one plain fork: %+v", forks)
	}
	return forks[0]
}

func freshPlainProgram(t *testing.T, f packload.Fork) packload.ForkPin {
	t.Helper()
	started := false
	pin, err := resolveFreshPlainProgram(context.Background(), f, func() error { started = true; return nil }, func(save func() error) error {
		if !started {
			t.Error("pin publication before check-start")
		}
		return save()
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("no real check-start")
	}
	return pin
}

func TestResolveFreshPlainProgramMovesOnlyRequestedBranchAndReusesArtifact(t *testing.T) {
	repo, commit := forkRepo(t)
	first := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	pin := freshPlainProgram(t, f)
	if pin.Commit != first {
		t.Fatal(pin)
	}
	// Missing artifact readiness uses the production pool at the recorded pin.
	count := 0
	entries := []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: 32}}
	fake := fakeCaptureJail(t, new(run.Options), entries)
	withFakeCaptureJail(t, func(o run.Options) int {
		count++
		entries[2].Size = int64(32 + count)
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity\n")
		return fake(o)
	})
	slot := func(pin packload.ForkPin) string {
		req := run.ForkBuildRequest{Pins: []packload.ForkPin{pin}, Runtime: "podman", Platform: "linux/arm64", Interrupt: &run.ActInterrupt{}}
		d, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxChecks: 1, MaxBuilds: 1}, io.Discard, false)
		if d[f.Bin].Key == "" {
			t.Fatalf("slot failed: %+v", d)
		}
		return d[f.Bin].Key
	}
	key := slot(pin)
	if count != 1 {
		t.Fatalf("missing artifact builds=%d", count)
	}
	// Ordinary background/launch readiness cannot move a standing branch pin.
	next := commit("second")
	standing := run.PinLaunchForks([]packload.Fork{f}, nil)
	if len(standing) != 1 || standing[0].Commit != first {
		t.Fatalf("automatic pin moved: %+v", standing)
	}
	if slot(standing[0]) != key || count != 1 {
		t.Fatal("ordinary warm pin rebuilt")
	}
	subs, err := readPreparationSubscriptions(preparationRoot())
	if err != nil || len(subs) != 0 {
		t.Fatalf("plain fork became timer subscriber: %+v %v", subs, err)
	}
	// Receipt selection compares timestamps at second precision. Give the distinct
	// revision a later receipt timestamp instead of testing its tie-break policy.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	pin = freshPlainProgram(t, f)
	if pin.Commit != next || pinnedCommit(t) != next {
		t.Fatal("explicit source check did not move branch", pin)
	}
	key = slot(pin)
	if count != 2 {
		t.Fatal("new revision not built once", count)
	}
	pin = freshPlainProgram(t, f)
	if slot(pin) != key || count != 2 {
		t.Fatal("unchanged fresh source check forced rebuild", count)
	}
}

func TestResolveFreshPlainProgramHonorsExplicitCommitAndPreservesOtherOwner(t *testing.T) {
	repo, commit := forkRepo(t)
	first := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	forkPinHome(t, "git+file://"+repo+"?ref="+first)
	commit("second")
	l := &packsrc.ForkLock{}
	l.Set(packsrc.ForkLockEntry{Key: "other/tool", Commit: "other"})
	if err := l.Save(forkLockPath()); err != nil {
		t.Fatal(err)
	}
	pin := freshPlainProgram(t, selectedPlainProgram(t))
	if pin.Commit != first {
		t.Fatalf("explicit commit moved: %+v", pin)
	}
	l, err := packsrc.LoadForkLock(forkLockPath())
	if err != nil || l.Forks["other/tool"].Commit != "other" {
		t.Fatalf("other pin lost: %+v %v", l, err)
	}
}

func TestResolveFreshPlainProgramRequiresHooksAndRejectsPublication(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	if _, err := resolveFreshPlainProgram(context.Background(), f, nil, func(save func() error) error { return save() }, io.Discard); err == nil {
		t.Fatal("missing boundary accepted")
	}
	if _, err := resolveFreshPlainProgram(context.Background(), f, func() error { return nil }, nil, io.Discard); err == nil {
		t.Fatal("missing guard accepted")
	}
	refused := errors.New("revoked")
	if _, err := resolveFreshPlainProgram(context.Background(), f, func() error { return refused }, func(save func() error) error { t.Error("failed check-start reached publication"); return save() }, io.Discard); !errors.Is(err, refused) {
		t.Fatal(err)
	}
	if _, err := resolveFreshPlainProgram(context.Background(), f, func() error { return nil }, func(func() error) error { return refused }, io.Discard); !errors.Is(err, refused) {
		t.Fatal(err)
	}
	if pinnedCommit(t) != "" {
		t.Fatal("revoked request published pin")
	}
}

func TestResolveFreshPlainProgramFetchFailureCannotCertifyCachedPin(t *testing.T) {
	repo, _ := forkRepo(t)
	forkPinHome(t, "git+file://"+repo+"?ref=main")
	f := selectedPlainProgram(t)
	freshPlainProgram(t, f)
	before, err := os.ReadFile(forkLockPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repo, repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveFreshPlainProgram(context.Background(), f, func() error { return nil }, func(save func() error) error { t.Error("failed fetch reached publication"); return save() }, io.Discard); err == nil {
		t.Fatal("cached fallback certified as fresh")
	}
	after, err := os.ReadFile(forkLockPath())
	if err != nil || string(before) != string(after) {
		t.Fatal("failed freshness modified pin", err)
	}
	// Legacy explicit pinForks keeps its preexisting cached fallback behavior.
	if rc := pinForks(richtext.Printer{W: io.Discard}, io.Discard, true); rc != 0 {
		t.Fatal("legacy pin behavior changed", rc)
	}
}
