package cli

// hostnofallback_test.go pins PD-NF1 (docs/design/program-delivery.md#PD-NF1) at `yolo host --`'s
// readiness act, its call site: a declared program whose newer version fails to install stops the
// launch with the floor's refusal block, YOLO_ALLOW_MISSING_PROGRAMS does not waive it (the installed
// copy is still on the floor, so "start without it" would not), and YOLO_USE_INSTALLED runs it.
// Delete the act's call in hostLaunch, or its refusal of a hostfloor.UpdateRefusal, and the first
// cell fails.

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// failedUpdateHost is twoProgramFixture after a first launch installed othercli 1.0.0, with 2.0.0
// published and failing to install, and every floor's update interval run out.
func failedUpdateHost(t *testing.T) *floortest.Dist {
	t.Helper()
	dist, _ := twoProgramFixture(t, "")
	captureHostExec(t)
	var errw bytes.Buffer
	if rc := hostExec(nil, []string{"sometool"}, io.Discard, &errw, nil); rc != 0 || !floorHolds("othercli") {
		t.Fatalf("setup: the first launch: rc=%d\n%s", rc, errw.String())
	}
	dist.Publish("othercli-pkg", "2.0.0", "bin=othercli", "fail")
	orig := newHostFloor
	newHostFloor = func(out io.Writer, progs []hostfloor.Program) *hostfloor.Floor {
		f := orig(out, progs)
		f.UpdateInterval = time.Nanosecond
		return f
	}
	t.Cleanup(func() { newHostFloor = orig })
	return dist
}

func TestAHostLaunchRefusesWhenANewerVersionOfADeclaredProgramFailsToInstall(t *testing.T) {
	failedUpdateHost(t)
	// The missing-program hatch is set too: it does not waive a failed update.
	t.Setenv(paths.AllowMissingProgramsEnv, "1")
	got := captureHostExec(t)
	var errw bytes.Buffer
	rc := hostExec(nil, []string{"sometool", "--x"}, io.Discard, &errw, nil)
	if rc != 1 || got.execed {
		t.Fatalf("rc=%d execed=%v: the launch ran past a failed update\n%s", rc, got.execed, errw.String())
	}
	for _, want := range []string{
		"ERROR: othercli 1.0.0 → 2.0.0: the update failed\n",
		"Repair:\n  fix what the error names, then retry it:\n    yolo host --update -- othercli\n",
		"Start anyway:\n  YOLO_USE_INSTALLED=othercli yolo host -- sometool    run the installed 1.0.0\n",
		"yolo host: refusing to launch: a newer version of a program could not be installed\n",
	} {
		if !strings.Contains(errw.String(), want) {
			t.Errorf("the refusal lacks %q:\n%s", want, errw.String())
		}
	}
	for _, unwanted := range []string{"running the installed", "starts WITHOUT"} {
		if strings.Contains(errw.String(), unwanted) {
			t.Errorf("the refusal says %q:\n%s", unwanted, errw.String())
		}
	}
}

func TestUseInstalledStartsAHostLaunchPastAFailedUpdate(t *testing.T) {
	for _, value := range []string{"1", "floorpack", "othercli", "othercli-pkg"} {
		t.Run(value, func(t *testing.T) {
			failedUpdateHost(t)
			t.Setenv(patchrecovery.UseInstalledEnv, value)
			got := captureHostExec(t)
			var errw bytes.Buffer
			if rc := hostExec(nil, []string{"sometool"}, io.Discard, &errw, nil); rc != 0 || !got.execed {
				t.Fatalf("%s=%s: rc=%d execed=%v\n%s", patchrecovery.UseInstalledEnv, value, rc, got.execed, errw.String())
			}
			if !strings.Contains(errw.String(), "YOLO_USE_INSTALLED is set, so yolo runs the installed 1.0.0\n") {
				t.Errorf("the hatch's line was not said:\n%s", errw.String())
			}
		})
	}
	t.Run("another name", func(t *testing.T) {
		failedUpdateHost(t)
		t.Setenv(patchrecovery.UseInstalledEnv, "floorcli")
		got := captureHostExec(t)
		var errw bytes.Buffer
		if rc := hostExec(nil, []string{"sometool"}, io.Discard, &errw, nil); rc != 1 || got.execed {
			t.Fatalf("a hatch naming another program: rc=%d execed=%v, want the refusal\n%s", rc, got.execed, errw.String())
		}
	})
}
