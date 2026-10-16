package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// PD-NF1 for ordinary preparation (docs/design/program-delivery.md §3.6): every test here launches
// through the public run front door, so the production PreparePrograms wiring is what runs.

type ordinaryNoFallback struct {
	f           *selectedNPMFixture
	req         run.ProgramCaptureRequest
	failInstall atomic.Bool
}

func newOrdinaryNoFallback(t *testing.T) *ordinaryNoFallback {
	t.Helper()
	x := &ordinaryNoFallback{f: newSelectedNPMFixture(t)}
	x.req = selectedNPMUser(t, x.f, true)
	stubPreparationWorkerStart(t)
	selectedNPMSharedWorker(t)
	inner := programCaptureChild
	programCaptureChild = func(ctx context.Context, workspace string, argv []string, w io.Writer) error {
		if len(argv) > 2 && argv[2] != "program-npm" && x.failInstall.Load() {
			x.f.installs.Add(1)
			return errors.New("synthetic install of the candidate failed")
		}
		return inner(ctx, workspace, argv, w)
	}
	t.Cleanup(func() { programCaptureChild = inner })
	return x
}

// launch runs `yolo run [args] -- probe` through the front door and returns what this launch's
// preparation delivered, what it printed and its error.
func (x *ordinaryNoFallback) launch(t *testing.T, args ...string) (map[string]entrypoint.ProgramCaptureDelivery, string, error) {
	t.Helper()
	old := launchRunPipeline
	t.Cleanup(func() { launchRunPipeline = old })
	var got map[string]entrypoint.ProgramCaptureDelivery
	var gotErr error
	var out bytes.Buffer
	called := false
	launchRunPipeline = func(o run.Options) int {
		if o.PreparePrograms == nil {
			t.Fatal("public run frontdoor omitted the selected producer callback")
		}
		called = true
		req := x.req
		req.Act, req.Fresh, req.Stderr = &run.ActInterrupt{}, o.Update, &out
		got, gotErr = o.PreparePrograms(req)
		return 0
	}
	if rc := runRun(append(append([]string{"run"}, args...), "--", "probe")); rc != 0 || !called {
		t.Fatalf("front door rc=%d called=%v", rc, called)
	}
	return got, out.String(), gotErr
}

// admit prepares 1.0.0 at a first launch and returns its compatibility.
func (x *ordinaryNoFallback) admit(t *testing.T) string {
	t.Helper()
	got, out, err := x.launch(t)
	if err != nil {
		t.Fatalf("first launch: %v\n%s", err, out)
	}
	if got["probe"].Compatibility == "" {
		t.Fatal("first launch admitted nothing")
	}
	return got["probe"].Compatibility
}

// failedCheck is the scheduled check's own act (runSharedPreparation's advanceNPMCapture, fresh)
// failing after it found 2.0.0, as the hourly timer leaves it for the next launch.
func (x *ordinaryNoFallback) failedCheck(t *testing.T) {
	t.Helper()
	x.f.version(t, "2.0.0")
	x.failInstall.Store(true)
	x.check(t)
	r, err := readNPMCaptureRecord(x.f.store, x.f.in)
	if err != nil || r.FailedRelease != "2.0.0" || r.Resolution == nil || r.Resolution.Release != "1.0.0" {
		t.Fatalf("failed check did not record the newer candidate over the admitted 1.0.0: %+v %v", r, err)
	}
}

