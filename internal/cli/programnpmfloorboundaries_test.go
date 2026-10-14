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
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"golang.org/x/sys/unix"
)

func floorBoundaryResolution(in npmCaptureInput) npmCaptureResolution {
	return npmCaptureResolution{Input: in, Runtime: npmCaptureRuntime{Image: "fixture", Node: "24.21.0", NodeDigest: "fixture", NPM: "11", Platform: "linux/amd64", ABI: "137"}, Lock: json.RawMessage(`{"lockfileVersion":3,"packages":{"node_modules/probe-pkg":{"version":"1.0.0","resolved":"https://registry.invalid/fixture.tgz","integrity":"sha512-fixture","bin":{"probe":"bin/probe.js"}}}}`), Release: "1.0.0", Source: "https://registry.invalid/fixture.tgz|sha512-fixture", BinPath: "bin/probe.js", Dependencies: []string{"node_modules/probe-pkg=1.0.0|https://registry.invalid/fixture.tgz|sha512-fixture"}}
}

func TestResolveSelectedNPMFloorErrorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                                                         string
		status                                                       int
		success, mutate, inputLink, resultLink, noWitness, rootAlias bool
		available                                                    string
	}{
		{name: "legitimate host root alias", status: 78, rootAlias: true},
		{name: "child0 full success plus error", success: true},
		{name: "child78 mixed success plus error", status: 78, success: true},
		{name: "child78 both inputs changed", status: 78, mutate: true},
		{name: "child78 input symlink", status: 78, inputLink: true},
		{name: "child78 result symlink", status: 78, resultLink: true},
		{name: "child78 no return witness", status: 78, noWitness: true},
		{name: "escaped availability", status: 78, available: "24.21.0 at /bin/node\x1b]52;c;UNSAFE\a"},
		{name: "newline availability", status: 78, available: "24.21.0 at /bin/node\nUNSAFE"},
		{name: "non-numeric availability", status: 78, available: "UNSAFE at /bin/node"},
		{name: "invalid source availability", status: 78, available: "24.21.0 at UNSAFE"},
		{name: "invalid path availability", status: 78, available: "24.21.0 at /tmp/UNSAFE/node"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := npmCaptureInput{Owner: "floor-fixture", Bin: "probe", Package: "probe-pkg", NodeFloor: "99"}
			in := original
			if tc.mutate {
				in = npmCaptureInput{Owner: "different", Bin: "other", Package: "other-pkg", NodeFloor: "100"}
			}
			available := tc.available
			if available == "" {
				available = "24.21.0 at /bin/node"
			}
			result := npmCaptureResolution{Input: in}
			if tc.success {
				result = floorBoundaryResolution(in)
				if _, err := result.inputs(); err != nil {
					t.Fatal(err)
				}
			}
			result.NodeFloorUnavailable = &run.NodeFloorUnavailable{Owner: in.Owner, Bin: in.Bin, Floor: in.NodeFloor, Available: available}
			payload, _ := json.Marshal(result)
			input, _ := json.Marshal(in)
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "input.json"), string(input))
			writeFile(t, filepath.Join(outside, "resolution.json"), string(payload))
			oldCmd, oldProbe := forkBuildChildCommand, probeForkBuildContainer
			t.Cleanup(func() { forkBuildChildCommand = oldCmd; probeForkBuildContainer = oldProbe })
			probeForkBuildContainer = func(string, string, time.Duration) (bool, bool) { return false, true }
			self, _ := os.Executable()
			fixture := filepath.Join(t.TempDir(), "fixture.json")
			forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) {
				workspace := ""
				for _, a := range argv {
					if v, ok := strings.CutPrefix(a, "--workspace="); ok {
						workspace = v
					}
				}
				f := programCaptureDiagnosticFixture{FD: 1, Chunks: [][]byte{[]byte("producer refusal\n")}, Workspace: workspace, Resolution: payload, Status: tc.status, NoWitness: tc.noWitness}
				if tc.mutate {
					f.Input = input
				}
				if tc.inputLink {
					f.InputLink = filepath.Join(outside, "input.json")
				}
				if tc.resultLink {
					f.ResolutionLink = filepath.Join(outside, "resolution.json")
				}
				b, _ := json.Marshal(f)
				writeFile(t, fixture, string(b))
				return exec.Command(self, "program-capture-diagnostics-fixture", fixture, "-test.short"), nil
			}
			storeDir, evalErr := filepath.EvalSymlinks(t.TempDir())
			if evalErr != nil {
				t.Fatal(evalErr)
			}
			if tc.rootAlias {
				alias := filepath.Join(t.TempDir(), "store")
				if err := os.Symlink(storeDir, alias); err != nil {
					t.Fatal(err)
				}
				storeDir = alias
			}
			_, err := resolveSelectedNPMCapture(context.Background(), &capture.Store{Dir: storeDir}, original, io.Discard)
			var unavailable *run.NodeFloorUnavailable
			if tc.rootAlias {
				if !errors.As(err, &unavailable) {
					t.Fatalf("legitimate host alias lost refusal: %v", err)
				}
			} else if err == nil || errors.As(err, &unavailable) {
				t.Fatalf("unsafe/mixed result classified refusal/success: %v", err)
			}
			if strings.Contains(err.Error(), "UNSAFE") {
				t.Fatalf("structured bytes disclosed: %v", err)
			}
		})
	}
}

