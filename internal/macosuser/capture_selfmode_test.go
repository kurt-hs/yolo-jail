package macosuser

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// capture_selfmode_test.go pins install capture, and a fork's build, in the run-as-you mode
// (macos_user_mode "self", slice 9 of docs/design/macos-user-model-plan.md): the capture runs as
// the invoking user under the deny-by-default capture profile, with no sudo, no access grant and no
// second account anywhere in the plan. None of it measures macOS; the profile's kernel case is
// integration/macosusercaptureseatbelt_test.go's TestMacosUserCaptureSeatbeltSelfProfileDeniesTheMachineStore.

func testSelfCaptureOptions() CaptureOptions {
	opts := testCaptureOptions()
	opts.Mode = ModeSelf
	opts.HostHome = "/Users/you"
	opts.HostUser = "you"
	opts.DarwinUserTempDir = "/private/var/folders/ab/xyz/T/"
	return opts
}

func testSelfForkBuildOptions() ForkBuildOptions {
	opts := testForkBuildOptions()
	opts.Mode = ModeSelf
	opts.HostHome = "/Users/you"
	opts.HostUser = "you"
	return opts
}

// selfCapturePlanArgvs is every argv a capture plan runs: the crossings and the host's commands.
func selfCapturePlanArgvs(plan CapturePlan) [][]string {
	all := [][]string{plan.BootstrapArgv, plan.DriverArgv}
	all = append(all, plan.ScratchGuestCommands...)
	all = append(all, plan.PrepareCommands...)
	all = append(all, plan.StageCommands...)
	all = append(all, plan.CleanupCommands...)
	all = append(all, plan.EnvFileCommands...)
	all = append(all, plan.EnvFileGrantCommands...)
	for _, f := range plan.caTrustFilePlans() {
		all = append(append(all, f.dir...), f.grant...)
	}
	return all
}

// NO SUDO IN THE RUN-AS-YOU CAPTURE: every crossing is /usr/bin/env alone, as the invoking user,
// and nothing the plan runs is a sudo, a grant or a word naming the second account. The plan is
// viable as built. Fails if buildCapturePlanAt goes back to RunAs{} for the run-as-you mode.
func TestSelfModeCapturePlanRunsNoSudo(t *testing.T) {
	opts := testSelfCaptureOptions()
	// A composed trust with a kept CA, so the CA files and their (absent) grants are in the plan.
	opts.CATrust = CATrust{ProfileBundle: "/p/bundle", Kept: []TrustedCA{{Name: "Corp CA"}}, Bundle: "x", Extras: "x"}
	plan := BuildCapturePlan(opts)
	if problems := CapturePlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("a run-as-you capture plan is not viable:\n  %s", strings.Join(problems, "\n  "))
	}
	if plan.RunAs.Mode != ModeSelf {
		t.Fatalf("the plan's crossing is %v, want self", plan.RunAs.Mode)
	}
	for _, argv := range selfCapturePlanArgvs(plan) {
		joined := strings.Join(argv, " ")
		if slices.ContainsFunc(argv, func(w string) bool { return filepath.Base(w) == "sudo" }) {
			t.Errorf("the run-as-you capture runs sudo: %s", joined)
		}
		if strings.Contains(joined, SandboxUser) || strings.Contains(joined, "chmod +a") {
			t.Errorf("the run-as-you capture names the account or grants access: %s", joined)
		}
	}
	for _, argv := range [][]string{plan.BootstrapArgv, plan.DriverArgv} {
		if len(argv) == 0 || argv[0] != envBin {
			t.Errorf("a crossing does not start with %s: %v", envBin, argv)
		}
		if !containsArg(argv, "HOME="+plan.StagingHome) {
			t.Errorf("a crossing does not run against the staging home: %v", argv)
		}
	}
	if !containsArg(plan.DriverArgv, "USER=you") {
		t.Errorf("the driver does not run as you: %v", plan.DriverArgv)
	}
	if !containsArgPair(plan.BootstrapArgv, "/usr/bin/sandbox-exec", "-f", plan.ProfilePath) {
		t.Errorf("the run-as-you bootstrap is not confined under the capture profile: %v", plan.BootstrapArgv)
	}
	if !strings.Contains(plan.Seatbelt, "(deny default)") || strings.Contains(plan.Seatbelt, "(allow default)") {
		t.Errorf("the run-as-you capture does not run under the deny-by-default profile")
	}
	if !strings.Contains(plan.Seatbelt, `(subpath "/private/var/folders/ab/xyz/T")`) {
		t.Errorf("the profile does not grant your DARWIN_USER_TEMP_DIR under the shared-/tmp ruling")
	}
}

