package macosuser

// buildactsealed_test.go pins PPX-D48 (docs/reference/pi-extensions.md) at the macos-user build act:
// its bootstrap is told it is a sealed build's, on its argv, so the staging home gets no agent's
// config and the build's output no notice about one. An installer capture's bootstrap is not, since
// the installer it runs is the launcher a pack surface renders.

import (
	"slices"
	"strings"
	"testing"
)

func TestTheBuildActsBootstrapIsToldItIsASealedBuild(t *testing.T) {
	for _, opts := range []ForkBuildOptions{testForkBuildOptions(), testTreeBuildOptions("npm ci")} {
		plan := BuildForkBuildPlan(opts)
		if n := len(plan.BootstrapArgv); n == 0 || plan.BootstrapArgv[n-1] != DarwinBootstrapSealedFlag {
			t.Errorf("the build's bootstrap argv does not end in %s: %q", DarwinBootstrapSealedFlag, plan.BootstrapArgv)
		}
		// The flag is the bootstrap's own, after its subcommand, never an env pair the user's channel
		// could carry.
		if i := slices.Index(plan.BootstrapArgv, "darwin-bootstrap"); i < 0 || i != len(plan.BootstrapArgv)-2 {
			t.Errorf("the sealed flag does not follow darwin-bootstrap: %q", plan.BootstrapArgv)
		}
	}
	if slices.Contains(BuildCapturePlan(testCaptureOptions()).BootstrapArgv, DarwinBootstrapSealedFlag) {
		t.Error("an installer capture's bootstrap is sealed: it would render no launcher for the installer to run")
	}
}

// THE CALL SITE IS PINNED: a build plan whose bootstrap lost the flag is refused.
func TestABuildPlanWhoseBootstrapIsNotSealedIsRefused(t *testing.T) {
	plan := BuildForkBuildPlan(testForkBuildOptions())
	plan.BootstrapArgv = plan.BootstrapArgv[:len(plan.BootstrapArgv)-1]
	if problems := strings.Join(ForkBuildPlanInvariants(plan), "\n"); !strings.Contains(problems, "sealed build") {
		t.Errorf("the unsealed bootstrap is not refused:\n%s", problems)
	}
}
