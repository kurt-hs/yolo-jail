package cli

// stopforce_test.go pins `yolo stop --force`: the flag parses (both spellings) and is documented,
// the container stop drops the grace it otherwise gives, force reaches the keeper's teardown, and a
// flag stop does not define is refused as misuse rather than silently dropped.

import (
	"bytes"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// TestStopForceIsParsedAndDocumented: both spellings parse, a plain stop asks for none, and the help
// names the flag and the new usage line.
func TestStopForceIsParsedAndDocumented(t *testing.T) {
	for _, args := range [][]string{{"stop", "--force"}, {"stop", "-f"}} {
		if !stopForceRequested(args) {
			t.Errorf("stopForceRequested(%v) = false, want true", args)
		}
	}
	if stopForceRequested([]string{"stop"}) {
		t.Error("a plain stop asked for force")
	}
	for _, want := range []string{"--force", "-f", "Usage: yolo stop [--force]"} {
		if !strings.Contains(stopUsage, want) {
			t.Errorf("stopUsage does not mention %q", want)
		}
	}
}

// TestAForcedStopDropsTheGraceAndForcesTheKeeper is the call-site pin on the --force plumbing: a
// plain stop gives the runtime the shared grace and hands force=false to the keeper's teardown; a
// forced one gives the runtime none and hands force=true.
func TestAForcedStopDropsTheGraceAndForcesTheKeeper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saved := finishStop
	t.Cleanup(func() { finishStop = saved })
	var forced []bool
	finishStop = func(_, _ io.Writer, _, _ string, _ int64, _ func(string, string), force bool) int {
		forced = append(forced, force)
		return 0
	}
	cname := runtime.FromWorkspace("/ws")

	var out, errb bytes.Buffer
	s := &stopRun{stats: []string{"true\n", ""}}
	if rc := stopJail(&out, &errb, "/ws", "podman", s.run, nil, false); rc != 0 {
		t.Fatalf("rc=%d, err=%s", rc, errb.String())
	}
	want := []string{"podman", "stop", "-t", strconv.Itoa(run.TeardownStopTimeoutSeconds), cname}
	if len(s.calls) != 2 || !slices.Equal(s.calls[1], want) {
		t.Errorf("a plain stop's argv = %v, want %v", s.calls, want)
	}

	out.Reset()
	errb.Reset()
	s = &stopRun{stats: []string{"true\n", ""}}
	if rc := stopJail(&out, &errb, "/ws", "podman", s.run, nil, true); rc != 0 {
		t.Fatalf("rc=%d, err=%s", rc, errb.String())
	}
	wantForced := []string{"podman", "stop", "-t", "0", cname}
	if len(s.calls) != 2 || !slices.Equal(s.calls[1], wantForced) {
		t.Errorf("a forced stop's argv = %v, want %v", s.calls, wantForced)
	}
	if !slices.Equal(forced, []bool{false, true}) {
		t.Errorf("the keeper's teardown was handed force=%v, want [false true]", forced)
	}
}

// TestAForcedStopReachesTheRuntimeFromTheFrontDoor drives runStop: `yolo stop --force` asks the
// runtime for a zero-grace stop.
func TestAForcedStopReachesTheRuntimeFromTheFrontDoor(t *testing.T) {
	cname, _ := stopWorkspace(t, `{"runtime": "podman"}`)
	s := &stopRun{stats: []string{"true\n", ""}}
	useStopExec(t, s.run)
	var rc int
	stdout, stderr := captureBoth(t, func() { rc = runStop([]string{"stop", "--force"}) })
	if rc != 0 {
		t.Fatalf("runStop = %d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	want := []string{"podman", "stop", "-t", "0", cname}
	if len(s.calls) != 2 || !slices.Equal(s.calls[1], want) {
		t.Errorf("the forced stop's argv = %v, want %v", s.calls, want)
	}
}

// TestStopRefusesAnUnknownFlag: a flag stop does not define is misuse — exit 2 and the pointer to
// help — never silently dropped with the ordinary status returned.
func TestStopRefusesAnUnknownFlag(t *testing.T) {
	var rc int
	_, stderr := captureBoth(t, func() { rc = runStop([]string{"stop", "--nope"}) })
	if rc != 2 {
		t.Fatalf("runStop with --nope = %d, want 2", rc)
	}
	for _, want := range []string{`unknown flag "--nope"`, "`yolo stop --help`"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal must say %q:\n%s", want, stderr)
		}
	}
}
