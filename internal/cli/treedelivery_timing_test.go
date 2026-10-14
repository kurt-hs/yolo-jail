package cli

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

func phaseCount(events []perf.Event, name string, kind perf.Kind) int {
	n := 0
	for _, e := range events {
		if e.Name == name && e.Kind == kind {
			n++
		}
	}
	return n
}

func TestTreeDeliveryTimingAtActualSlotCalls(t *testing.T) {
	fx := newTreesFixture(t, 1)
	fx.slot(t, "podman", 1, nil) // establish an admitted tree, without recording
	stubSpawn(t)
	for _, mode := range []string{"copied", "copy failed", "no copy root", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			var events []perf.Event
			log := perf.New(nil, func(e perf.Event) { events = append(events, e) })
			if mode == "disabled" {
				log = perf.Disabled()
			}
			root := filepath.Join(t.TempDir(), "copies")
			if mode == "no copy root" {
				root = ""
			}
			if mode == "copy failed" {
				writeFile(t, root, "not a directory")
			}
			req := run.TreeBuildRequest{Trees: fx.trees, Platform: patchedTestPlatform, Runtime: "podman", Build: false, CopyRoot: root}
			_, got := runBuildSlot(run.BuildSlotRequest{Trees: &req, Perf: log}, io.Discard, false)
			d := got[fx.trees[0].Key()]
			wantCopy := mode != "no copy root"
			if mode == "copied" && d.Dir == "" {
				t.Fatalf("actual copy missing: %+v", d)
			}
			if mode == "copy failed" && !strings.Contains(d.Reason, "could not be copied") {
				t.Fatalf("copy did not fail: %+v", d)
			}
			for name, want := range map[string]bool{"preparation.admitted_read": true, "preparation.copy_install": wantCopy, "preparation.registration": false} {
				n := 0
				if want && mode != "disabled" {
					n = 1
				}
				if phaseCount(events, name, perf.KindStart) != n || phaseCount(events, name, perf.KindEnd) != n {
					t.Fatalf("%s expected %d actual span pairs: %+v", name, n, events)
				}
			}
			if mode == "copy failed" {
				found := false
				for _, e := range events {
					if e.Kind == perf.KindNote && e.Name == "preparation.copy_install" && strings.Contains(e.Detail, "failed:") {
						found = true
					}
				}
				if !found {
					t.Fatal("failed copy must have a factual failure note")
				}
			}
		})
	}
}

func TestEmptySlotTimingDoesNotClaimPreparation(t *testing.T) {
	var events []perf.Event
	runBuildSlot(run.BuildSlotRequest{Perf: perf.New(nil, func(e perf.Event) { events = append(events, e) })}, io.Discard, false)
	if len(events) != 0 {
		t.Fatalf("empty slot claimed work: %+v", events)
	}
}

func TestRegistrationTimingSkipsUnsupportedAndInJailCalls(t *testing.T) {
	fx := newTreesFixture(t, 1)
	for _, mode := range []string{"unsupported", "in jail"} {
		t.Run(mode, func(t *testing.T) {
			// Apple Container registers check-only preparation since the foreground
			// required-build gate, so the unsupported runtime is macos-user, which runs
			// no preparation at all.
			rt := "macos-user"
			if mode == "in jail" {
				rt = "podman"
				t.Setenv("YOLO_VERSION", "test")
			}
			var events []perf.Event
			req := run.TreeBuildRequest{Trees: fx.trees, Platform: patchedTestPlatform, Runtime: rt, Build: false}
			runBuildSlot(run.BuildSlotRequest{Trees: &req, Perf: perf.New(nil, func(e perf.Event) { events = append(events, e) })}, io.Discard, false)
			if phaseCount(events, "preparation.registration", perf.KindStart) != 0 || phaseCount(events, "preparation.registration", perf.KindEnd) != 0 {
				t.Fatalf("skipped registration claimed timing: %+v", events)
			}
			if phaseCount(events, "preparation.copy_install", perf.KindStart) != 0 {
				t.Fatal("missing delivery claimed a copy")
			}
		})
	}
}

func TestRegistrationTimingSurroundsActualSubscriptionWrite(t *testing.T) {
	fx := newTreesFixture(t, 1)
	stubSpawn(t)
	previous := treeUpdateTiming
	treeUpdateTiming = func(packload.Fork) updateTiming { return timingNextLaunch }
	t.Cleanup(func() { treeUpdateTiming = previous })
	var events []perf.Event
	req := run.TreeBuildRequest{Trees: fx.trees, Platform: patchedTestPlatform, Runtime: "podman", Build: true}
	var out bytes.Buffer
	runBuildSlot(run.BuildSlotRequest{Trees: &req, Perf: perf.New(nil, func(e perf.Event) { events = append(events, e) })}, &out, false)
	subs, err := readPreparationSubscriptions(preparationRoot())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sub := range subs {
		if sub.Key == fx.trees[0].Key() {
			found = true
		}
	}
	if !found {
		t.Fatalf("timed registration never registered its actual selection: %s events=%+v", out.String(), events)
	}
	if phaseCount(events, "preparation.registration", perf.KindStart) != 1 || phaseCount(events, "preparation.registration", perf.KindEnd) != 1 {
		t.Fatalf("actual subscription write not timed: %+v", events)
	}
}

func TestRegistrationTimingReportsStartFailure(t *testing.T) {
	fx := newTreesFixture(t, 1)
	previousTiming, previousSpawn := treeUpdateTiming, preparationSpawn
	treeUpdateTiming = func(packload.Fork) updateTiming { return timingNextLaunch }
	preparationSpawn = func() error { return errors.New("synthetic start failure") }
	t.Cleanup(func() { treeUpdateTiming, preparationSpawn = previousTiming, previousSpawn })
	var events []perf.Event
	var out bytes.Buffer
	req := run.TreeBuildRequest{Trees: fx.trees, Platform: patchedTestPlatform, Runtime: "podman", Build: true}
	runBuildSlot(run.BuildSlotRequest{Trees: &req, Perf: perf.New(nil, func(e perf.Event) { events = append(events, e) })}, &out, false)
	if !strings.Contains(out.String(), "could not be recorded") {
		t.Fatalf("missing repair notice: %s", out.String())
	}
	if phaseCount(events, "preparation.registration", perf.KindStart) != 1 || phaseCount(events, "preparation.registration", perf.KindEnd) != 1 {
		t.Fatalf("failed attempt not timed: %+v", events)
	}
	found := false
	for _, e := range events {
		if e.Kind == perf.KindNote && e.Name == "preparation.registration" && strings.Contains(e.Detail, "failed: synthetic start failure") {
			found = true
		}
	}
	if !found {
		t.Fatal("failed registration claimed no failure outcome")
	}
}
