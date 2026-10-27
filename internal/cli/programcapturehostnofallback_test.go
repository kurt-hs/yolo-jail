package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// PD-NF1 for the HOST floor's machine-prepared npm generation (docs/design/program-delivery.md
// §3.6): `yolo host -- probe` over a generation whose newer version the scheduled check found and
// failed to prepare. Every cell runs through hostMain, so the production floor wiring
// (wireFloorOrdinaryCapture) is what decides.

// hostOverFailedCheck admits probe 1.0.0 at a jail launch, serves it on the host floor at a first
// `yolo host -- probe`, then has the scheduled check find 2.0.0 and fail to prepare it.
func hostOverFailedCheck(t *testing.T) *ordinaryNoFallback {
	t.Helper()
	baselineNodeRuntime(t)
	x := newOrdinaryNoFallback(t)
	x.admit(t)
	record, err := readNPMCaptureRecord(x.f.store, x.f.in)
	if err != nil {
		t.Fatal(err)
	}
	selectedNPMHostFloor(t, x.f, record.Resolution.Runtime.Node)
	got := captureHostExec(t)
	var out bytes.Buffer
	if rc := hostMain([]string{"--", "probe"}, io.Discard, &out, false, nil); rc != 0 || got.target == "" {
		t.Fatalf("setup: the first host launch: rc=%d\n%s", rc, out.String())
	}
	x.failedCheck(t)
	return x
}

func (x *ordinaryNoFallback) hostLaunch(t *testing.T) (int, *execCapture, string) {
	t.Helper()
	got := captureHostExec(t)
	var out bytes.Buffer
	rc := hostMain([]string{"--", "probe"}, io.Discard, &out, false, nil)
	return rc, got, out.String()
}

func TestHostFloorRefusesThePreparedGenerationPastAFailedNewerPreparation(t *testing.T) {
	x := hostOverFailedCheck(t)
	resolves, installs := x.f.resolves.Load(), x.f.installs.Load()
	rc, got, out := x.hostLaunch(t)
	if rc == 0 || got.execed {
		t.Fatalf("yolo host ran the prepared 1.0.0 past 2.0.0's failed preparation: rc=%d\n%s", rc, out)
	}
	if x.f.resolves.Load() != resolves+1 || x.f.installs.Load() != installs+1 {
		t.Fatalf("the launch did not retry 2.0.0 in front of the program: %d/%d\n%s",
			x.f.resolves.Load()-resolves, x.f.installs.Load()-installs, out)
	}
	for _, want := range []string{
		"ERROR: probe 1.0.0 → 2.0.0: preparing the newer version failed\n",
		"synthetic install of the candidate failed",
		"Repair:\n  fix what the error names, then retry it:\n    yolo host --update -- probe\n",
		"Start anyway:\n  YOLO_USE_INSTALLED=probe yolo host -- probe    run the prepared 1.0.0\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, out)
		}
	}
}

func TestHostFloorUseInstalledServesThePreparedGenerationAndSaysSo(t *testing.T) {
	x := hostOverFailedCheck(t)
	t.Setenv(patchrecovery.UseInstalledEnv, "probe")
	resolves := x.f.resolves.Load()
	rc, got, out := x.hostLaunch(t)
	if rc != 0 || !got.execed {
		t.Fatalf("the hatch did not run the prepared 1.0.0: rc=%d\n%s", rc, out)
	}
	if x.f.resolves.Load() != resolves {
		t.Fatal("the hatch still checked the registry")
	}
	if !strings.Contains(out, "probe: preparing 2.0.0 failed (") || !strings.Contains(out, "YOLO_USE_INSTALLED is set, so yolo runs the prepared 1.0.0") {
		t.Fatalf("the hatch ran silently:\n%s", out)
	}
}

func TestHostFloorRetryThatCouldNotCheckServesThePreparedGeneration(t *testing.T) {
	x := hostOverFailedCheck(t)
	writeFile(t, filepath.Join(x.f.root, "fail"), "")
	resolves := x.f.resolves.Load()
	rc, got, out := x.hostLaunch(t)
	if rc != 0 || !got.execed {
		t.Fatalf("an unreachable registry at the retry stopped the launch: rc=%d\n%s", rc, out)
	}
	if x.f.resolves.Load() != resolves+1 || !strings.Contains(out, "probe: could not check for a newer version (") ||
		!strings.Contains(out, "; running the prepared 1.0.0") {
		t.Fatalf("the retry did not run, or ran silently:\n%s", out)
	}
}

func TestHostFloorAgentUpdatesHoldServesThePreparedGenerationUnchecked(t *testing.T) {
	x := hostOverFailedCheck(t)
	configPath := paths.UserConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, strings.Replace(string(data), `"agent_updates":true`, `"agent_updates":false`, 1))
	resolves, installs := x.f.resolves.Load(), x.f.installs.Load()
	rc, got, out := x.hostLaunch(t)
	if rc != 0 || !got.execed {
		t.Fatalf("a held program did not run its prepared generation: rc=%d\n%s", rc, out)
	}
	if x.f.resolves.Load() != resolves || x.f.installs.Load() != installs {
		t.Fatalf("a held program was checked\n%s", out)
	}
}
