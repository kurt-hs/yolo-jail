package packload_test

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// copilot's `--yolo` IS AN AUTONOMY CONTRIBUTION, NOT A PLAIN LAUNCH FLAG — the jail notch
// injects it, the host notch does not, and the declaration is what makes that true.
//
// `--yolo` is copilot's permission bypass: `--allow-all-tools --allow-all-paths
// --allow-all-urls`, in its own help text. Declared as a plain `launch` contribution it was
// outside the §4.2 notch policy (jail/guest autonomous, host guarded) by construction — the
// one thing that policy exists to prevent, after `yolo host apply` leaked shipped packs'
// jail-bypass keys onto a real machine. It was harmless only because `cli.hostExec` injects
// no flags at all today, which is a fact about that function and not about the declaration.
//
// THE KIND THAT ALLOWED THE MISTAKE IS GONE, which is what makes the notch pair below a
// sufficient test rather than half of one. `kind: "launch"` was retired precisely because it
// was an ungated channel — a flag declared there was one no notch could withhold — so there is
// no longer a second place `--yolo` could be declared from. packdecl refuses the spelling with
// the migration named (TestTheRetiredLaunchKindRefusesWithItsReplacement); this test asserts
// the composed result, at both notches, off render's own policy table.
func TestCopilotYoloIsDeclaredUnderAutonomyNotAsAPlainLaunchFlag(t *testing.T) {
	packs := loadAll(t)

	// The notch pair, read off render's ONE notch→preset table rather than a literal
	// true/false, so flipping HostProfile's policy bit fails here too.
	jail := packload.LaunchFlagsFor(packs, render.ProfileFor(render.KindJail).AgentAutonomy)["copilot"]
	if strings.Join(jail, " ") != "--yolo" {
		t.Errorf("at the JAIL notch copilot's flags = %v, want [--yolo]", jail)
	}
	host := packload.LaunchFlagsFor(packs, render.ProfileFor(render.KindHost).AgentAutonomy)["copilot"]
	if len(host) != 0 {
		t.Errorf("at the HOST notch copilot's flags = %v, want none: %q is a permission "+
			"bypass and nothing contains the agent on a real machine", host, strings.Join(host, " "))
	}
}

// copilot's GUARDED POSTURE IS ABSENT, and that is the declaration rather than an omission.
//
// A guarded posture exists to TIGHTEN — to assert the safe value where an unsafe one would
// otherwise persist. claude needs one because its autonomous posture writes `managed` keys
// into `~/.claude/settings.json`, a file that survives the render and carries a host layer, so
// a stale `skipDangerousModePermissionPrompt: true` would outlive the notch change. copilot's
// autonomous posture is a LAUNCH FLAG ONLY, and a flag has no persistence: it is composed per
// launch from this table, so NOT selecting it is the whole of the tightening. There is nothing
// for a guarded posture to undo.
//
// Nor is it worth spelling as an empty `guarded.launch` entry for copilot. An absent posture
// and a posture naming the bin with no flags compose the same empty flag list — since the
// `launch` kind was retired there is no ungated list underneath for an entry to subtract
// from — so the entry would assert nothing the absence does not already assert.
//
// packdecl requires only "at least one of autonomous or guarded", so this is valid; `yolo pack
// footprint` reports it as "autonomous posture only", which is the honest disclosure.
func TestCopilotHasNoGuardedPosture(t *testing.T) {
	copilot := packNamed(t, loadAll(t), "copilot")

	ac := copilot.Decl.AutonomyContributions()
	if ac == nil {
		t.Fatal("the copilot pack declares no autonomy contribution — `--yolo` is then outside " +
			"the notch policy again")
	}
	if ac.Autonomous == nil {
		t.Error("copilot's autonomous posture is gone; the jail notch would launch copilot with " +
			"permission prompts on")
	}
	if ac.Guarded != nil {
		t.Error("copilot grew a guarded posture. That may well be right — but it is a decision " +
			"about what `yolo host apply` and `yolo host -- copilot` assert on a real machine, " +
			"so state it in the manifest's commit and rewrite this test's reasoning rather than " +
			"deleting the check")
	}

	// The DECLARED validity of the shape, not just the shipped instance: a posture pair with
	// only `autonomous` is what packdecl accepts, and a pack with neither is what it refuses.
	if _, probs := packdecl.Decode([]byte(
		`{"name":"x","contributes":[{"kind":"autonomy","autonomous":{"launch":[{"bin":"x","flags":["--f"]}]}}]}`,
	)); len(probs) != 0 {
		t.Errorf("an autonomy contribution with only an autonomous posture must be valid: %v", probs)
	}
	if _, probs := packdecl.Decode([]byte(
		`{"name":"x","contributes":[{"kind":"autonomy"}]}`,
	)); len(probs) == 0 {
		t.Error("an autonomy contribution with NEITHER posture should still be a validation error")
	}
}

func packNamed(t *testing.T, packs []*packload.Pack, name string) *packload.Pack {
	t.Helper()
	for _, p := range packs {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no %q pack among the embedded set", name)
	return nil
}
