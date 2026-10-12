package run

// stalewedge_test.go covers the fresh launch's cleared-vs-collision decision for a container whose
// plain removal was refused but that is NOT running (internal/cli/run/run.go's stale-removal block,
// lifecycle.go's clearStaleRemainder).
//
// The wedge a real host met: `podman rm -f` reported "StopSignal SIGTERM failed … resorting to
// SIGKILL" and "cannot remove container … given PID did not die within timeout", leaving a container
// that holds the jail's name. The next launch's stale path ran a plain `rm`, which a non-running
// leftover would accept but a wedged one refuses; then waitForRunningContainer gave up (it never
// runs), and the launch created into `the container name … is already in use`.

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"
	"time"
)

// staleWedgeExec answers podman's running probe (`ps -q`), existence probe (`ps -a -q`) and force
// removal (`rm --force`), in clearStaleRemainder's order. running decides the probes; forceRemoved
// records the removal; existsAfter is what the existence probe answers once a removal has run.
func staleWedgeExec(t *testing.T, running bool, existsAfter *bool, forceRemoved *bool) func([]string, string, []string, time.Duration) ExecResult {
	t.Helper()
	return func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "rm --force"):
			*forceRemoved = true
			*existsAfter = false
			return ExecResult{Ran: true, RC: 0}
		case strings.Contains(joined, "ps -a -q"):
			if *existsAfter {
				return ExecResult{Ran: true, RC: 0, Stdout: "abcd1234deadbeef\n"}
			}
			return ExecResult{Ran: true, RC: 0, Stdout: ""}
		case strings.Contains(joined, "ps -q"):
			if running {
				return ExecResult{Ran: true, RC: 0, Stdout: "abcd1234deadbeef\n"}
			}
			return ExecResult{Ran: true, RC: 0, Stdout: ""}
		}
		return ExecResult{Ran: false}
	}
}

// TestClearStaleRemainderForceRemovesAStoppedLeftover: a container that is not running and would not
// go with a plain `rm` is force-removed, so the launch proceeds instead of colliding.
func TestClearStaleRemainderForceRemovesAStoppedLeftover(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	o := goldenOptions("/ws", t.TempDir())
	exists, forceRemoved := true, false
	o.Exec = staleWedgeExec(t, false, &exists, &forceRemoved)
	if !o.clearStaleRemainder("yolo-wedge", "podman") {
		t.Fatal("a not-running leftover was not force-removed: the launch would collide on the create")
	}
	if !forceRemoved {
		t.Error("no `rm --force` was run, so the leftover was not actually removed")
	}
}

// TestClearStaleRemainderNeverForceRemovesARunningContainer: the runtime reports it RUNNING, so the
// launch must refuse rather than remove it — the "never remove a live container" rule.
func TestClearStaleRemainderNeverForceRemovesARunningContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	o := goldenOptions("/ws", t.TempDir())
	exists, forceRemoved := true, false
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		if strings.Contains(strings.Join(argv, " "), "rm --force") {
			t.Error("force-removed a container the runtime reported RUNNING")
		}
		return staleWedgeExec(t, true, &exists, &forceRemoved)(argv, "", nil, 0)
	}
	if o.clearStaleRemainder("yolo-live", "podman") {
		t.Error("claimed a RUNNING container was cleared")
	}
	if forceRemoved {
		t.Error("a live container was force-removed")
	}
}

// TestClearStaleRemainderRefusesWhenItCannotTell: a runtime that cannot be told whether the
// container is running is never read as "not running" (JL-P3), so nothing is force-removed.
func TestClearStaleRemainderRefusesWhenItCannotTell(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	o := goldenOptions("/ws", t.TempDir())
	forceRemoved := false
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		if strings.Contains(strings.Join(argv, " "), "rm --force") {
			forceRemoved = true
		}
		return ExecResult{Ran: false}
	}
	if o.clearStaleRemainder("yolo-unknown", "podman") {
		t.Error("a probe that could not answer was read as the container not running")
	}
	if forceRemoved {
		t.Error("force-removed without being able to tell whether the container was running")
	}
}

// TestTheFreshLaunchForceRemovesAWedgedLeftover pins the CALL SITE: runContainer's stale path tries
// the plain removal, then clearStaleRemainder. Deleting the helper call would leave the wedge
// unreachable by any of the tests above.
func TestTheFreshLaunchForceRemovesAWedgedLeftover(t *testing.T) {
	pos := map[string]token.Pos{}
	ast.Inspect(funcDecl(t, "run.go", "runContainer"), func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if name := skelCallee(call); pos[name] == token.NoPos {
				pos[name] = call.Pos()
			}
		}
		return true
	})
	for _, name := range []string{"removeStaleContainer", "clearStaleRemainder"} {
		if pos[name] == token.NoPos {
			t.Errorf("runContainer's stale path no longer calls %s", name)
		}
	}
	if pos["removeStaleContainer"] != token.NoPos && pos["clearStaleRemainder"] != token.NoPos &&
		!(pos["removeStaleContainer"] < pos["clearStaleRemainder"]) {
		t.Error("the plain removal must be tried before the force-removal")
	}
}
