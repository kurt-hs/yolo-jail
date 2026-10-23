package cli

// packtreefailure_test.go pins a PACK-TREE PROGRAM's failed build (docs/design/pack-buildable-programs.md
// §8, BG-D13, BG-D14): the launch slot refuses that one program with the build's log and the
// retry command, in the pack's words and never a fork's; a launch that waited on another launch's
// build of the same key takes its failure rather than repeating it; and a launch that starts after
// the failure builds again.

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// failingPackTreeJail stands in for a build jail whose build line ran and exited 3, as the line's
// own status records it. runs counts it.
func failingPackTreeJail(t *testing.T, runs *int) func(run.Options) int {
	t.Helper()
	return func(o run.Options) int {
		*runs++
		writeFile(t, filepath.Join(o.Workspace, forkToolchainLeaf), "image-identity-abc\n")
		writeFile(t, filepath.Join(o.Workspace, capture.InstallerStatusLeaf), "3\n")
		return 3
	}
}

// THE CALL SITE: a launch's slot (runBuildSlot → addForkKeys → buildPlainForkForLaunch) whose
// pack-tree build fails answers no key and a reason naming the pack's build, its log under the
// workspace's .yolo and `yolo pack build <pack>` — never a fork, a commit, or that the launch
// continues without it (the readiness act stops it: OQ-BG1's leaning). Red if addForkKeys stops
// routing a pack-tree pin to the act, or the failure is worded as a fork's.
func TestAFailedPackTreeBuildIsRefusedWithItsLogAndRetry(t *testing.T) {
	f, digest := packTreeBuildFixture(t, "exit 3")
	runs := 0
	withFakeCaptureJail(t, failingPackTreeJail(t, &runs))
	prev := forkBuildChild
	forkBuildChild = func(_ context.Context, _ time.Duration, staging string, b forkBuild, s captureStreams,
		color bool) (int, bool) {
		return forkBuildRunJail(staging, b, s, color), false
	}
	t.Cleanup(func() { forkBuildChild = prev })
	ws := floortest.ResolvedTemp(t)
	slot := func() (string, string) {
		var stream syncBuffer
		req := run.ForkBuildRequest{Pins: []packload.ForkPin{{Fork: f, Commit: digest}}, Platform: "linux/amd64",
			Runtime: "podman", Workspace: ws, Interrupt: &run.ActInterrupt{}}
		forks, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxChecks: 1, MaxBuilds: 1}, &stream, false)
		if forks["x"].Key != "" {
			t.Fatalf("a failed build answered a key: %+v\n%s", forks["x"], stream.String())
		}
		return forks["x"].Reason, stream.String()
	}
	reason, out := slot()
	if runs != 1 {
		t.Fatalf("%d build jail runs, want 1", runs)
	}
	for _, want := range []string{"pack fx's build of x from tools/x (tree digest " + shortSHA(digest) + ")",
		"failed on the host", ".yolo/build-", "`yolo pack build fx`"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not name %q:\n%s", want, reason)
		}
	}
	for _, said := range []string{reason, out} {
		if strings.Contains(said, "fork") || strings.Contains(said, "commit") || strings.Contains(said, "continues without") {
			t.Errorf("a pack-tree program's failure reads as a fork's, or says the launch continues:\n%s", said)
		}
	}
	if !strings.Contains(out, "Retry: `yolo pack build fx`") {
		t.Errorf("the launch's warning does not name the retry:\n%s", out)
	}
	// A launch that STARTS after the failure builds again, with no back-off (BG-D13).
	if _, _ = slot(); runs != 2 {
		t.Errorf("the next launch ran %d build jails in all, want a second build", runs)
	}
}

// BG-D13: A WAITER TAKES THE WINNER'S RESULT, FAILURE INCLUDED. A launch blocked on the build's lock
// while another launch's build of the same key fails returns that failure, naming the winner's log,
// and runs no build jail of its own; a launch that took the lock without waiting builds.
func TestAPackTreeWaiterTakesTheWinnersFailure(t *testing.T) {
	f, digest := packTreeBuildFixture(t, "exit 3")
	runs := 0
	withFakeCaptureJail(t, failingPackTreeJail(t, &runs))
	b := forkBuild{Fork: f, Commit: digest, Platform: "linux/amd64"}
	mode := buildMode{lock: pidlock.Mode{Wait: true, Bound: time.Minute}, runJail: func(staging string, b forkBuild,
		s captureStreams) int {
		return forkBuildRunJail(staging, b, s, false)
	}}

	// The winner: this test holds the lock, as another launch's build would.
	winner, err := pidlock.Acquire(b.lockPath(), pidlock.NoWait, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out syncBuffer
	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		_, err := buildFork(b, mode, &out, io.Discard, false)
		done <- result{err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "waiting for pid") {
		if time.Now().After(deadline) {
			t.Fatalf("the second build never waited on the lock:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The winner's build fails, recording its result under the lock, then releases it.
	recordPackTreeOutcome(b, buildMode{}, errors.New("the build line ran and failed with exit status 3"))
	winner.Release()
	r := <-done
	var adopted packTreeWaitedFailure
	if !errors.As(r.err, &adopted) || !strings.Contains(r.err.Error(), "exit status 3") {
		t.Fatalf("the waiter returned %v, want the winner's failure", r.err)
	}
	if runs != 0 {
		t.Errorf("the waiter ran %d build jails, want none: it takes the winner's result", runs)
	}

	// A launch that STARTS after the failure takes the lock at once, and builds again.
	if _, err := buildFork(b, mode, io.Discard, io.Discard, false); err == nil || errors.As(err, &adopted) || runs != 1 {
		t.Errorf("a launch after the failure: %v, %d jail runs, want its own build", err, runs)
	}
}
