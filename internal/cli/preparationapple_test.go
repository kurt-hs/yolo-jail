package cli

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func allowAppleContainerBuild(t testing.TB) {
	t.Helper()
	prev := appleContainerBuildCapability
	appleContainerBuildCapability = func() (bool, string) { return true, "" }
	t.Cleanup(func() { appleContainerBuildCapability = prev })
}

func TestAppleContainerCheckOnlyDoesNotStartBuildChildAndKeepsAdmittedTree(t *testing.T) {
	pf := newPoolFixture(t)
	first, _ := pf.launch(t, "podman", &run.ActInterrupt{})
	if first[poolKeyA].Entry == "" {
		t.Fatalf("fixture did not establish admitted trees: %+v", first)
	}
	var children atomic.Int32
	pf.child = func(context.Context, forkBuild) int {
		children.Add(1)
		return 0
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	pf.now = pf.now.Add(2 * time.Hour)
	prev := appleContainerBuildCapability
	appleContainerBuildCapability = func() (bool, string) {
		return false, "Apple Container has running yolo jail(s): yolo-another-session"
	}
	t.Cleanup(func() { appleContainerBuildCapability = prev })

	built := children.Load()
	got, out := pf.launch(t, "container", &run.ActInterrupt{})
	if children.Load() != built {
		t.Fatalf("Apple Container started %d build children while another jail was live:\n%s", children.Load()-built, out)
	}
	if got[poolKeyA].Entry != first[poolKeyA].Entry || got[poolKeyA].Dir == "" {
		t.Errorf("the prior admitted tree was not used: before=%+v after=%+v", first[poolKeyA], got[poolKeyA])
	}
	for _, want := range []string{"checked candidate", "left it pending", "yolo-another-session", "container ls"} {
		if !strings.Contains(out, want) {
			t.Errorf("Apple Container check-only progress lacks %q:\n%s", want, out)
		}
	}
}

func TestAppleContainerRequiredColdBuildRefusesWhenLivenessIsUnknown(t *testing.T) {
	pf := newPoolFixture(t)
	var children atomic.Int32
	pf.child = func(context.Context, forkBuild) int {
		children.Add(1)
		return 0
	}
	prev := appleContainerBuildCapability
	appleContainerBuildCapability = func() (bool, string) {
		return false, "`container ls` could not establish Apple Container liveness (daemon unavailable); no build child was started"
	}
	t.Cleanup(func() { appleContainerBuildCapability = prev })

	got, out := pf.launch(t, "container", &run.ActInterrupt{})
	if children.Load() != 0 {
		t.Fatalf("Apple Container started %d build children with unknown liveness:\n%s", children.Load(), out)
	}
	if got[poolKeyA].Dir != "" || !strings.Contains(got[poolKeyA].Reason, "left it pending") {
		t.Errorf("a required cold build did not refuse with its pending reason: %+v\n%s", got[poolKeyA], out)
	}
	for _, want := range []string{"no build child was started", "repair the Apple Container listing", "retry a fresh launch"} {
		if !strings.Contains(out+got[poolKeyA].Reason, want) {
			t.Errorf("the cold-build refusal lacks %q:\n%s\nreason: %s", want, out, got[poolKeyA].Reason)
		}
	}
}

func TestAppleContainerFreshCheckRefusesPendingCandidateDespiteServingGood(t *testing.T) {
	pf := newPoolFixture(t)
	initial, out := pf.launch(t, "podman", &run.ActInterrupt{})
	if initial[poolKeyA].Entry == "" {
		t.Fatalf("fixture did not establish an admitted tree: %+v\n%s", initial[poolKeyA], out)
	}
	before := patchedRecordOf(t, poolKeyA)
	if before.Good == nil {
		t.Fatal("fixture has no previous admitted good build")
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	pf.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	pf.now = pf.now.Add(2 * time.Hour)
	var children atomic.Int32
	pf.child = func(context.Context, forkBuild) int {
		children.Add(1)
		return 0
	}
	prevCapability := appleContainerBuildCapability
	appleContainerBuildCapability = func() (bool, string) {
		return false, "Apple Container has running yolo jail(s): yolo-another-session"
	}
	t.Cleanup(func() { appleContainerBuildCapability = prevCapability })

	trees := packload.PatchedTrees(selectConfiguredHostPacks().packs)
	req := run.TreeBuildRequest{FreshUpdate: true, Trees: trees, Platform: patchedTestPlatform, Runtime: "container",
		Workspace: "/ws", Build: true, CopyRoot: filepath.Join(t.TempDir(), "tree.patched"), Interrupt: &run.ActInterrupt{}}
	var report syncBuffer
	got := deliverTreesForLaunch(req, &report, &report, false)
	if children.Load() != 0 {
		t.Fatalf("fresh check started an Apple Container build child despite live-jail liveness:\n%s", report.String())
	}
	if got[poolKeyA].Dir != "" || !strings.Contains(got[poolKeyA].Reason, "could not be prepared freshly") {
		t.Fatalf("freshness request silently used the old admitted build: delivery=%+v\n%s", got[poolKeyA], report.String())
	}
	if !strings.Contains(got[poolKeyA].Reason, "candidate remains pending") || !strings.Contains(got[poolKeyA].Reason, "yolo-another-session") {
		t.Fatalf("strict refusal did not retain the Apple pending/liveness explanation: %+v\n%s", got[poolKeyA], report.String())
	}
	after := patchedRecordOf(t, poolKeyA)
	if after.Good == nil || after.Good.Commit != before.Good.Commit || after.Good.Recipe != before.Good.Recipe {
		t.Fatalf("check-only freshness damaged the old admitted good build: before=%+v after=%+v", before.Good, after.Good)
	}
	if after.Check == nil || len(after.Check.List) == 0 || after.Check.List[0].Tag != "v1.1.0" {
		t.Fatalf("freshness did not record the checked candidate as pending: %+v", after.Check)
	}
}

func TestAppleContainerCapabilityTreatsUnknownListingAsUnsafe(t *testing.T) {
	for _, listing := range []string{"", "yolo-one image running"} {
		if safe, reason := appleContainerBuildDecision(listing); safe || !strings.Contains(reason, "liveness is unknown") {
			t.Errorf("unrecognized listing %q was treated as safe: safe=%v reason=%q", listing, safe, reason)
		}
	}
	if safe, reason := appleContainerBuildDecision("ID IMAGE OS ARCH STATE\nyolo-live image linux arm64 running\n"); safe || !strings.Contains(reason, "yolo-live") {
		t.Errorf("a live yolo jail was not a build blocker: safe=%v reason=%q", safe, reason)
	}
	if safe, reason := appleContainerBuildDecision("ID IMAGE OS ARCH STATE\n"); !safe || reason != "" {
		t.Errorf("a valid empty container listing was not safe: safe=%v reason=%q", safe, reason)
	}
}
