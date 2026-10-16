package run

import (
	"os"
	"strings"
	"testing"
)

// hostsingletonwiring_test.go pins a one-line PRODUCTION WIRING that no behavioural test in
// this package reaches: the container argv's insert loop skipping a handle with no variable
// name. It was found by mutation: deleting it left the entire unit gate green, which is the
// shape AGENTS.md names — the callee is pinned, the caller is not.
//
// # Why this is a source assertion
//
// The insert loop lives inside the container launch, which needs a container. So this asserts
// the EXPRESSION. That is weaker than driving it, and it is the strongest thing available at
// this seam; the alternative measured at the time was nothing at all. (Its sibling, the
// direction of the old `scope` dispatch, went with the dispatch: HD-R1 left nothing to
// dispatch on.)

// runSource reads run.go, the file both assertions below are about.
func runSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("run.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestHostScopedHandleContributesNoContainerEnv is the CONSUMER half of
// TestOnPodmanAHostScopedRecordRunsAsTheJailsOwnDaemon's envVarName assertion, and without it that
// test pins a value nobody is required to read.
//
// A handle whose endpoint the assembler promised (endpointPromisedAtAssembly) has an EMPTY
// envVarName, deliberately: its jail-facing variable is emitted much earlier, by
// hostServicesMountArgs at argv-assembly time (see that function's own note on why
// the optimism is load-bearing). runNormal's insert loop must therefore SKIP such a
// handle.
//
// Delete the skip and the loop appends `-e` followed by `"" + "=" + jailPath` — a
// literal `-e =/var/lib/yolo-jail/host-services/claude-oauth-broker.endpoint` in the
// container argv of every jail that selected `packs: ["claude"]` with a live broker.
// That is an environment variable with no name, on every launch, for the one loophole
// that is on by default. Measured 2026-08-19: removing the skip passes `go test
// -short ./...` in full.
func TestHostScopedHandleContributesNoContainerEnv(t *testing.T) {
	src := runSource(t)
	if !strings.Contains(src, `if svc.envVarName == "" {`) {
		t.Error("runNormal's host-service insert loop no longer skips a handle with no env " +
			"var name. A host-scoped loophole's handle carries one by design " +
			"(endpointPromisedAtAssembly), so without the skip every claude jail's " +
			"argv gains a nameless `-e =<path>` pair")
	}
	// The control: the loop must still INSERT for a handle that does carry a name, or
	// the assertion above would be satisfied by a loop that emits nothing at all and
	// every per-jail loophole would lose its endpoint variable.
	if !strings.Contains(src, `svc.envVarName + "=" + svc.jailPath`) {
		t.Error("runNormal no longer inserts `-e <VAR>=<jailPath>` for a handle that has a " +
			"variable — the skip above is only correct while the ordinary path still runs")
	}
}