// The account mode is untouched by the mode field's existence: the zero value is today's plan.
func TestAccountModeCapturePlanStillCrossesThroughTheRule(t *testing.T) {
	plan := BuildCapturePlan(testCaptureOptions())
	if plan.RunAs.Mode != ModeAccount || len(plan.DriverArgv) == 0 || plan.DriverArgv[0] != sudoBin {
		t.Errorf("the account capture no longer crosses through the rule: %v", plan.DriverArgv)
	}
	if !strings.Contains(plan.Seatbelt, "(allow default)") {
		t.Error("the account capture stopped using its own profile")
	}
}

// Each run-as-you promise is a call site, and swapping any one back to the account mode's makes
// the plan unviable: the account profile, the account's staging tree with its group grants, an
// unconfined bootstrap, a sudo crossing, and no home for you.
func TestSelfModeCapturePlanInvariantsRefuseEachAccountPiece(t *testing.T) {
	base := BuildCapturePlan(testSelfCaptureOptions())
	account := BuildCapturePlan(testCaptureOptions())
	for name, tc := range map[string]struct {
		mutate func(*CapturePlan)
		want   string
	}{
		"account profile": {func(p *CapturePlan) { p.Seatbelt = SeatbeltCaptureProfile(p.StagingRoot) }, "deny-by-default"},
		"account staging": {func(p *CapturePlan) { p.PrepareCommands = CaptureStagingCommands(p.StagingRoot, "you") },
			"grants access to another account"},
		"unconfined bootstrap": {func(p *CapturePlan) {
			p.BootstrapArgv = DarwinBootstrapArgv(p.RunAs, p.StagedYolo, p.StagingHome, nil, "")
		}, "bootstrap does not run under"},
		"sudo crossing": {func(p *CapturePlan) { p.DriverArgv = account.DriverArgv }, "alone"},
		"env grant":     {func(p *CapturePlan) { p.EnvFileGrantCommands = account.EnvFileGrantCommands }, "grants access"},
		"no home": {func(p *CapturePlan) {
			p.RunAs.Homes = Homes{Home: p.StagingHome}
			p.Seatbelt = SeatbeltSelfCaptureProfile(p.StagingRoot, p.RunAs.Homes, "you", "", false)
		}, "no absolute home for you"},
	} {
		p := base
		tc.mutate(&p)
		problems := strings.Join(CapturePlanInvariants(p), "\n")
		if !strings.Contains(problems, tc.want) {
			t.Errorf("%s: no problem names %q:\n%s", name, tc.want, problems)
		}
	}
}

// An unnamed home is refused rather than resolved against the cwd: the machine root and shared
// store the profile keeps out would be relative paths, or nothing.
func TestSelfModeCaptureWithNoHomeIsNotViable(t *testing.T) {
	opts := testSelfCaptureOptions()
	opts.HostHome = ""
	problems := strings.Join(CapturePlanInvariants(BuildCapturePlan(opts)), "\n")
	if !strings.Contains(problems, "no absolute home for you") {
		t.Errorf("a run-as-you capture with no home is viable:\n%s", problems)
	}
}

// A FORK'S BUILD TAKES THE SAME MODE: the sealed deny-by-default profile, a staging tree that is
// yours alone with its src/ leaf, and no sudo. Fails if BuildForkBuildPlan swaps the account's
// sealed profile or staging commands back in.
func TestSelfModeForkBuildPlanIsSealedAndRunsNoSudo(t *testing.T) {
	plan := BuildForkBuildPlan(testSelfForkBuildOptions())
	if problems := ForkBuildPlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("a run-as-you fork build plan is not viable:\n  %s", strings.Join(problems, "\n  "))
	}
	if strings.Contains(plan.Seatbelt, "(allow default)") || !strings.Contains(plan.Seatbelt, sealedBuildNixSocketDeny) {
		t.Errorf("the run-as-you build does not run under the sealed deny-by-default profile")
	}
	for _, argv := range append(selfCapturePlanArgvs(plan.CapturePlan), plan.SourceCommands...) {
		joined := strings.Join(argv, " ")
		if strings.Contains(joined, "sudo") || strings.Contains(joined, SandboxUser) || strings.Contains(joined, "chmod +a") {
			t.Errorf("the run-as-you build runs sudo, names the account or grants: %s", joined)
		}
	}
	if !commandsInclude(plan.PrepareCommands, []string{mkdirBin, "-p", plan.SrcDir}) {
		t.Errorf("the run-as-you build never makes its checkout dir: %v", plan.PrepareCommands)
	}
	if n := len(plan.BootstrapArgv); n == 0 || plan.BootstrapArgv[n-1] != DarwinBootstrapSealedFlag {
		t.Errorf("the run-as-you build's bootstrap is not told it is sealed: %v", plan.BootstrapArgv)
	}
	// Swapping the account's sealed profile back in is refused.
	plan.Seatbelt = SeatbeltSealedCaptureProfile(plan.StagingRoot)
	if !strings.Contains(strings.Join(ForkBuildPlanInvariants(plan), "\n"), "deny-by-default") {
		t.Error("a run-as-you build under the account's sealed profile is viable")
	}
}

