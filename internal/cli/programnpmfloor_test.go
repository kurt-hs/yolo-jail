package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
)

func TestNPMCaptureNodeUnavailableRequiresSuccessfulVersionObservation(t *testing.T) {
	for _, tc := range []struct {
		name, list            string
		listErr, installError error
		cancel                bool
		unavailable           bool
	}{
		{name: "unavailable", list: "20.19.0\n24.21.0\n", unavailable: true},
		{name: "install transient with satisfying release", list: "99.1.0\n"},
		{name: "install cancelled", list: "24.21.0\n", installError: context.Canceled},
		{name: "install timeout", list: "24.21.0\n", installError: context.DeadlineExceeded},
		{name: "listing timeout", list: "24.21.0\n", listErr: context.DeadlineExceeded},
		{name: "network listing", listErr: errors.New("network down")},
		{name: "empty listing"},
		{name: "malformed listing", list: "24.21.0\nwarning: partial listing\n"},
		{name: "cancel", list: "24.21.0\n", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			installErr := errors.New("install failed HTTP404 (not authority)")
			if tc.installError != nil {
				installErr = tc.installError
			}
			calls := 0
			ops := npmCaptureNodeIO{validateTrust: func(string) error { return nil }, resolveNodeFloor: func(string) string { return "" }, runCommand: func(c context.Context, name string, args, env []string, out, errw io.Writer) error {
				calls++
				if args[0] == "install" {
					return installErr
				}
				if _, bounded := c.Deadline(); !bounded {
					t.Fatal("release observation has no context deadline")
				}
				if name != "mise" || strings.Join(args, " ") != "ls-remote node" {
					t.Fatalf("unexpected command %s %v", name, args)
				}
				if tc.cancel {
					cancel()
				}
				if _, err := io.WriteString(out, tc.list); err != nil {
					return err
				}
				return tc.listErr
			}}
			_, err := npmCaptureNodeWithIO(ctx, t.TempDir(), "99", "private-npmrc", io.Discard, ops)
			var unavailable *run.NodeFloorUnavailable
			isUnavailable := errors.As(err, &unavailable) && unavailable.Floor == "99"
			if isUnavailable != tc.unavailable {
				t.Fatalf("unavailable=%v want %v: %v (calls %d)", isUnavailable, tc.unavailable, err, calls)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel lost: %v", err)
			}
			if !tc.cancel && !tc.unavailable && !errors.Is(err, installErr) {
				t.Fatalf("install cause lost: %v", err)
			}
		})
	}
}

func TestProgramCaptureChildFloorRefusalUsesOwnedResultNotOutput(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		status                    int
		payload, mismatch, cancel bool
		typed                     bool
	}{
		{name: "owned refusal", status: 78, payload: true, typed: true},
		{name: "output only", status: 78},
		{name: "generic failure with payload", status: 1, payload: true},
		{name: "wrong declaration", status: 78, payload: true, mismatch: true},
		{name: "cancel wins", status: 78, payload: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCommand, oldProbe := forkBuildChildCommand, probeForkBuildContainer
			t.Cleanup(func() { forkBuildChildCommand = oldCommand; probeForkBuildContainer = oldProbe })
			probeForkBuildContainer = func(string, string, time.Duration) (bool, bool) { return false, true }
			workspace := t.TempDir()
			in := npmCaptureInput{Owner: "floor-fixture", Bin: "probe", Package: "probe-pkg", NodeFloor: "99"}
			unavailable := &run.NodeFloorUnavailable{Owner: in.Owner, Bin: in.Bin, Floor: in.NodeFloor, Available: "24.21.0 at /bin/node"}
			raw, _ := json.Marshal(in)
			writeFile(t, filepath.Join(workspace, "input.json"), string(raw))
			result := npmCaptureResolution{Input: in}
			if tc.payload {
				result.NodeFloorUnavailable = unavailable
			}
			if tc.mismatch {
				result.Input.Package = "different"
			}
			payload, _ := json.Marshal(result)
			config, _ := json.Marshal(programCaptureDiagnosticFixture{FD: 1, Chunks: [][]byte{[]byte(unavailable.Error() + "\n")}, Workspace: workspace, Resolution: payload, Status: tc.status})
			fixture := filepath.Join(t.TempDir(), "result-fixture.json")
			writeFile(t, fixture, string(config))
			self, _ := os.Executable()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) {
				for _, a := range argv {
					if value, ok := strings.CutPrefix(a, "--workspace="); ok {
						workspace = value
					}
				}
				config, _ = json.Marshal(programCaptureDiagnosticFixture{FD: 1, Chunks: [][]byte{[]byte("producer refusal\n")}, Workspace: workspace, Resolution: payload, Status: tc.status})
				writeFile(t, fixture, string(config))
				if tc.cancel {
					cancel()
				}
				return exec.Command(self, "program-capture-diagnostics-fixture", fixture, "-test.short"), nil
			}
			storeDir, evalErr := filepath.EvalSymlinks(t.TempDir())
			if evalErr != nil {
				t.Fatal(evalErr)
			}
			_, err := resolveSelectedNPMCapture(ctx, &capture.Store{Dir: storeDir}, in, io.Discard)
			var got *run.NodeFloorUnavailable
			if errors.As(err, &got) != tc.typed {
				t.Fatalf("typed=%v want %v: %v", got, tc.typed, err)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel lost: %v", err)
			}
			if tc.typed {
				if _, e := result.inputs(); e == nil || errors.As(e, &got) {
					t.Fatalf("refusal admitted as success: %v", e)
				}
			}
		})
	}
}

