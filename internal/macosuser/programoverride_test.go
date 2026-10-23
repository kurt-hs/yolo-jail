package macosuser

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// programoverride_test.go pins a DEV OVERRIDE on this backend (docs/design/pack-buildable-programs.md
// §5, BG-D9; the plan's Task 10): the checkout a program runs from crosses as a read-only context link
// marked with its program (ContextLink.Override), and BOTH profiles — the account mode's and the
// run-as-you mode's deny-by-default one — open it for read and exec at its resolved path in a block of
// its own (program-override-read-allow), keep it out of the mounts' read allow, deny its writes, and
// place the grant before the denies that must still win. The plan stages its link, asks the DAC
// preflight of it and refuses a plan whose profile lacks the grant. Whether the kernel lets the
// launcher run the file is integration/macosuserdenydefault_test.go's and macosuserseatbelt_test.go's,
// on a Mac.

// overrideLink is one override's link, outside the workspace in a place both modes site.
var overrideLink = ContextLink{Dest: "/ctx/program-overrides/board", Source: "/Users/Shared/dev/board",
	Named: "/Users/Shared/dev/board", Dir: true, Override: "board"}

// overrideGrantProblems is what the rendered profile lacks of the override's grant, in order.
func overrideGrantProblems(profile, src string) []string {
	var out []string
	clause := "(subpath " + sbplStr(src) + ")"
	block := profileBlock(profile, ProgramOverrideReadAllowID)
	if !strings.Contains(block, "(allow file-read*\n    "+clause) {
		out = append(out, "no read allow at "+src+" in "+ProgramOverrideReadAllowID+":\n"+block)
	}
	if !strings.Contains(block, "(allow process-exec\n    "+clause) {
		out = append(out, "no exec allow at "+src+" in "+ProgramOverrideReadAllowID+":\n"+block)
	}
	if strings.Contains(profileBlock(profile, "context-read-allow"), clause) {
		out = append(out, "the override's checkout is in the context mounts' read allow, not its own block")
	}
	if !strings.Contains(profileBlock(profile, "context-readonly-deny"), clause) {
		out = append(out, "the override's checkout is not write-denied (context-readonly-deny)")
	}
	return out
}

func TestBothProfilesOpenADevOverrideForReadAndExec(t *testing.T) {
	ctx := []ContextLink{roLink, overrideLink}
	account := SeatbeltProfileWithContext(ctxWorkspace, SandboxHome(), nil, HomeReadonly{}, ctx, nil)
	for _, p := range overrideGrantProblems(account, overrideLink.Source) {
		t.Errorf("the account profile: %s", p)
	}
	// The grant re-opens what the /Users read deny closed, and the keychain deny still wins after it.
	mustPrecede(t, account, "#seatbelt-test-id:users-read-deny#", "#seatbelt-test-id:"+ProgramOverrideReadAllowID+"#",
		"the /Users read deny would close the checkout again")
	mustPrecede(t, account, "#seatbelt-test-id:"+ProgramOverrideReadAllowID+"#", "#seatbelt-test-id:library-keychains-deny#",
		"a checkout naming a keychain would re-open it")

	pp := testProfileParams()
	pp.Context = ctx
	self := DenyDefaultProfile(pp)
	for _, p := range overrideGrantProblems(self, overrideLink.Source) {
		t.Errorf("the deny-default profile: %s", p)
	}
	mustPrecede(t, self, "#seatbelt-test-id:"+ProgramOverrideReadAllowID+"#", "#seatbelt-test-id:host-secrets-deny#",
		"a checkout holding a secret would re-open it")
	if !strings.Contains(profileBlock(self, "ancestor-literal-allow"), `(literal "/Users/Shared/dev")`) {
		t.Error("the checkout's ancestors are not listed, so realpath() of the file it runs fails")
	}
	// No override, no block: a launch without one renders the profile it rendered before.
	if strings.Contains(SeatbeltProfileWithContext(ctxWorkspace, SandboxHome(), nil, HomeReadonly{}, []ContextLink{roLink}, nil),
		ProgramOverrideReadAllowID) {
		t.Error("a profile with no override renders its grant")
	}
}

// THE PLAN CARRIES IT IN BOTH MODES: staged as a link at its /ctx path, asked of the DAC preflight,
// and opened by the profile, with every invariant held; and PlanInvariants refuses a plan whose
// profile has no grant for it, naming the override.
func TestAPlanDeliversADevOverrideInBothModes(t *testing.T) {
	plans := map[string]RunPlan{
		"account": planWithLinks(t, overrideLink),
		"self":    selfRunPlanWith(t, ctxWorkspace, HostContext{Links: []ContextLink{overrideLink}}),
	}
	for mode, plan := range plans {
		if probs := PlanInvariants(plan); len(probs) > 0 {
			t.Errorf("%s: a plan delivering an override violates its invariants:\n%s", mode, strings.Join(probs, "\n"))
		}
		if !stagesContextLink(plan.StageCommands, plan.ContextDir, overrideLink) {
			t.Errorf("%s: the override is not staged at $YOLO_CONTEXT_DIR/program-overrides/board", mode)
		}
		for _, p := range overrideGrantProblems(plan.Seatbelt, overrideLink.Source) {
			t.Errorf("%s: %s", mode, p)
		}
		asked := false
		for _, p := range plan.ContextPreflight {
			asked = asked || (p.Link.Override == "board" && p.Access == "read")
		}
		if !asked {
			t.Errorf("%s: the DAC preflight never asks for the override's checkout", mode)
		}
		broken := plan
		broken.Seatbelt = strings.Replace(plan.Seatbelt, "#seatbelt-test-id:"+ProgramOverrideReadAllowID+"#", "", 1)
		if probs := strings.Join(PlanInvariants(broken), "\n"); !strings.Contains(probs, "the dev override of board") {
			t.Errorf("%s: PlanInvariants admitted a profile with no grant for the override:\n%s", mode, probs)
		}
	}
	if got := overrideLink.Origin(); got != "programs.overrides.board" {
		t.Errorf("an override link names its origin %q, want the config key", got)
	}
}

// THE BOOTSTRAP IS HANDED THE DECISIONS: the launch env's YOLO_PROGRAM_OVERRIDES crosses to the darwin
// bootstrap, whose launcher generation writes the override's launcher from it, as YOLO_FORK_BUILDS
// does. Drop the relay from buildBootstrapEnv and the sandbox runs the pack's own copy.
func TestTheBootstrapIsHandedTheDevOverrides(t *testing.T) {
	env := jsonx.NewOrderedMap()
	wire := `{"board":{"dir":"/Users/Shared/dev/board","run":"dist/board.js","host":"/Users/Shared/dev/board"}}`
	env.Set(entrypoint.ProgramOverridesEnv, wire)
	plan := BuildRunPlan(ctxWorkspace, jsonx.NewOrderedMap(), []string{"claude"},
		[]string{"/bin/zsh", "-l"}, "/usr/local/bin/yolo", hostStaged, HomeOverlay{},
		HostContext{Links: []ContextLink{overrideLink}}, env, nil, nil)
	if !argvMentions(plan.BootstrapArgv, entrypoint.ProgramOverridesEnv+"="+wire) {
		t.Errorf("the bootstrap argv does not carry %s:\n%v", entrypoint.ProgramOverridesEnv, plan.BootstrapArgv)
	}
}
