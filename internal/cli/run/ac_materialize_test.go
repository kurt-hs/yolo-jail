package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// TestAppleContainerLeavesBriefingsForThePostRefreshDelivery pins the assembler half of the
// producer-order contract. Apple Container's home copy is made by runContainer only after
// refreshJailBriefings has written current applied facts; assembling argv cannot copy a prior
// launch's bytes or silently leave a fresh launch empty.

func TestAppleContainerLeavesBriefingsForThePostRefreshDelivery(t *testing.T) {
	ws := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	emptyLoopholeDirs(t)
	o := goldenOptions(ws, home)
	o.IsMacOS = true
	o.IsLinux = false

	wsState := filepath.Join(ws, ".yolo", "home")
	if err := os.MkdirAll(wsState, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stale staging file must not be copied before the refresh producer runs.
	agentsPath := filepath.Join(ws, "agents")
	if err := os.MkdirAll(agentsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// PACK-DECLARED staging name: the mount reads briefingStagingName(<dest>), so the fixture
	// must write the same file or this asserts a path the assembler never looks at.
	loadedPacks := claudePackFixture(t)
	stagedBriefing := filepath.Join(agentsPath, briefingStagingName(claudeBriefingDest))
	if err := os.MkdirAll(filepath.Dir(stagedBriefing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedBriefing, []byte("# briefing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sec := jsonx.NewOrderedMap()
	sec.Set("blocked_tools", []any{})
	cfg := newConfig("security", sec)

	in := &assembleInput{
		cfg:          cfg,
		rt:           "container",
		cname:        "yolo-ws-abcd1234",
		packs:        loadedPacks,
		agentsPath:   agentsPath,
		wsState:      wsState,
		miseStore:    "/mise-store",
		yoloVersion:  "9.9.9-test",
		mountTargets: map[string]struct{}{},
	}

	got := o.assembleRunCmd(in)
	joined := strings.Join(got, " ")

	// 1. briefing copied under ws_state at the PACK's declared mount destination — read
	// off the declaration, so the test breaks if the pack moves it rather than asserting a
	// path the assembler never looks at.
	briefDest := ""
	for _, c := range loadedPacks[0].Decl.Contributions() {
		if c.Kind == packdecl.KindBriefing && c.Agent != "" { // the destination, not the pack's own prose
			briefDest = c.Into
		}
	}
	if briefDest == "" {
		t.Fatal("the claude pack declares no briefing mount")
	}
	if b, err := os.ReadFile(filepath.Join(wsState, briefDest)); err == nil {
		t.Errorf("assembly copied a briefing before refresh: %q", b)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat pre-refresh briefing destination: %v", err)
	}
	// 2. NO single-file -v mount for either (that's the AC bug being avoided), and no
	// bind of the per-agent directory, which deliverChannel writes in-home on this backend.
	if strings.Contains(joined, "yolo-user-env.sh:/home/agent") {
		t.Errorf("AC path must NOT single-file-mount yolo-user-env.sh: %v", got)
	}
	if strings.Contains(joined, ":/home/agent/.config/yolo-agent-env") {
		t.Errorf("AC path must NOT bind the per-agent env directory: %v", got)
	}
	if strings.Contains(joined, ":/home/agent/"+briefDest+":ro") {
		t.Errorf("AC path must NOT single-file-mount the briefing: %v", got)
	}
}