func TestWaitOrdinaryRejectsMixedStructuredFloorResults(t *testing.T) {
	for _, timer := range []bool{false, true} {
		for _, name := range []string{"not failed", "program", "good", "built", "wrong input", "unsafe availability"} {
			t.Run(fmt.Sprintf("timer=%v/%s", timer, name), func(t *testing.T) {
				f := newSelectedNPMFixture(t)
				f.in.NodeFloor = "99"
				req := selectedNPMUser(t, f, true)
				result := preparationDemandResult{Failed: true, NodeFloorUnavailable: &run.NodeFloorUnavailable{Owner: f.in.Owner, Bin: f.in.Bin, Floor: "99", Available: "24.21.0 at /bin/node"}}
				switch name {
				case "not failed":
					result.Failed = false
				case "program":
					result.Program = &npmCaptureRecord{}
				case "good":
					result.Good = &packsrc.GoodBuild{}
				case "built":
					result.Built = true
				case "wrong input":
					result.NodeFloorUnavailable.Owner = "different"
				case "unsafe availability":
					result.NodeFloorUnavailable.Available = "UNSAFE at /bin/node"
				}
				old := sharedPreparationSpawn
				t.Cleanup(func() { sharedPreparationSpawn = old })
				sharedPreparationSpawn = func(ctx context.Context, d preparationDemand) error {
					dir := sharedDemandDir(d.Key, d.Runtime, d.Platform)
					return withDemandState(ctx, dir, true, func() error {
						live, err := livePreparationDemands(dir)
						if err != nil {
							return err
						}
						for _, r := range live {
							if err = writeDemandJSON(r.path+".result", result); err != nil {
								return err
							}
						}
						return nil
					})
				}
				var err error
				if timer {
					_, err = runOrdinaryScheduledPreparation(context.Background(), preparationRecord{Kind: preparationOrdinaryProgramKind, Key: ordinaryProgramKey(f.in), Runtime: req.Runtime, Platform: req.Platform, InputIdentity: ordinaryProgramIdentity(f.in, req.Fingerprint, req.Runtime, req.Platform)}, io.Discard)
				} else {
					_, err = waitOrdinaryPreparation(context.Background(), f.in, req.Fingerprint, req.Runtime, req.Platform, true, io.Discard, foregroundOrdinaryGuard(context.Background(), req, f.in))
				}
				var unavailable *run.NodeFloorUnavailable
				if err == nil || errors.As(err, &unavailable) {
					t.Fatalf("mixed result became refusal/success: %v", err)
				}
				if strings.Contains(err.Error(), "UNSAFE") {
					t.Fatalf("unsafe shared text disclosed: %v", err)
				}
			})
		}
	}
}

func TestPrepareOrdinaryFinalActInterruptOverridesCompletedTypedError(t *testing.T) {
	f := newSelectedNPMFixture(t)
	f.in.NodeFloor = "99"
	req := selectedNPMUser(t, f, false)
	old := programCaptureChild
	t.Cleanup(func() { programCaptureChild = old })
	typed := &run.NodeFloorUnavailable{Owner: f.in.Owner, Bin: f.in.Bin, Floor: "99", Available: "24.21.0 at /bin/node"}
	// The typed result is complete; a nested scope records an act interrupt without
	// cancelling the outer wait context. Its final boundary must still return cancel.
	programCaptureChild = func(ctx context.Context, _ string, _ []string, _ io.Writer) error {
		req.Act.Scope(func(inner context.Context) {
			if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
				t.Fatal(err)
			}
			select {
			case <-inner.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("interrupt not routed")
			}
		})
		if ctx.Err() != nil {
			t.Fatal("test cancelled the leaf rather than the final act")
		}
		return typed
	}
	// Workspace-only selection forces the real foreground wait/caller path.
	t.Setenv("YOLO_VERSION", "floor-boundary-fixture")
	_, err := prepareOrdinaryForLaunch(req, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("final act retained typed refusal: %v", err)
	}
}

func TestReadProgramFailureFileRejectsLinkedAncestorAndOversizedFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, noFollowDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	real := filepath.Join(root, "real")
	if err = os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err = os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "input.json"), "{}")
	if _, err = readProgramFailureFile(rootFD, "linked/input.json"); err == nil {
		t.Fatal("followed linked ancestor")
	}
	writeFile(t, filepath.Join(real, "input.json"), strings.Repeat("x", 16385))
	if _, err = readProgramFailureFile(rootFD, "real/input.json"); err == nil {
		t.Fatal("accepted oversized failure file")
	}
	if err = os.Remove(filepath.Join(real, "input.json")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(real, "input.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = readProgramFailureFile(rootFD, "real/input.json"); err == nil {
		t.Fatal("accepted non-regular file")
	}
}
