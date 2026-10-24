package integration

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/darwinpkg"
)

// Q3'S MEASUREMENT STEP, pinned from Linux under -short
// (docs/design/macos-user-build-step-threat-model.md, Q3).
//
// macos-user.yml builds the macOS floor with `--option sandbox true` and lists what the
// sandbox refuses. That step is shell in YAML, and three ways it could stop measuring anything
// are visible from here, on every push, without a Mac:
//
//   - it could build the wrong attribute. The attribute is `darwinpkg.FloorProfileAttr`, the one
//     every macos-user launch realizes; a rename there (which floor_drift_test.go would carry
//     into flake.nix) would leave the step evaluating a name that no longer exists. The step
//     evaluates `<attr>.name` first and fails loudly, but only on the Mac, a night later.
//   - it could run AFTER the launches. They realize the same floor unsandboxed, so a later step
//     would find nothing to build and report a clean result that measured nothing.
//   - it could fail the job. The answer is a list of refusals, so a refusal is data.
//
// Carries the TestMacosUser prefix for the reason the gate's own tests do: the job that depends
// on the step also verifies it. It is not behind requireMacosUser and needs no Mac.
func TestMacosUserQ3SandboxStepBuildsTheFloorFirst(t *testing.T) {
	wf := uncommentedYAML(readWorkflow(t, "macos-user.yml"))
	const needle = "--option sandbox true"
	step, ok := stepContaining(wf, needle)
	if !ok {
		t.Fatalf("macos-user.yml has no step running `%s`, so Q3's measurement is gone "+
			"(docs/design/macos-user-build-step-threat-model.md)", needle)
	}

	wantAttr := ".#packages.${sys}." + darwinpkg.FloorProfileAttr
	if !strings.Contains(step, wantAttr) {
		t.Errorf("the Q3 step does not build %s — darwinpkg.FloorProfileAttr is %q, the "+
			"attribute every macos-user launch realizes (darwinpkg.BuildFloorProfileArgv). "+
			"A step building anything else measures a closure no launch uses.\nstep:\n%s",
			wantAttr, darwinpkg.FloorProfileAttr, step)
	}
	// THE FLOOR ALONE: a `packages:` list leaking in from the job env would add derivations
	// no default launch builds, and they would read as the floor's refusals.
	if !strings.Contains(step, "env -u YOLO_EXTRA_PACKAGES nix build") {
		t.Errorf("the Q3 build does not clear YOLO_EXTRA_PACKAGES, so the closure it measures " +
			"is not guaranteed to be the floor alone")
	}
	if !strings.Contains(step, "continue-on-error: true") {
		t.Errorf("the Q3 step is not `continue-on-error: true`: a sandbox refusal is the " +
			"ANSWER to Q3, and without the key it turns the whole macos-user verdict red")
	}
	if !strings.Contains(step, "timeout-minutes:") {
		t.Errorf("the Q3 step has no `timeout-minutes`, so a wedged sandboxed build would " +
			"spend the job's whole budget and take the launch tests' results with it")
	}
	// VOID, never a clean result: the setting is restricted and an untrusted user's is ignored.
	if !strings.Contains(step, "ignoring the client-specified setting 'sandbox'") {
		t.Errorf("the Q3 step no longer checks for nix IGNORING `sandbox` (a restricted " +
			"setting), so an unsandboxed build would be reported as a sandboxed one")
	}

	q3 := strings.Index(wf, needle)
	tests := strings.Index(wf, "-run '^TestMacosUser'")
	if tests < 0 {
		t.Fatal("macos-user.yml no longer runs `-run '^TestMacosUser'`; this test's ordering " +
			"check has lost its reference point")
	}
	if q3 > tests {
		t.Errorf("the Q3 step runs AFTER the macos-user launches. They realize the same floor " +
			"unsandboxed, so the sandboxed build would find nothing left to build and report " +
			"a clean result that measured nothing. Move it before the tests.")
	}
}

// nixBuildCmdRe captures one `nix build` command with its backslash continuations, the shape
// machineInitRe reads a `podman machine init` with.
var nixBuildCmdRe = regexp.MustCompile(`(?m)^[ \t]*nix build\b(?:[^\n]*\\\n)*[^\n]*`)

