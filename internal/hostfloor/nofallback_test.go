package hostfloor

// nofallback_test.go pins PD-NF1 at the floor (nofallback.go): a failed update past a newer version
// found refuses with one block, the user's YOLO_USE_INSTALLED runs the installed version, and an
// upstream that cannot be reached is a check that could not run, never a refusal. Its call site,
// `yolo host --`, is pinned in internal/cli's hostnofallback_test.go.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
)

// No floor a test builds reaches the network to probe an upstream: every probe answers "reachable"
// unless the test hands its floor a ProbeUpstream of its own.
func init() {
	ProbeUpstreamDefault = func(context.Context, string) error { return nil }
}

func unreachable(context.Context, string) error { return errors.New("dial tcp: connection refused") }

// failedUpdateWorld is a floor holding ocpack's opencode 1.0.0 (package opencode-ai, so pack, bin
// and package are three names), with 2.0.0 published and failing to install, two hours on.
func failedUpdateWorld(t *testing.T) (*world, Program) {
	t.Helper()
	w := newWorld(t)
	clk := &clock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	w.floor.Now = clk.now
	w.publish("opencode-ai", "1.0.0", "bin=opencode")
	p := npmProgram("ocpack", "opencode", "opencode-ai")
	if _, _, err := w.floor.Ensure(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	w.publish("opencode-ai", "2.0.0", "bin=opencode", "fail")
	clk.t = clk.t.Add(2 * time.Hour)
	return w, p
}

// The refusal's exact shape: the ERROR line naming the program and both versions, the facts, the
// repair, and the one hatch that runs the installed version.
func TestAFailedUpdateRefusalNamesTheVersionsTheRepairAndTheHatch(t *testing.T) {
	w, p := failedUpdateWorld(t)
	before := len(w.out.String())
	_, _, err := w.floor.Ensure(context.Background(), p)
	if err == nil {
		t.Fatalf("a failed update did not refuse:\n%s", w.out)
	}
	block := w.out.String()[before:]
	block = block[strings.Index(block, "ERROR:"):]
	want := "ERROR: opencode 1.0.0 → 2.0.0: the update failed\n" +
		"  npm install opencode-ai@latest: npm exited 1; its last lines:\n" +
		"        npm ERR! 404 opencode-ai is not in this registry\n" +
		"\n" +
		"Repair:\n" +
		"  fix what the error names, then retry it:\n" +
		"    yolo host --update -- opencode\n" +
		"\n" +
		"Start anyway:\n" +
		"  YOLO_USE_INSTALLED=opencode yolo host -- opencode    run the installed 1.0.0\n"
	if block != want {
		t.Errorf("the refusal block is\n%s\nwant\n%s", block, want)
	}
	if !strings.Contains(err.Error(), "YOLO_USE_INSTALLED=opencode runs it") {
		t.Errorf("the error's one-line form names no hatch: %v", err)
	}
}

// The hatch, read from the process environment: "1", or a name equal to the program's pack, bin or
// npm package, runs the installed version with one line saying so; any other name still refuses.
func TestUseInstalledRunsTheInstalledVersionOnlyForWhatItNames(t *testing.T) {
	for _, c := range []struct {
		value string
		runs  bool
	}{{"1", true}, {"ocpack", true}, {"opencode", true}, {"opencode-ai", true}, {"other,ocpack", true},
		{"other", false}, {"ocpack/opencode-ai", false}} {
		t.Run(c.value, func(t *testing.T) {
			w, p := failedUpdateWorld(t)
			t.Setenv(patchrecovery.UseInstalledEnv, c.value)
			st, outcome, err := w.floor.Ensure(context.Background(), p)
			if !c.runs {
				var refusal *UpdateRefusal
				if !errors.As(err, &refusal) {
					t.Fatalf("%s=%s: %s %v, want the refusal", patchrecovery.UseInstalledEnv, c.value, outcome, err)
				}
				return
			}
			if err != nil || outcome != Kept || st.Record.Version != "1.0.0" {
				t.Fatalf("%s=%s: %s %+v %v, want 1.0.0 kept", patchrecovery.UseInstalledEnv, c.value, outcome, st.Record, err)
			}
			if !strings.Contains(w.out.String(), "opencode: the update failed (") ||
				!strings.Contains(w.out.String(), "YOLO_USE_INSTALLED is set, so yolo runs the installed 1.0.0\n") {
				t.Errorf("the hatch's line was not said:\n%s", w.out)
			}
			if strings.Contains(w.out.String(), "ERROR:") {
				t.Errorf("the hatch ran the installed version and still printed the refusal:\n%s", w.out)
			}
		})
	}
}

// Offline is not a failure: a poll that cannot reach the registry runs the installed version and
// says it could not check.
func TestAnUnreachableRegistryRunsTheInstalledVersionAndSaysItCouldNotCheck(t *testing.T) {
	w, p := failedUpdateWorld(t)
	must(t, os.Remove(filepath.Join(w.dist.Registry, "opencode-ai.latest"))) // `npm view` now fails
	st, outcome, err := w.floor.Ensure(context.Background(), p)
	if err != nil || outcome != Current || st.Record.Version != "1.0.0" {
		t.Fatalf("an unreachable registry: %s %+v %v, want 1.0.0 current", outcome, st.Record, err)
	}
	if !strings.Contains(w.out.String(), "opencode: could not check for a newer version (") ||
		strings.Contains(w.out.String(), "ERROR:") {
		t.Errorf("want the could-not-check line and no refusal:\n%s", w.out)
	}
}

// PD-NF6: a moved declaration is a known target, so its failed reinstall refuses even with the
// upstream unreachable, and only the hatch runs the installed version.
func TestAFailedReinstallOfAMovedPinRefusesOfflineToo(t *testing.T) {
	w := newWorld(t)
	w.publish("oh-my-pi", "9.9.9", "bin=omp")
	p := npmProgram("oh-omp", "omp", "oh-my-pi@0.15.3")
	if _, _, err := w.floor.Ensure(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	w.publish("oh-my-pi", "9.9.9", "bin=omp", "fail")
	w.floor.ProbeUpstream = unreachable
	p.Install.Package = "oh-my-pi@0.16.0"
	st, _, err := w.floor.Ensure(context.Background(), p)
	var refusal *UpdateRefusal
	if !errors.As(err, &refusal) || refusal.Installed != "0.15.3" || refusal.Newer != "oh-my-pi@0.16.0" {
		t.Fatalf("a moved pin whose reinstall failed: %v (%+v), want the refusal", err, refusal)
	}
	if !strings.Contains(w.out.String(), "ERROR: omp 0.15.3 → oh-my-pi@0.16.0: the reinstall the pack now asks for") {
		t.Errorf("the refusal does not name the move:\n%s", w.out)
	}
	if st.Record == nil || st.Record.Version != "0.15.3" {
		t.Errorf("a refused reinstall lost the installed record: %+v", st.Record)
	}
	w.floor.UseInstalled = func(q Program) bool { return q.Bin() == "omp" }
	if st, outcome, err := w.floor.Ensure(context.Background(), p); err != nil || outcome != Kept ||
		st.Record.Version != "0.15.3" {
		t.Errorf("under the hatch: %s %+v %v, want 0.15.3 kept", outcome, st.Record, err)
	}
}

// PD-NF3 for a capture: a recapture that fails while the installer's upstream answers is a failed
// update, refused, and retried by the next launch.
func TestARecaptureFailingWithItsUpstreamReachableRefuses(t *testing.T) {
	e := newEvergreenWorld(t)
	e.capturing = func(string) error { return errors.New("the installer exited 1") }
	e.floor.ProbeUpstream = func(context.Context, string) error { return nil }
	e.clk.t = e.clk.t.Add(25 * time.Hour)
	st, outcome, err := e.ensure()
	var refusal *UpdateRefusal
	if !errors.As(err, &refusal) || outcome == Kept {
		t.Fatalf("a failed recapture with its upstream reachable: %s %v, want the refusal\n%s", outcome, err, e.out)
	}
	if st.Record == nil || st.Record.Version != "2.1.267" {
		t.Errorf("the refusal moved the record: %+v", st.Record)
	}
	if !strings.Contains(e.out.String(), "ERROR: claude 2.1.267: capturing a newer release failed\n") {
		t.Errorf("the refusal was not said:\n%s", e.out)
	}
	e.clk.t = e.clk.t.Add(10 * time.Minute)
	if _, _, _ = e.ensure(); e.captures != 2 {
		t.Errorf("the next launch did not retry the refused capture (%d captures)", e.captures)
	}
}

// PD-NF5 for a capture: another `yolo capture` of the program holds its lock, so this launch checked
// nothing. The installed version runs with a line saying so, never the refusal however reachable the
// upstream, and the stamp is put back, so the launch after it looks again.
func TestABusyCaptureLockIsNotAFailedUpdate(t *testing.T) {
	e := newEvergreenWorld(t)
	e.capturing = func(string) error { return fmt.Errorf("`yolo capture claude` exited 75: %w", ErrCaptureBusy) }
	probes := 0
	e.floor.ProbeUpstream = func(context.Context, string) error { probes++; return nil }
	e.clk.t = e.clk.t.Add(25 * time.Hour)
	st, outcome, err := e.ensure()
	if err != nil || outcome == Updated {
		t.Fatalf("a busy capture lock: %s %v, want the installed version kept\n%s", outcome, err, e.out)
	}
	if st.Record == nil || st.Record.Version != "2.1.267" {
		t.Errorf("a busy lock moved the record: %+v", st.Record)
	}
	if !strings.Contains(e.out.String(), "claude: another `yolo capture claude` is in progress; running the installed 2.1.267") {
		t.Errorf("the busy line was not said:\n%s", e.out)
	}
	if probes != 0 {
		t.Errorf("a busy lock is no failure to judge, yet the upstream was probed %d times", probes)
	}
	e.clk.t = e.clk.t.Add(10 * time.Minute)
	if _, _, _ = e.ensure(); e.captures != 2 {
		t.Errorf("the next launch did not look again (%d captures)", e.captures)
	}
}

// PD-NF3 for the pre-launch refresh: the probe follows a FAILED refresh. One whose upstream cannot
// be reached could not check: said, not refused, and stamped, so an offline hour asks once.
func TestAFailedPrelaunchRefreshOfflineCouldNotCheck(t *testing.T) {
	w := newRefreshWorld(t, "exit 3")
	var probed []string
	w.f.ProbeUpstream = func(_ context.Context, url string) error {
		probed = append(probed, url)
		return errors.New("dial tcp: connection refused")
	}
	got := w.f.PrelaunchRefresh(refreshProgram(), w.stub, []string{"PATH=/usr/bin:/bin",
		"npm_config_registry=https://registry.example.invalid/"})
	if got.Outcome != RefreshUnchecked || got.Refused || got.Err != nil || w.runs(t) != 1 {
		t.Fatalf("a failed refresh with an unreachable registry: %+v, %d runs, want it run once and not refused\n%s",
			got, w.runs(t), w.out)
	}
	if len(probed) != 1 || probed[0] != "https://registry.example.invalid/" {
		t.Errorf("the probe asked %q, want the refresh environment's registry", probed)
	}
	if !strings.Contains(w.out.String(), "yolo host: pi: the pre-launch refresh (update --extensions) failed "+
		"(it exited with status 3), and could not check for newer add-ons: https://registry.example.invalid/ is unreachable") {
		t.Errorf("the could-not-check line was not said:\n%s", w.out)
	}
	if w.refresh(refreshProgram()); len(probed) != 1 || w.runs(t) != 1 {
		t.Errorf("an offline launch inside the interval asked again (%d probes, %d runs)", len(probed), w.runs(t))
	}
}

// PD-NF3's "after": a refresh that succeeds costs no network call of yolo's own.
func TestASucceedingPrelaunchRefreshIsNeverProbed(t *testing.T) {
	w := newRefreshWorld(t, "")
	w.f.ProbeUpstream = func(context.Context, string) error {
		t.Errorf("a refresh that succeeded was probed")
		return nil
	}
	if got := w.refresh(refreshProgram()); got.Outcome != RefreshRan || w.runs(t) != 1 {
		t.Fatalf("a succeeding refresh: %+v, %d runs\n%s", got, w.runs(t), w.out)
	}
}

// A FORK keeps its base's refresh (packdecl/fork.go): a patched pi, install kind "source", runs
// `pi update --extensions` too, and its failure is judged by the same registry probe. Offline it
// could not check and pi runs; with the registry answering it refuses.
func TestAForksFailedRefreshIsJudgedByTheRegistry(t *testing.T) {
	fork := func() Program {
		p := refreshProgram()
		p.Install.Kind, p.Install.Package = packdecl.InstallKindSource, ""
		return p
	}
	t.Run("offline", func(t *testing.T) {
		w := newRefreshWorld(t, "exit 3")
		w.f.ProbeUpstream = unreachable
		got := w.refresh(fork())
		if got.Outcome != RefreshUnchecked || got.Refused || got.Err != nil {
			t.Fatalf("a fork's failed refresh offline: %+v, want could-not-check\n%s", got, w.out)
		}
		if !strings.Contains(w.out.String(), "could not check for newer add-ons: "+defaultNpmRegistry+" is unreachable") {
			t.Errorf("the could-not-check line was not said:\n%s", w.out)
		}
	})
	t.Run("reachable", func(t *testing.T) {
		w := newRefreshWorld(t, "exit 3")
		probes := 0
		w.f.ProbeUpstream = func(context.Context, string) error { probes++; return nil }
		got := w.refresh(fork())
		var refusal *UpdateRefusal
		if !got.Refused || !errors.As(got.Err, &refusal) || probes != 1 {
			t.Fatalf("a fork's failed refresh with its registry answering: %+v (%d probes), want the refusal\n%s",
				got, probes, w.out)
		}
	})
}

// The hatch runs a failed refresh's program with what is installed.
func TestUseInstalledRunsPastAFailedRefresh(t *testing.T) {
	w := newRefreshWorld(t, "exit 3")
	t.Setenv(patchrecovery.UseInstalledEnv, "pi")
	got := w.refresh(refreshProgram())
	if got.Outcome != RefreshFailed || got.Refused {
		t.Fatalf("a failed refresh under the hatch: %+v, want it not refused\n%s", got, w.out)
	}
	if !strings.Contains(w.out.String(), "yolo host: pi: the pre-launch refresh (update --extensions) failed "+
		"(it exited with status 3); YOLO_USE_INSTALLED is set, so yolo runs the installed add-ons\n") {
		t.Errorf("the hatch's line was not said:\n%s", w.out)
	}
}