func (x *ordinaryNoFallback) check(t *testing.T) {
	t.Helper()
	ctx := withOrdinaryCaptureRuntime(context.Background(), x.req.Runtime)
	boundary, err := registerNPMCaptureRequest(ctx, x.f.store, x.f.in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := advanceNPMCapture(ctx, x.f.store, x.f.in, x.req.Fingerprint, true, boundary, io.Discard, func(commit func() error) error { return commit() }); err == nil {
		t.Fatal("the synthetic check did not fail")
	}
}

func TestOrdinaryNewerFailureRefusesTheLaunchWithTheRecoveryBlock(t *testing.T) {
	x := newOrdinaryNoFallback(t)
	x.admit(t)
	x.failedCheck(t)
	resolves, installs := x.f.resolves.Load(), x.f.installs.Load()
	got, _, err := x.launch(t)
	var refusal *hostfloor.UpdateRefusal
	if !errors.As(err, &refusal) || got != nil {
		t.Fatalf("the launch ran the older generation past a newer one's failed preparation: %v %v", got, err)
	}
	if x.f.resolves.Load() != resolves+1 || x.f.installs.Load() != installs+1 {
		t.Fatalf("the launch did not retry the failed newer preparation in front of the program: %d/%d", x.f.resolves.Load()-resolves, x.f.installs.Load()-installs)
	}
	want := "ERROR: probe 1.0.0 → 2.0.0: preparing the newer version failed\n" +
		"  " + refusal.Err.Error() + "\n" +
		"\n" +
		"Repair:\n" +
		"  fix what the error names, then retry it:\n" +
		"    yolo\n" +
		"\n" +
		"Start anyway:\n" +
		"  YOLO_USE_INSTALLED=probe yolo    run the prepared 1.0.0\n"
	if text := refusal.Render(false); text != want {
		t.Fatalf("refusal block:\n%s\nwant:\n%s", text, want)
	}
	if !strings.Contains(refusal.Err.Error(), "synthetic install of the candidate failed") {
		t.Fatalf("the refusal does not carry the failure: %v", refusal.Err)
	}
}

func TestOrdinaryUseInstalledServesTheAdmittedGenerationOnlyWhenItNamesTheProgram(t *testing.T) {
	x := newOrdinaryNoFallback(t)
	first := x.admit(t)
	x.failedCheck(t)
	for _, value := range []string{"probe", "fixture", "probe-pkg", "1"} {
		t.Setenv(patchrecovery.UseInstalledEnv, value)
		resolves := x.f.resolves.Load()
		got, out, err := x.launch(t)
		if err != nil || got["probe"].Compatibility != first {
			t.Fatalf("%s=%s did not run the prepared 1.0.0: %v %v\n%s", patchrecovery.UseInstalledEnv, value, got, err, out)
		}
		if x.f.resolves.Load() != resolves {
			t.Fatalf("%s=%s still checked the registry", patchrecovery.UseInstalledEnv, value)
		}
		if want := "probe: preparing 2.0.0 failed ("; !strings.Contains(out, want) || !strings.Contains(out, patchrecovery.UseInstalledEnv+" is set, so yolo runs the prepared 1.0.0") {
			t.Fatalf("the hatch ran silently:\n%s", out)
		}
	}
	t.Setenv(patchrecovery.UseInstalledEnv, "other")
	var refusal *hostfloor.UpdateRefusal
	if _, _, err := x.launch(t); !errors.As(err, &refusal) {
		t.Fatalf("a hatch naming another program ran the older generation: %v", err)
	}
}

func TestOrdinaryUnreachableRegistryRunsTheAdmittedGenerationAndSaysSo(t *testing.T) {
	t.Run("the timer could not check", func(t *testing.T) {
		x := newOrdinaryNoFallback(t)
		first := x.admit(t)
		x.f.version(t, "2.0.0")
		writeFile(t, filepath.Join(x.f.root, "fail"), "")
		x.check(t)
		resolves := x.f.resolves.Load()
		got, out, err := x.launch(t)
		if err != nil || got["probe"].Compatibility != first {
			t.Fatalf("an unreachable registry stopped the launch: %v %v\n%s", got, err, out)
		}
		if x.f.resolves.Load() != resolves || !strings.Contains(out, "probe: could not check for a newer version (") || !strings.Contains(out, "; running the prepared 1.0.0") {
			t.Fatalf("could-not-check line missing or the launch rechecked:\n%s", out)
		}
	})
	t.Run("the launch's retry could not check", func(t *testing.T) {
		x := newOrdinaryNoFallback(t)
		first := x.admit(t)
		x.failedCheck(t)
		writeFile(t, filepath.Join(x.f.root, "fail"), "")
		resolves := x.f.resolves.Load()
		got, out, err := x.launch(t)
		if err != nil || got["probe"].Compatibility != first {
			t.Fatalf("an unreachable registry at the retry stopped the launch: %v %v\n%s", got, err, out)
		}
		if x.f.resolves.Load() != resolves+1 || !strings.Contains(out, "probe: could not check for a newer version (") {
			t.Fatalf("the retry did not run, or ran silently:\n%s", out)
		}
	})
}

func TestOrdinaryAgentUpdatesHoldRunsTheAdmittedGenerationUnchecked(t *testing.T) {
	x := newOrdinaryNoFallback(t)
	first := x.admit(t)
	x.failedCheck(t)
	configPath := paths.UserConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, strings.Replace(string(data), `"agent_updates":true`, `"agent_updates":false`, 1))
	resolves, installs := x.f.resolves.Load(), x.f.installs.Load()
	got, out, err := x.launch(t)
	if err != nil || got["probe"].Compatibility != first {
		t.Fatalf("a held program did not run its prepared generation: %v %v\n%s", got, err, out)
	}
	if x.f.resolves.Load() != resolves || x.f.installs.Load() != installs {
		t.Fatal("a held program was checked")
	}
}

func TestOrdinaryNothingAdmittedRefusesInTheRecoveryShape(t *testing.T) {
	x := newOrdinaryNoFallback(t)
	x.failInstall.Store(true)
	_, _, err := x.launch(t)
	var failed *ordinaryPreparationFailed
	if !errors.As(err, &failed) {
		t.Fatalf("a first preparation's failure is not the refusal: %T %v", err, err)
	}
	var refusal *hostfloor.UpdateRefusal
	if errors.As(err, &refusal) {
		t.Fatal("nothing is prepared, yet the refusal offers to run an installed version")
	}
	want := "ERROR: probe: preparing it failed\n" +
		"  " + failed.Err.Error() + "\n" +
		"\n" +
		"Repair:\n" +
		"  fix what the error names, then retry the launch:\n" +
		"    yolo\n"
	if text := failed.Render(false); text != want {
		t.Fatalf("refusal block:\n%s\nwant:\n%s", text, want)
	}
	if strings.Contains(failed.Render(false), "Start anyway") {
		t.Fatal("a refusal with nothing to run lists a hatch")
	}
}

func TestOrdinaryUpdateStaysStrictOverAnAdmittedGeneration(t *testing.T) {
	x := newOrdinaryNoFallback(t)
	x.admit(t)
	x.failedCheck(t)
	t.Setenv(patchrecovery.UseInstalledEnv, "probe")
	got, _, err := x.launch(t, "--update")
	var failed *ordinaryPreparationFailed
	if !errors.As(err, &failed) || got != nil {
		t.Fatalf("--update ran the older generation: %v %v", got, err)
	}
}