// TestMacosUserQ3CacheMissStepRebuildsAFloorPackage pins Q3's CACHE-MISS step the same way, from
// Linux under -short. The floor step above has never built a floor package (every run
// substituted them), so this one forces one with `nix build --rebuild`, and the ways it could
// stop measuring that are visible from here:
//
//   - the package could stop being a floor entry, and the step would then measure a builder no
//     macos-user launch runs;
//   - it could resolve the package from a nixpkgs other than this flake's locked input, or drop
//     the sandbox flag, the rebuild, the VOID checks, its `continue-on-error` or its cap.
//
// Its position is checked too, though a --rebuild builds whether or not the launches realized
// the package first: before the tests is where its summary sits beside the floor step's.
func TestMacosUserQ3CacheMissStepRebuildsAFloorPackage(t *testing.T) {
	wf := uncommentedYAML(readWorkflow(t, "macos-user.yml"))
	const needle = "--rebuild"
	step, ok := stepContaining(wf, needle)
	if !ok {
		t.Fatalf("macos-user.yml has no step running `nix build %s`, so Q3's cache-miss "+
			"measurement is gone (docs/design/macos-user-build-step-threat-model.md#Q3)", needle)
	}
	m := regexp.MustCompile(`(?m)^\s*pkg=([A-Za-z0-9_.+-]+)\s*$`).FindStringSubmatch(step)
	if m == nil {
		t.Fatalf("the Q3 cache-miss step no longer names its package as `pkg=<name>`, so this "+
			"test cannot check it is a floor entry:\n%s", step)
	}
	if !slices.Contains(darwinpkg.FloorNames(), m[1]) {
		t.Errorf("the Q3 cache-miss step rebuilds %q, which is not in darwinpkg.FloorNames(): it "+
			"would measure a builder no macos-user launch runs. Pick a floor entry", m[1])
	}
	// THE FLAGS ARE READ OFF THE REBUILD'S OWN COMMAND, not the step: the summary's echo lines
	// spell `--rebuild` and `--option sandbox true` too, so a step-wide search passed with both
	// gone from the nix invocation.
	var rebuild string
	for _, c := range nixBuildCmdRe.FindAllString(step, -1) {
		if strings.Contains(c, "--rebuild") {
			rebuild = c
			break
		}
	}
	if rebuild == "" {
		t.Fatalf("the Q3 cache-miss step runs no `nix build … --rebuild`, so it never builds the "+
			"package and every run would read VOID:\n%s", step)
	}
	for _, want := range []struct{ text, why string }{
		{"--inputs-from .", "the package must resolve from this flake's locked nixpkgs, the set the floor reads"},
		{"--option sandbox true", "without it the rebuild says nothing about the sandbox"},
		{`"$inst"`, "the rebuild must name the installable the step realized, every output of it"},
	} {
		if !strings.Contains(rebuild, want.text) {
			t.Errorf("the Q3 cache-miss rebuild lacks %q: %s.\ncommand:\n%s", want.text, want.why, rebuild)
		}
	}
	for _, want := range []struct{ text, why string }{
		{`inst="nixpkgs#${pkg}^*"`, "every output must be realized, or --rebuild refuses to check the derivation"},
		{"aarch64-darwin", "the step must say VOID on a system whose floor reads another nixpkgs input"},
		{"ignoring the client-specified setting 'sandbox'", "an unsandboxed rebuild must read as VOID, not as a result"},
		{"continue-on-error: true", "a refusal is the answer, and must not turn the macos-user verdict red"},
		{"timeout-minutes:", "a wedged compile must not spend the job's whole budget"},
	} {
		if !strings.Contains(step, want.text) {
			t.Errorf("the Q3 cache-miss step lacks %q: %s.\nstep:\n%s", want.text, want.why, step)
		}
	}
	if i, tests := strings.Index(wf, needle), strings.Index(wf, "-run '^TestMacosUser'"); tests < 0 || i > tests {
		t.Errorf("the Q3 cache-miss step does not run before the macos-user tests (rebuild at %d, "+
			"tests at %d)", i, tests)
	}
}

// floorBuildStepName is the step in each macos-user.yml job that realizes the floor before any
// launch does.
const floorBuildStepName = "Build the macOS floor (before any launch)"