// THE RUN-AS-YOU ACTS ASK NO ACCOUNT AND NO RUN-AS RULE, and run no sudo: on a Mac with neither,
// both acts run every step as you, and an absent staging root is made, as you. Fails if
// captureGatesPass asks the account's gates in the run-as-you mode, or a crossing is a sudo.
func TestSelfModeCaptureActsNeedNoAccountAndRunNoSudo(t *testing.T) {
	tmp := t.TempDir()
	capOpts := testSelfCaptureOptions()
	capOpts.CaptureRoot = filepath.Join(tmp, "captures")
	forkOpts := testSelfForkBuildOptions()
	forkOpts.Darwin = nil
	forkOpts.CaptureRoot = filepath.Join(tmp, "captures")
	for name, act := range map[string]captureAct{
		"capture": {CaptureStagingRoot(capOpts.CaptureRoot, capOpts.Bin), func(d Deps) int {
			return RunCaptureAct(d, capOpts, filepath.Join(tmp, "store", "capture-out"), false)
		}},
		"fork": {ForkBuildStagingRoot(forkOpts.CaptureRoot, forkOpts.BuildID), func(d Deps) int {
			return RunForkBuildAct(d, forkOpts, filepath.Join(tmp, "store", "fork-out"),
				filepath.Join(tmp, "store", "toolchain"), false)
		}},
	} {
		var rec []string
		var buf bytes.Buffer
		d := captureActDeps(t, &rec, act.root)
		d.Out = &buf
		d.SandboxUserExists = func() bool { return false }
		d.RunAsGrant = func() bool { return false }
		stagingMade := false
		d.StagingRootFacts = func(p string) StagingRootFacts {
			if p == StagingRoot() && !stagingMade {
				return StagingRootFacts{}
			}
			return StagingRootFacts{Exists: true, IsDir: true, UID: 501, Mode: 0o755}
		}
		run := d.Run
		d.Run = func(argv []string) int {
			if len(argv) == 4 && argv[0] == mkdirBin && argv[3] == StagingRoot() {
				stagingMade = true
			}
			return run(argv)
		}
		if rc := act.run(d); rc != 0 {
			t.Fatalf("%s: rc = %d with no account and no rule\n%s", name, rc, buf.String())
		}
		if recAt(rec, "run:"+mkdirBin+" -m "+categoryDirMode+" "+StagingRoot()) < 0 {
			t.Errorf("%s: the absent staging root was not made as you:\n%s", name, strings.Join(rec, "\n"))
		}
		crossings := 0
		for _, r := range rec {
			if strings.Contains(r, "sudo") || strings.Contains(r, SandboxUser) {
				t.Errorf("%s: %s", name, r)
			}
			if strings.HasPrefix(r, "run:"+envBin+" -i ") {
				crossings++
			}
		}
		if crossings < 2 {
			t.Errorf("%s: the bootstrap and driver were not run as you through %s:\n%s", name, envBin,
				strings.Join(rec, "\n"))
		}
	}
}

// The account mode's acts still refuse a Mac with no rule (TestACaptureRefusesBeforeAnyWorkWhenTheRuleIsMissing),
// and the run-as-you mode's still refuse a foreign staging root: the staging root is the privilege
// fact it keeps.
func TestSelfModeCaptureRefusesAForeignStagingRoot(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.StagingRootFacts = func(p string) StagingRootFacts {
		if p == StagingRoot() {
			return StagingRootFacts{Exists: true, IsDir: true, UID: 602, Mode: 0o755}
		}
		return StagingRootFacts{Exists: true, IsDir: true, UID: 501, Mode: 0o755}
	}
	if rc := RunCaptureAct(d, testSelfCaptureOptions(), t.TempDir(), false); rc != 1 {
		t.Errorf("rc = %d", rc)
	}
	if !strings.Contains(buf.String(), StagingRoot()+" belongs to another account") || len(rec) != 0 {
		t.Errorf("printed %q and ran %v", buf.String(), rec)
	}
}

