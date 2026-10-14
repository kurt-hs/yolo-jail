package run

// consumehandoff_test.go pins the one-time host→jail carry-in
// (docs/reference/host-to-jail-handoff.md): a fresh .yolo/handover.md is rendered into the
// jail's briefing and the pointer is consumed, so the handoff surfaces on exactly one
// launch and never returns as a stale task.
//
// The load-bearing test here is the WIRE one — TestRefreshJailBriefingsCarriesHandoff —
// and it exists because the first version of this file was the shape AGENTS.md warns
// about: it exercised the helper directly and stayed green with the call site deleted, so
// the whole feature could be switched off without a red test. Measured: replacing
// `handoff := readHandoff(o.Workspace)` in refreshJailBriefings with `handoff := ""` left
// the package green. Everything below goes through refreshJailBriefings for that reason.

import (
	"bytes"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/ioprio"
	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// handoffResult is what one refreshJailBriefings call did to a workspace carrying a
// handoff: the briefing it composed (empty when it wrote none), whether the pointer
// survived, and what it told the user.
type handoffResult struct {
	briefing        string
	pointerSurvives bool
	consumedExists  bool
	stderr          string
	events          []perf.Event
}

// runRefreshWithHandoff files `handoff` at .yolo/handover.md, stages either a
// briefing-declaring pack or no packs at all, and runs the real refreshJailBriefings over
// it. Going through the run pipeline's own function — not readHandoff, not
// BriefingContent — is the whole point: this is the call site.
func runRefreshWithHandoff(t *testing.T, handoff string, withBriefingPack bool, blockConsume ...bool) handoffResult {
	t.Helper()
	home := packHome(t)
	ws := t.TempDir()
	yoloDir := filepath.Join(ws, ".yolo")
	if err := os.MkdirAll(yoloDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if handoff != "" {
		if err := os.WriteFile(filepath.Join(yoloDir, handoffPointer), []byte(handoff), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if len(blockConsume) > 0 && blockConsume[0] {
		if err := os.MkdirAll(filepath.Join(yoloDir, handoffConsumed), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	packsJSON := "[]"
	if withBriefingPack {
		packDir := filepath.Join(t.TempDir(), "bp")
		if err := os.MkdirAll(packDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writePack(t, packDir, `{"name":"bp","contributes":[`+
			`{"kind":"briefing","into":".claude/CLAUDE.md"}]}`)
		packsJSON = `[{"source":"file://` + packDir + `","name":"bp"}]`
	}
	writeUserPacks(t, home, packsJSON)

	jailcontent.SetPackSkillDirs(nil)
	jailcontent.SetPackSkillTargets(nil)
	t.Cleanup(func() { jailcontent.SetPackSkillDirs(nil); jailcontent.SetPackSkillTargets(nil) })

	var errBuf bytes.Buffer
	o := goldenOptions(ws, home)
	o.Stdout = discardBuf()
	o.Stderr = &errBuf
	var events []perf.Event
	o.Perf = perf.New(nil, func(e perf.Event) { events = append(events, e) })

	const cname = "yolo-test-handoff"
	root, packs, briefings, err := o.stagePacks(cname)
	if err != nil {
		t.Fatalf("stagePacks: %v", err)
	}
	staging, err := o.refreshJailBriefings(cname, jsonx.NewOrderedMap(), "podman",
		stagedPacks{root: root, packs: packs, briefings: briefings}, ioprio.Normal)
	if err != nil {
		t.Fatalf("refreshJailBriefings: %v", err)
	}

	res := handoffResult{stderr: errBuf.String(), events: events}
	if data, err := os.ReadFile(filepath.Join(staging, briefingStagingName(claudeBriefingDest))); err == nil {
		res.briefing = string(data)
	}
	if _, err := os.Stat(filepath.Join(yoloDir, handoffPointer)); err == nil {
		res.pointerSurvives = true
	}
	if _, err := os.Stat(filepath.Join(yoloDir, handoffConsumed)); err == nil {
		res.consumedExists = true
	}
	return res
}

// The wire: a filed handoff reaches the briefing the jail will read, and the pointer is
// consumed. Deleting either half of the wiring in refreshJailBriefings — the readHandoff
// call or the BriefingInput.Handoff field — fails this.
func TestRefreshJailBriefingsCarriesHandoff(t *testing.T) {
	const task = "Task: wire the OAuth broker. Context: docs/handoff/oauth.md"
	got := runRefreshWithHandoff(t, task, true)

	if got.briefing == "" {
		t.Fatal("no briefing written for a pack that declares one")
	}
	for _, want := range []string{"## Handoff", "it is **the task**", task} {
		if !strings.Contains(got.briefing, want) {
			t.Errorf("briefing missing %q:\n%s", want, got.briefing)
		}
	}
	if got.pointerSurvives {
		t.Error("pointer should be consumed once its handoff reached a briefing")
	}
	if !got.consumedExists {
		t.Error("consumed marker missing — the rename is what makes the carry-in one-time")
	}
	// The burn is announced. Core cannot tell an agent launch from `yolo -- bash`, so the
	// only protection against a handoff consumed by a shell is that the human sees it go.
	if !strings.Contains(got.stderr, handoffConsumed) {
		t.Errorf("consuming a handoff must say so and name the restore path, got: %q", got.stderr)
	}
}

// A second launch finds nothing: the whole point of consuming. This is the stale-handoff
// bug the design was written for — a four-week-old handover read as the current task.
func TestRefreshJailBriefingsHandoffSurfacesOnce(t *testing.T) {
	got := runRefreshWithHandoff(t, "", true)
	if got.briefing == "" {
		t.Fatal("no briefing written for a pack that declares one")
	}
	if strings.Contains(got.briefing, "## Handoff") {
		t.Errorf("no pointer must yield no Handoff section:\n%s", got.briefing)
	}
	if got.consumedExists {
		t.Error("nothing to consume, yet a consumed marker appeared")
	}
	if got.stderr != "" {
		t.Errorf("no handoff should be silent, got: %q", got.stderr)
	}
}

// A jail with no briefing destination (no packs — run.go's warnIfNoPacks) renders the
// handoff NOWHERE, so it must not consume the pointer. Consuming here loses the task
// outright: the section was never written, and the next launch finds a consumed marker.
// The pre-fix code consumed unconditionally, ahead of the write loop.
func TestRefreshJailBriefingsKeepsHandoffWhenNothingCarriesIt(t *testing.T) {
	const task = "Task: wire the OAuth broker."
	got := runRefreshWithHandoff(t, task, false)

	if got.briefing != "" {
		t.Fatalf("no pack declares a briefing, yet one was written:\n%s", got.briefing)
	}
	if !got.pointerSurvives {
		t.Error("handoff consumed by a launch that wrote no briefing — the task is now lost")
	}
	if got.consumedExists {
		t.Error("consumed marker written for a handoff that reached no briefing")
	}
}

// readHandoff/consumeHandoff at the unit level: read does not consume, consume is
// one-shot, and both are quiet about an absent pointer.
func TestReadAndConsumeHandoff(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".yolo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const content = "Task: wire the broker. Context: docs/handoff/broker.md"
	if err := os.WriteFile(filepath.Join(dir, handoffPointer), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reading twice is non-destructive — the split from consume is what lets the caller
	// wait until a briefing has actually carried it.
	for i := range 2 {
		if got := readHandoff(ws); got != content {
			t.Fatalf("read %d = %q, want %q", i, got, content)
		}
	}

	if !consumeHandoff(ws) {
		t.Fatal("consume reported no pointer, but one was filed")
	}
	if got := readHandoff(ws); got != "" {
		t.Errorf("read after consume = %q, want empty", got)
	}
	if consumeHandoff(ws) {
		t.Error("second consume reported success — nothing was there to consume")
	}

	// Absent from the start: empty, no marker.
	empty := t.TempDir()
	if got := readHandoff(empty); got != "" {
		t.Errorf("absent handoff = %q, want empty", got)
	}
	if consumeHandoff(empty) {
		t.Error("consume of an absent handoff reported success")
	}
}

func TestRefreshJailBriefingsReportsActualHandoffPhases(t *testing.T) {
	for _, tc := range []struct {
		name, handoff string
		briefing      bool
		consume       bool
	}{
		{"delivered", "task", true, true},
		{"no destination", "task", false, false},
		{"absent", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runRefreshWithHandoff(t, tc.handoff, tc.briefing)
			assertPhaseEvents(t, got.events, "launch.handoff_read", true)
			assertPhaseEvents(t, got.events, "launch.handoff_consume", tc.consume)
			if tc.consume {
				found := false
				for _, e := range got.events {
					if e.Kind == perf.KindNote && e.Name == "launch.handoff_consume" && e.Detail == "pointer consumed" {
						found = true
					}
				}
				if !found {
					t.Fatal("consumed pointer has no factual outcome")
				}
			}
		})
	}
}

func assertPhaseEvents(t *testing.T, events []perf.Event, name string, want bool) {
	t.Helper()
	starts, ends := 0, 0
	for _, e := range events {
		if e.Name != name {
			continue
		}
		if e.Kind == perf.KindStart {
			starts++
		}
		if e.Kind == perf.KindEnd {
			ends++
		}
	}
	n := 0
	if want {
		n = 1
	}
	if starts != n || ends != n {
		t.Fatalf("%s: starts=%d ends=%d, want %d each", name, starts, ends, n)
	}
}

func TestHandoffConsumeReportsRetainedPointerOnRenameFailure(t *testing.T) {
	got := runRefreshWithHandoff(t, "task retained after rename failure", true, true)
	if !strings.Contains(got.briefing, "task retained after rename failure") {
		t.Fatal("refresh did not deliver the handoff before attempting consumption")
	}
	if !got.pointerSurvives {
		t.Fatal("failed rename lost pointer")
	}
	if strings.Contains(got.stderr, "surfaced in this jail's briefing and consumed") {
		t.Fatal("failed rename announced success")
	}
	events := got.events
	assertPhaseEvents(t, events, "launch.handoff_consume", true)
	for _, e := range events {
		if e.Kind == perf.KindNote && e.Name == "launch.handoff_consume" && e.Detail == "pointer retained (rename did not succeed)" {
			return
		}
	}
	t.Fatal("failed consume must not claim that the pointer was consumed")
}
