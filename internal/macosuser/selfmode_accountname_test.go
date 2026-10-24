package macosuser

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// selfmode_accountname_test.go pins the two run-as-you defects of
// docs/lab-reports/2026-10-24-mac-verification-after-gitbroker.md sections 1b and 1c, on Linux:
// a run-as-you capture's bootstrap is told your real home and may stat it (MMP-D22), and a
// diagnostic that mentions the second account never makes a plan unviable (MMP-D23).

// forkBuildFailureWire is a YOLO_FORK_BUILDS value as a failed run-as-you build leaves it: its
// reason quotes the bootstrap's stderr, which names the account's home.
const forkBuildFailureWire = `{"mupacktree-x":{"reason":"mupacktree-x was not started: its build failed",` +
	`"cause":{"lines":["The end of its stderr:","yolo internal darwin-bootstrap: cannot read the sandbox ` +
	`account's home /Users/_yolojail (stat /Users/_yolojail: operation not permitted)"]}}}`

// A BUILD FAILURE'S TEXT LEAVES THE LAUNCH VIABLE (MMP-D23): the decision rides the bootstrap's
// env -i list through the real relay (buildBootstrapEnv), names /Users/_yolojail, and the plan
// still passes, so the missing-programs hatch can start the sandbox. Red with the viability check
// back on "any word containing _yolojail".
func TestSelfModeABuildFailureNamingTheAccountKeepsThePlanViable(t *testing.T) {
	env := jsonx.NewOrderedMap()
	env.Set(entrypoint.ForkBuildsEnv, forkBuildFailureWire)
	plan := BuildRunPlanWithDaemons("/Users/you/code/alpha", jsonx.NewOrderedMap(), nil, []string{"claude"},
		"/usr/local/bin/yolo", "", HomeOverlay{}, HostContext{}, env, nil, nil,
		JailDaemons{}, FloorStage{}, PlanSession{Mode: ModeSelf, HostHome: "/Users/you", HostUser: "you",
			DarwinUserTempDir: "/private/var/folders/ab/xyz/T/"})
	if !containsArg(plan.BootstrapArgv, entrypoint.ForkBuildsEnv+"="+forkBuildFailureWire) {
		t.Fatalf("the fixture's decision does not reach the bootstrap argv, so this test proves nothing: %v",
			plan.BootstrapArgv)
	}
	if problems := PlanInvariants(plan); len(problems) != 0 {
		t.Errorf("a build failure's text made the run-as-you plan unviable:\n  %s", strings.Join(problems, "\n  "))
	}

	// The same plan with a word that LOCATES something in the account's home is still refused.
	for name, word := range map[string]string{
		"a path value":   "HOME=/Users/_yolojail",
		"a path list":    "PATH=/usr/bin:/Users/_yolojail/.local/bin",
		"a flag":         "--home=/Users/_yolojail",
		"a program":      "/Users/_yolojail/bin/yolo",
		"the bare name":  SandboxUser,
		"a -D parameter": "YOLO_STATE=/Users/_yolojail/.yolo",
	} {
		p := plan
		p.BootstrapArgv = append(append([]string(nil), plan.BootstrapArgv[:2]...), append([]string{word},
			plan.BootstrapArgv[2:]...)...)
		if !hasProblem(PlanInvariants(p), "names the second account") {
			t.Errorf("%s naming the account (%s) passed the run-as-you check", name, word)
		}
	}
}

// The word classes, one at a time: text is never a location, and each location form is judged.
func TestArgvWordNamingAccountJudgesOnlyLocations(t *testing.T) {
	for _, w := range []string{
		entrypoint.ForkBuildsEnv + "=" + forkBuildFailureWire,
		"YOLO_REASON=the account _yolojail is missing",
		"cd '/Users/_yolojail' && exec claude", // a shell body, which the env-pair checks cover
		"capture-run",
		"NOT AN ENV=/Users/_yolojail",
	} {
		if got, ok := argvWordNamingAccount([]string{"/usr/bin/env", "-i", w}); ok {
			t.Errorf("text was judged a location: %q", got)
		}
	}
	for _, w := range []string{"HOME=/Users/_yolojail", "-u", "/Users/_yolojail", "--user=_yolojail", "_yolojail",
		"TMPDIR=/private/tmp::/Users/_yolojail"} {
		if !strings.Contains(w, SandboxUser) {
			continue
		}
		if _, ok := argvWordNamingAccount([]string{"/usr/bin/env", w}); !ok {
			t.Errorf("a location naming the account passed: %q", w)
		}
	}
}