func TestOrdinaryDemandTransportsFloorRefusalWithoutAdmittingGood(t *testing.T) {
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprint(typed), func(t *testing.T) {
			f := newSelectedNPMFixture(t)
			f.in.NodeFloor = "99"
			req := selectedNPMUser(t, f, false)
			workers := selectedNPMSharedWorker(t)
			old := programCaptureChild
			t.Cleanup(func() { programCaptureChild = old })
			programCaptureChild = func(context.Context, string, []string, io.Writer) error {
				if typed {
					return &run.NodeFloorUnavailable{Owner: f.in.Owner, Bin: f.in.Bin, Floor: "99", Available: "24.21.0 at /bin/node"}
				}
				return errors.New("transient install failure")
			}
			_, err := waitOrdinaryPreparation(context.Background(), f.in, req.Fingerprint, req.Runtime, req.Platform, true, io.Discard, foregroundOrdinaryGuard(context.Background(), req, f.in))
			workers.Wait()
			var got *run.NodeFloorUnavailable
			if err == nil || errors.As(err, &got) != typed {
				t.Fatalf("typed=%v want %v: %v", got, typed, err)
			}
			record, e := readNPMCaptureRecord(f.store, f.in)
			if e != nil || record.Entry != "" || record.Completed != 0 || record.Resolution != nil || f.installs.Load() != 0 {
				t.Fatalf("failure published success: %+v %v", record, e)
			}
		})
	}
}

func TestNPMCaptureNoSatisfyingReleaseDoesNotTrustPartialText(t *testing.T) {
	for _, list := range []string{"", "24.0", "24.21.0\n...", "v24.21.0", "+24.21.0", "24.21.0\n99.0.0", strings.Repeat("1", 1<<20+1)} {
		if npmCaptureNoSatisfyingRelease(list, "99") {
			t.Fatal("unproven release list became unavailable")
		}
	}

}

// Node's list carries prereleases (27.0.0-alpha.0.0.0 from 2026-10); one such line must not
// turn every unsatisfiable floor into a retry. A prerelease is judged by its release numbers.
func TestNPMCaptureNoSatisfyingReleaseReadsPrereleaseLines(t *testing.T) {
	if !npmCaptureNoSatisfyingRelease("26.13.0\n27.0.0-alpha.0.0.0\n27.0.0-rc.1", "99") {
		t.Fatal("a prerelease line made an unsatisfiable floor unproven")
	}
	for _, list := range []string{"26.13.0\n99.0.0-alpha.1", "26.13.0\n27.0.0-", "26.13.0\n27.0.0-alpha beta", "26.13.0\n-alpha.1"} {
		if npmCaptureNoSatisfyingRelease(list, "99") {
			t.Fatalf("%q proved no release satisfies 99", list)
		}
	}
}

func TestNPMCaptureReleaseListBoundsProviderOutput(t *testing.T) {
	var b npmCaptureReleaseList
	if _, err := io.WriteString(&b, strings.Repeat("1", 1<<20+1)); err == nil || b.String() != "" {
		t.Fatal("oversized observation accepted")
	}
}