// THE FLOOR IS BUILT BEFORE ANY LAUNCH, IN ITS OWN STEP, in both macos-user.yml jobs that
// launch. A cold floor (the rustls mise after a change to its derivation, ~27 min on an
// M-series Mac) used to be paid inside the first test's 30-minute deadline, which timed out and
// kept nothing (lab report 2026-10-24, section 3). Pinned from Linux under -short, like Q3.
func TestMacosUserFloorIsBuiltBeforeAnyLaunch(t *testing.T) {
	wf := uncommentedYAML(readWorkflow(t, "macos-user.yml"))
	split := strings.Index(wf, "\n  install:")
	if split < 0 {
		t.Fatal("macos-user.yml has no `install` job; this test's job split has lost its reference point")
	}
	for _, job := range []struct{ name, body, tests string }{
		{"macos-user", wf[:split], "-run '^TestMacosUser'"},
		{"install", wf[split:], "TestMacosUserPackInstallsVersionsAndConfigures"},
	} {
		step, ok := stepContaining(job.body, floorBuildStepName)
		if !ok {
			t.Errorf("the %s job has no %q step, so its first launch pays a cold floor inside "+
				"a test's deadline", job.name, floorBuildStepName)
			continue
		}
		for _, want := range []struct{ text, why string }{
			{"env -u YOLO_EXTRA_PACKAGES nix build", "the floor alone, as a default launch builds it"},
			{".#packages.${sys}." + darwinpkg.FloorProfileAttr, "the attribute every macos-user launch realizes"},
			{"--accept-flake-config", "the flake's substituter (yolo-jail.cachix.org) must be asked"},
			{"timeout-minutes:", "the step needs its own ceiling, not a test's"},
		} {
			if !strings.Contains(step, want.text) {
				t.Errorf("the %s job's floor step lacks %q: %s.\nstep:\n%s", job.name, want.text, want.why, step)
			}
		}
		for _, bad := range []struct{ text, why string }{
			{"continue-on-error", "a floor that does not build fails every launch, and this step must say so"},
			{"--option sandbox", "the floor a launch uses is built unsandboxed; Q3 measures the sandbox"},
		} {
			if strings.Contains(step, bad.text) {
				t.Errorf("the %s job's floor step has %q: %s", job.name, bad.text, bad.why)
			}
		}
		at, tests := strings.Index(job.body, floorBuildStepName), strings.Index(job.body, job.tests)
		if tests < 0 {
			t.Errorf("the %s job no longer runs %s; the ordering check lost its reference point", job.name, job.tests)
		} else if at > tests {
			t.Errorf("the %s job builds the floor AFTER its launches", job.name)
		}
	}
	// Q3's sandboxed build stays FIRST in the main job: after a plain floor build it would find
	// nothing left to build.
	mainJob := wf[:split]
	if q3, at := strings.Index(mainJob, "--option sandbox true"), strings.Index(mainJob, floorBuildStepName); q3 > at {
		t.Errorf("the plain floor build runs before Q3's sandboxed one, which then measures nothing")
	}
}

// THE DARWIN FLOOR IS PUSHED TO CACHIX WHEN ITS INPUTS CHANGE ON main, from a macOS runner, out
// of a plain build. Every other push to the cache runs on Linux, and a cache is per-system.
func TestDarwinFloorCacheWorkflowPushesTheFloorOnEveryFlakeChange(t *testing.T) {
	wf := uncommentedYAML(readWorkflow(t, "darwin-floor-cache.yml"))
	for _, want := range []struct{ text, why string }{
		{"branches: [main]", "the push must follow main, where users update from"},
		{"- flake.nix", "the profile's inputs are flake.nix and flake.lock"},
		{"- flake.lock", "the profile's inputs are flake.nix and flake.lock"},
		{"aarch64-darwin", "Apple Silicon is where macos-user runs"},
		{"env -u YOLO_EXTRA_PACKAGES nix build", "the floor alone, as a default launch builds it"},
		{".#packages.${sys}." + darwinpkg.FloorProfileAttr, "the attribute every macos-user launch realizes"},
		{`cachix push "$CACHE_NAME" "$OUT"`, "the built profile's closure must be what is pushed"},
		{"secrets.CACHIX_AUTH_TOKEN", "the push is gated on the token, as nightly-macos.yml's is"},
		{"vars.CACHIX_CACHE || 'yolo-jail'", "the cache name comes from the vars context, as nightly-macos.yml's does"},
	} {
		if !strings.Contains(wf, want.text) {
			t.Errorf("darwin-floor-cache.yml lacks %q: %s", want.text, want.why)
		}
	}
	if strings.Contains(wf, "--option sandbox") {
		t.Error("darwin-floor-cache.yml builds with the sandbox option: it must push what an " +
			"unsandboxed launch builds, and Q3 in macos-user.yml is the measurement")
	}
	if strings.Contains(wf, "schedule:") {
		t.Error("darwin-floor-cache.yml has a schedule; its trigger is a flake change on main, " +
			"and a schedule adds runs that push nothing new")
	}
}
