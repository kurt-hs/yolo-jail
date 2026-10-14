package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func selectedNPMConsumer(t *testing.T, f *selectedNPMFixture, r npmCaptureRecord) (string, *entrypoint.Env) {
	t.Helper()
	restore := packload.OverrideSkewTolerance(false)
	t.Cleanup(restore)
	home := t.TempDir()
	root := t.TempDir()
	pack := filepath.Join(root, "fixture")
	if err := os.MkdirAll(pack, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"contributes":[{"kind":"program","bin":"probe","via":"npm","package":"probe-pkg"}]}`
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	inputs, err := r.Resolution.inputs()
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(map[string]entrypoint.ProgramCaptureDelivery{"probe": {Declared: f.in.Package, Compatibility: r.Identity, Platform: capture.Platform(), Runtime: inputs.Runtime, NodeFloor: f.in.NodeFloor}})
	e := entrypoint.NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_WORKSPACE": filepath.Join(home, "ws"), "YOLO_PACK_ROOT": root, entrypoint.CapturesDirEnv: f.store.Dir, entrypoint.ProgramCapturesEnv: string(wire)})
	if err := entrypoint.GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	return home, e
}

func runSelectedConsumer(t *testing.T, f *selectedNPMFixture, home string, e *entrypoint.Env, mode string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(e.LaunchDir(), "probe"))
	cmd.Env = append(os.Environ(), "HOME="+home, "NPM_CONFIG_PREFIX="+e.NpmPrefix, "PATH="+f.tools+":"+os.Getenv("PATH"), "YOLO_INSTALL_ONLY=0", "YOLO_PACK_UPDATE=0")
	if mode != "" {
		cmd.Env = append(cmd.Env, mode+"=1")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("consumer %s: %v\n%s", mode, err, out)
	}
	return string(out)
}

func TestSelectedNPMGeneratedReadinessConsumesExactArtifactsWithoutInstaller(t *testing.T) {
	f := newSelectedNPMFixture(t)
	record := f.advance(t, false)
	home, e := selectedNPMConsumer(t, f, record)
	if out := runSelectedConsumer(t, f, home, e, entrypoint.InstallOnlyEnv); strings.Contains(out, "1.0.0") {
		t.Fatal("readiness ran synthetic target")
	}
	if got := runSelectedConsumer(t, f, home, e, ""); strings.TrimSpace(got) != "1.0.0" {
		t.Fatalf("target %q", got)
	}
	if got := runSelectedConsumer(t, f, home, e, entrypoint.FreshRefreshEnv); strings.Contains(got, "1.0.0") {
		t.Fatal("refresh ran target")
	}
	counts, _ := os.ReadFile(filepath.Join(f.root, "counts"))
	if strings.Count(string(counts), "install\n") != 1 || strings.Count(string(counts), "resolve\n") != 1 {
		t.Fatalf("consumer contacted installer or registry: %s", counts)
	}
	// A newer admission has no authority to update a running launch's fixed vector.
	f.version(t, "2.0.0")
	next := f.advance(t, true)
	if got := runSelectedConsumer(t, f, home, e, ""); strings.TrimSpace(got) != "1.0.0" {
		t.Fatalf("running generation moved: %q", got)
	}
	otherHome, other := selectedNPMConsumer(t, f, next)
	if got := runSelectedConsumer(t, f, otherHome, other, ""); !strings.HasSuffix(strings.TrimSpace(got), "2.0.0") {
		t.Fatalf("next launch did not consume new admission: %q", got)
	}
}

func TestSelectedNPMEnabledMCPRefreshConsumesExactDecision(t *testing.T) {
	f := newSelectedNPMFixture(t)
	record := f.advance(t, false)
	_, e := selectedNPMConsumer(t, f, record)
	if err := entrypoint.RefreshServers(entrypoint.ServerRefreshRequest{Env: e, NpmSpecs: f.in.Package, Updates: true, Stderr: io.Discard, Run: func(_ctx context.Context, _w io.Writer, argv []string) error {
		if argv[0] != "yolo" || argv[2] != "capture-materialize" {
			t.Fatalf("MCP consumer ran mutable installer: %v", argv)
		}
		if rc := runInternal(argv[2:]); rc != 0 {
			return fmt.Errorf("materializer status %d", rc)
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedNPMEnabledPresetBootstrapConsumesActualMaterializerWithoutNPM(t *testing.T) {
	f := newSelectedNPMFixture(t)
	home := t.TempDir()
	e := entrypoint.NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_WORKSPACE": filepath.Join(home, "ws"), "YOLO_MCP_PRESETS": `["sequential-thinking"]`, entrypoint.CapturesDirEnv: f.store.Dir})
	vector := entrypoint.SelectedServerPreparations(e, nil, entrypoint.ProgramInstallOnly)
	if len(vector) != 1 {
		t.Fatalf("actual enabled preset vector: %+v", vector)
	}
	var ok bool
	f.in, ok = npmCaptureInputFor(vector[0].Pack, vector[0].Install)
	if !ok {
		t.Fatal("actual preset has no selected npm input")
	}
	record := f.advance(t, false)
	delivery, err := ordinaryCaptureDelivery(record)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(map[string]entrypoint.ProgramCaptureDelivery{f.in.Bin: delivery})
	if err != nil {
		t.Fatal(err)
	}
	e.Vars[entrypoint.ProgramCapturesEnv] = string(wire)
	for n := 0; n < 2; n++ {
		cmd := exec.Command("/bin/bash")
		cmd.Stdin = strings.NewReader(entrypoint.BootstrapScript(e))
		cmd.Env = append(os.Environ(), "HOME="+home, "GOPATH="+filepath.Join(home, "go"), "NPM_CONFIG_PREFIX="+filepath.Join(home, ".npm-global"), "NPM_CONFIG_CACHE="+filepath.Join(home, ".cache/npm"), "PATH="+f.tools+":/bin:/usr/bin")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("real generated preset bootstrap: %v\n%s", err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".npm-global/bin", f.in.Bin)); err != nil {
		t.Fatal("bootstrap omitted exact preset materialization", err)
	}
	counts, _ := os.ReadFile(filepath.Join(f.root, "counts"))
	if strings.Count(string(counts), "install\n") != 1 || strings.Count(string(counts), "resolve\n") != 1 {
		t.Fatalf("preset bootstrap ran mutable npm: %s", counts)
	}
}

func TestSelectedNPMWarmGeneratedConsumersRejectChangedRuntime(t *testing.T) {
	for _, mode := range []string{entrypoint.InstallOnlyEnv, entrypoint.FreshRefreshEnv, "execute"} {
		t.Run(mode, func(t *testing.T) {
			f := newSelectedNPMFixture(t)
			record := f.advance(t, false)
			home, e := selectedNPMConsumer(t, f, record)
			runSelectedConsumer(t, f, home, e, entrypoint.InstallOnlyEnv)
			markerBefore, _ := os.ReadFile(entrypoint.ProgramCaptureMarker(home, "probe"))
			writeFile(t, npmCaptureImagePath, "changed-consumer-runtime")
			cmd := exec.Command(filepath.Join(e.LaunchDir(), "probe"))
			cmd.Env = append(os.Environ(), "HOME="+home, "NPM_CONFIG_PREFIX="+e.NpmPrefix, "PATH="+f.tools+":"+os.Getenv("PATH"), "YOLO_INSTALL_ONLY=0", "YOLO_PACK_UPDATE=0")
			if mode != "execute" {
				cmd.Env = append(cmd.Env, mode+"=1")
			}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("matching marker hid changed runtime; consumer succeeded: %s", out)
			}
			if !strings.Contains(string(out), "fresh launch") {
				t.Fatalf("refusal lacks next step: %s", out)
			}
			markerAfter, _ := os.ReadFile(entrypoint.ProgramCaptureMarker(home, "probe"))
			if string(markerBefore) != string(markerAfter) {
				t.Fatal("validation mutated running generation")
			}
			counts, _ := os.ReadFile(filepath.Join(f.root, "counts"))
			if strings.Count(string(counts), "install\n") != 1 || strings.Count(string(counts), "resolve\n") != 1 {
				t.Fatalf("validation contacted registry/installer: %s", counts)
			}
		})
	}
}

func TestSelectedNPMWarmMCPRejectsChangedRuntime(t *testing.T) {
	f := newSelectedNPMFixture(t)
	record := f.advance(t, false)
	_, e := selectedNPMConsumer(t, f, record)
	refresh := func() error {
		return entrypoint.RefreshServers(entrypoint.ServerRefreshRequest{Env: e, NpmSpecs: f.in.Package, Updates: true, Stderr: io.Discard, Run: func(ctx context.Context, w io.Writer, argv []string) error {
			if argv[0] != "yolo" || argv[2] != "capture-materialize" {
				t.Fatalf("mutable MCP installer: %v", argv)
			}
			if rc := runInternal(argv[2:]); rc != 0 {
				return fmt.Errorf("materializer status %d", rc)
			}
			return nil
		}})
	}
	if err := refresh(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, npmCaptureImagePath, "changed-MCP-runtime")
	if err := refresh(); err == nil {
		t.Fatal("warm MCP skipped changed runtime validation")
	}
}
