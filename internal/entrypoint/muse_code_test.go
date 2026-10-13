package entrypoint

// muse_code_test.go pins the two declarations that make Meta's Muse Code usable in a jail:
// the workspace-trust store it reads before the model runs, and the §4.2 autonomy flags that
// clear its tool-approval prompts.
//
// BOTH ASSERTIONS ARE ON THE RENDERED RESULT OR THE DISPATCHED ARGV, not on a manifest field,
// so deleting either declaration from packs/muse-code/pack.json turns them red:
//
//   - the trust test renders the shipped pack through the boot's own loop
//     (ConfigurePackByName → renderPackSet) into a real home and reads the file Muse reads;
//   - the autonomy test drives packload.InjectLaunchFlags, the same function the launcher
//     uses to put flags on the agent's command line, at the jail AND host notches.
//
// Without the trust surface Muse's interactive TUI parks on "Do you trust this workspace?"
// (measured: the dialog opens and `muse exec` prints `workspace is untrusted`); without the
// autonomy flags it stalls on the first tool-approval prompt. Both are the observed "the
// Muse agent does not start" symptom.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// TestMuseCodeTrustStoreRendersTheWorkspaceTrusted renders the shipped muse-code pack at the
// jail notch and requires `~/.config/muse/trust.json` to hold a `trusted` entry for the
// render-time workspace — the exact shape Muse Code 1.4.7 was measured to accept
// (`muse: workspace trust: trusted source=user-config`).
func TestMuseCodeTrustStoreRendersTheWorkspaceTrusted(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	e := &Env{Home: home, Workspace: ws, Vars: map[string]string{}}
	withCtxRoot(t, t.TempDir(), "muse")

	if err := ConfigurePackByName(e, "muse-code"); err != nil {
		t.Fatalf("rendering muse-code: %v", err)
	}

	path := filepath.Join(home, ".config", "muse", "trust.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Muse's trust store was NOT rendered at %s — the trust dialog will park every "+
			"launch: %v", path, err)
	}
	if strings.Contains(string(data), agentcfg.WorkspacePlaceholder) {
		t.Errorf("the ${workspace} placeholder reached the trust store instead of the jail root:\n%s", data)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("trust store is not JSON: %v\n%s", err, data)
	}
	if got["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1 — Muse refuses to start without it: %s",
			got["schema_version"], data)
	}
	// MUSE REJECTS A FLOAT VERSION, so the byte-level form is load-bearing and a JSON-unmarshal
	// comparison would not catch it (`1` and `1.0` both decode to float64(1) here).
	// `{"schema_version": 1.0}` is `invalid type: floating point `1.0`, expected u32` — exit 1,
	// worse than the dialog this surface exists to clear. This is why the surface is `stateful`
	// and not claude's `rmw`.
	if !strings.Contains(string(data), `"schema_version": 1
`) || strings.Contains(string(data), `"schema_version": 1.0`) {
		t.Errorf("schema_version must be rendered as the INTEGER 1 Muse accepts, not `1.0`:\n%s", data)
	}
	projects, _ := got["projects"].(map[string]any)
	entry, _ := projects[ws].(map[string]any)
	if entry["decision"] != "trusted" {
		t.Errorf("projects[%q].decision = %v, want %q — Muse refuses the exact shape otherwise\nfile: %s",
			ws, entry["decision"], "trusted", data)
	}
}

// TestMuseCodeAutonomyDispatchesNoPromptFlagsAtJailAndPromptsAtHost drives the launcher's own
// argv builder: the jail notch must inject the approval/sandbox bypass flags, the host notch
// must not (it injects the guarded posture's prompts-on flag instead).
func TestMuseCodeAutonomyDispatchesNoPromptFlagsAtJailAndPromptsAtHost(t *testing.T) {
	p, err := embeddedPack("muse-code")
	if err != nil {
		t.Fatal(err)
	}
	packs := []*packload.Pack{p}

	jailAutonomy := render.ProfileFor(render.KindJail).AgentAutonomy
	jail, inj := packload.InjectLaunchFlags(packs, jailAutonomy, []string{"muse"})
	jailLine := strings.Join(jail, " ")
	for _, flag := range []string{"--disable-approval", "--disable-sandbox"} {
		if !strings.Contains(jailLine, flag) {
			t.Errorf("the jail launch of `muse` is missing %q, so the agent stalls on approval "+
				"prompts:\n  dispatched: %s", flag, jailLine)
		}
	}
	if inj == nil {
		t.Errorf("the jail launch rewrote argv with no injection record, so nothing is disclosed")
	}
	// Trust is NOT a launch flag: it belongs to the persisted store. `--yolo` would also trust
	// for the run, which is redundant at best and not what this pack declares.
	for _, flag := range []string{"--yolo", "--trust-workspace"} {
		if strings.Contains(jailLine, flag) {
			t.Errorf("the jail launch passes %q; trust is answered by the rendered store, not a "+
				"per-run flag:\n  dispatched: %s", flag, jailLine)
		}
	}

	hostAutonomy := render.ProfileFor(render.KindHost).AgentAutonomy
	host, _ := packload.InjectLaunchFlags(packs, hostAutonomy, []string{"muse"})
	hostLine := strings.Join(host, " ")
	if strings.Contains(hostLine, "--disable-approval") || strings.Contains(hostLine, "--disable-sandbox") {
		t.Errorf("the HOST launch carries the jail's bypass flags, which nothing contains on a "+
			"real machine:\n  dispatched: %s", hostLine)
	}
	if !strings.Contains(hostLine, "--approval-mode on-request") {
		t.Errorf("the HOST launch does not assert prompts-on (%q), so the guarded posture "+
			"renders nothing:\n  dispatched: %s", "--approval-mode on-request", hostLine)
	}
}