// PrivilegeUnmetFor in the run-as-you mode asks the staging root alone: a missing rule and a
// missing account are no refusal, an absent staging root holds (the act makes it), and a foreign
// one is refused.
func TestPrivilegeUnmetForSelfModeReducesToTheStagingRoot(t *testing.T) {
	d := mockDeps(nil)
	d.SandboxUserExists = func() bool { return false }
	d.RunAsGrant = func() bool { return false }
	d.StagingRootFacts = func(string) StagingRootFacts {
		return StagingRootFacts{Exists: true, IsDir: true, UID: 501, Mode: 0o755}
	}
	if c, unmet := PrivilegeUnmetFor(ModeSelf, d.launchProbes()); unmet {
		t.Errorf("self mode refused for %s with no account and no rule", c.ID)
	}
	if _, unmet := PrivilegeUnmetFor(ModeAccount, d.launchProbes()); !unmet {
		t.Error("the account mode stopped asking the rule")
	}
	d.StagingRootFacts = func(string) StagingRootFacts { return StagingRootFacts{} }
	if c, unmet := PrivilegeUnmetFor(ModeSelf, d.launchProbes()); unmet {
		t.Errorf("self mode refused an absent staging root (%s), which the act makes", c.ID)
	}
	d.StagingRootFacts = func(string) StagingRootFacts {
		return StagingRootFacts{Exists: true, IsDir: true, UID: 602, Mode: 0o755}
	}
	if c, unmet := PrivilegeUnmetFor(ModeSelf, d.launchProbes()); !unmet || c.ID != PreconditionStagingRootOwner {
		t.Errorf("self mode accepted a foreign staging root (%v, %s)", unmet, c.ID)
	}
}

// The dry run says who the capture runs as, and prints no sudo.
func TestSelfModeCaptureDryRunSaysItRunsAsYou(t *testing.T) {
	var buf bytes.Buffer
	plan := BuildCapturePlan(testSelfCaptureOptions())
	PrintCapturePlan(&buf, plan, CapturePlanInvariants(plan))
	out := buf.String()
	if !strings.Contains(out, "as you, inside the capture profile") || strings.Contains(out, "sudo rule") ||
		!strings.Contains(out, "all capture plan invariants hold") {
		t.Errorf("the run-as-you dry run does not say so:\n%s", out)
	}
}

// MMP-D21: a run-as-you capture and a run-as-you build each get a TMPDIR of their own inside the
// staging tree, which their profile makes writable, because that profile closes the shared /tmp
// their `env -i` children would otherwise fall back to. It reaches the driver (the env file) and
// the bootstrap (its env -i list), the prepare step makes it, and the account mode sets none.
// The user's hatch reaches both profiles. Deleting the TMPDIR call site, the bootstrap relay, the
// leaf, or the hatch's plumbing fails this.
func TestSelfModeCaptureAndBuildGetTheirOwnTmpdir(t *testing.T) {
	capOpts := testSelfCaptureOptions()
	buildOpts := testSelfForkBuildOptions()
	plans := map[string]CapturePlan{
		"capture": BuildCapturePlan(capOpts),
		"build":   BuildForkBuildPlan(buildOpts).CapturePlan,
	}
	for name, plan := range plans {
		tmp := CaptureStagingTmp(plan.StagingRoot)
		if v, ok := sandboxEnvFileValue(plan.EnvFileContent, "TMPDIR"); !ok || v != tmp {
			t.Errorf("%s: the env file sets TMPDIR=%q (present %v), want %q", name, v, ok, tmp)
		}
		if !containsArg(plan.BootstrapArgv, "TMPDIR="+tmp) {
			t.Errorf("%s: the bootstrap is not given TMPDIR=%s: %v", name, tmp, plan.BootstrapArgv)
		}
		if !slices.ContainsFunc(plan.PrepareCommands, func(c []string) bool {
			return slices.Equal(c, []string{mkdirBin, "-p", tmp})
		}) {
			t.Errorf("%s: no prepare command makes %s: %v", name, tmp, plan.PrepareCommands)
		}
		if !strings.HasPrefix(tmp, plan.StagingRoot+"/") ||
			!strings.Contains(plan.Seatbelt, `(subpath "`+plan.StagingRoot+`")`) {
			t.Errorf("%s: the TMPDIR %s is not inside the profile's writable staging tree", name, tmp)
		}
		if strings.Contains(plan.Seatbelt, "#seatbelt-test-id:"+SharedTmpAllowID+"#") {
			t.Errorf("%s: the shared /tmp is open with no hatch", name)
		}
	}
	capOpts.SharedTmp, buildOpts.SharedTmp = true, true
	for name, plan := range map[string]CapturePlan{
		"capture": BuildCapturePlan(capOpts),
		"build":   BuildForkBuildPlan(buildOpts).CapturePlan,
	} {
		if !strings.Contains(plan.Seatbelt, "#seatbelt-test-id:"+SharedTmpAllowID+"#") {
			t.Errorf("%s: the hatch did not open the shared /tmp", name)
		}
	}
	account := BuildCapturePlan(testCaptureOptions())
	if _, ok := sandboxEnvFileValue(account.EnvFileContent, "TMPDIR"); ok ||
		slices.ContainsFunc(account.BootstrapArgv, func(w string) bool { return strings.HasPrefix(w, "TMPDIR=") }) {
		t.Error("the account-mode capture was given a TMPDIR")
	}
}