// A RUN-AS-YOU CAPTURE'S BOOTSTRAP IS TOLD YOUR REAL HOME, AND ITS PROFILE LETS IT STAT IT
// (MMP-D22). Red with capture.go's self branch no longer setting YOLO_DARWIN_REAL_HOME, or
// SeatbeltSelfCaptureProfile no longer setting HostHomeStat: the bootstrap then takes the account
// mode's identity check and stats /Users/_yolojail, which the lab report's witness shows.
func TestSelfModeCaptureBootstrapIsToldYourRealHome(t *testing.T) {
	capture := BuildCapturePlan(testSelfCaptureOptions())
	build := BuildForkBuildPlan(testSelfForkBuildOptions())
	if problems := CapturePlanInvariants(capture); len(problems) != 0 {
		t.Errorf("capture: not viable:\n  %s", strings.Join(problems, "\n  "))
	}
	if problems := ForkBuildPlanInvariants(build); len(problems) != 0 {
		t.Errorf("fork build: not viable:\n  %s", strings.Join(problems, "\n  "))
	}
	for name, plan := range map[string]CapturePlan{"capture": capture, "fork build": build.CapturePlan} {
		if !containsArg(plan.BootstrapArgv, paths.DarwinRealHomeEnv+"=/Users/you") {
			t.Errorf("%s: the bootstrap is not told your real home: %v", name, plan.BootstrapArgv)
		}
		if !strings.Contains(plan.Seatbelt, HostHomeStatAllow("/Users/you")) {
			t.Errorf("%s: the profile does not let the bootstrap stat your real home", name)
		}
		// Neither the machine root nor the shared store rides with it: the profile denies both.
		for _, k := range []string{entrypoint.DarwinMachineHomeEnv, entrypoint.DarwinSharedHomeEnv} {
			for _, w := range plan.BootstrapArgv {
				if strings.HasPrefix(w, k+"=") {
					t.Errorf("%s: the capture bootstrap names %s", name, w)
				}
			}
		}
	}
	// The account mode's capture names no real home.
	for _, w := range BuildCapturePlan(testCaptureOptions()).BootstrapArgv {
		if strings.HasPrefix(w, paths.DarwinRealHomeEnv+"=") {
			t.Errorf("the account capture's bootstrap names a real home: %s", w)
		}
	}
}

// The capture plan check catches each half missing, on Linux, before a Mac sees it.
func TestSelfModeCaptureInvariantsRequireTheRealHome(t *testing.T) {
	base := BuildCapturePlan(testSelfCaptureOptions())
	noVar := base
	noVar.BootstrapArgv = nil
	for _, w := range base.BootstrapArgv {
		if !strings.HasPrefix(w, paths.DarwinRealHomeEnv+"=") {
			noVar.BootstrapArgv = append(noVar.BootstrapArgv, w)
		}
	}
	if !hasProblem(CapturePlanInvariants(noVar), "not told your real home") {
		t.Errorf("a capture bootstrap with no real home passed: %v", CapturePlanInvariants(noVar))
	}
	noStat := base
	noStat.Seatbelt = strings.Replace(base.Seatbelt, HostHomeStatAllow("/Users/you"), "", 1)
	if noStat.Seatbelt == base.Seatbelt {
		t.Fatal("the fixture's profile has no stat block to remove")
	}
	if !hasProblem(CapturePlanInvariants(noStat), "stat your real home") {
		t.Errorf("a capture profile with no stat of the real home passed: %v", CapturePlanInvariants(noStat))
	}
}

// The stat block is metadata only, on the home and its ancestors, and comes before every closing
// deny, so SBPL's last match keeps ~/.ssh and the store closed.
func TestHostHomeStatAllowIsMetadataOnly(t *testing.T) {
	got := HostHomeStatAllow("/Users/you/")
	want := "(allow file-read-metadata\n    (literal \"/Users/you\")\n    (literal \"/Users\"))\n"
	if !strings.Contains(got, want) {
		t.Errorf("the stat block is not metadata on the home and its parents:\n%s", got)
	}
	if strings.Contains(got, "subpath") || strings.Contains(got, "file-read*") {
		t.Errorf("the stat block grants more than metadata:\n%s", got)
	}
	if HostHomeStatAllow("") != "" || HostHomeStatAllow("relative") != "" {
		t.Error("a home that is not absolute rendered a rule")
	}
	p := SeatbeltSelfCaptureProfile(testStagingRoot, testSelfHomes(), "you", "", false)
	at := strings.Index(p, "#seatbelt-test-id:"+HostHomeStatID+"#")
	if at < 0 || at > strings.Index(p, "#seatbelt-test-id:host-secrets-deny#") {
		t.Errorf("the stat block is missing or follows the closing denies")
	}
}
