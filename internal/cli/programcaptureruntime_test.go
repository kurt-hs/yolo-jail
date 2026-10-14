package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestSelectedNPMRunPublicCustomizedRuntimeRetainsBoundedLegacyConsumer(t *testing.T) {
	packload.OverrideSkewTolerance(false)
	t.Cleanup(func() { packload.OverrideSkewTolerance(false) })
	for _, tc := range []struct {
		name, config string
		store        bool
	}{
		{"image-packages", `{"packages":["hello"]}`, false},
		{"store-loader", `{}`, true},
		{"configured-node", `{"mise_tools":{"node":"24"}}`, false},
		{"configured-loader", `{"env_sources":[{"NODE_OPTIONS":"--max-old-space-size=1024"}]}`, false},
		{"configured-ca-file", `{"env_sources":[{"SSL_CERT_FILE":"/fixture/trust.pem"}]}`, false},
		{"configured-proxy", `{"env_sources":[{"HTTPS_PROXY":"http://proxy.invalid:3128"}]}`, false},
		{"configured-lowercase-proxy", `{"env_sources":[{"https_proxy":"http://proxy.invalid:3128"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSelectedNPMFixture(t)
			req := selectedNPMUser(t, f, false)
			raw, err := jsonx.Decode([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			cfg := raw.(*jsonx.OrderedMap)
			req.UnsupportedRuntime = run.OrdinaryRuntimeUnsupported(cfg, tc.store)
			if req.UnsupportedRuntime == "" {
				t.Fatal("customized runtime falsely equated with base")
			}
			home, packroot := t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(packroot, "fixture", "pack.json"), `{"contributes":[{"kind":"program","bin":"probe","via":"npm","package":"probe-pkg"}]}`)
			e := entrypoint.NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_WORKSPACE": filepath.Join(home, "ws"), "YOLO_PACK_ROOT": packroot, "YOLO_AGENT_UPDATES": "false"})
			if err := entrypoint.GenerateAgentLaunchers(e); err != nil {
				t.Fatal(err)
			}
			previous := launchRunPipeline
			t.Cleanup(func() { launchRunPipeline = previous })
			var disclosure bytes.Buffer
			launchRunPipeline = func(o run.Options) int {
				if o.PreparePrograms == nil {
					t.Fatal("public frontdoor omitted preparation/fallback")
				}
				req.Act = &run.ActInterrupt{}
				req.Fresh = o.Update
				req.Stderr = &disclosure
				got, err := o.PreparePrograms(req)
				if err != nil || len(got) != 0 {
					t.Fatalf("customized runtime reused base artifacts: %+v %v", got, err)
				}
				mode := entrypoint.InstallOnlyEnv
				if o.Update {
					mode = entrypoint.FreshRefreshEnv
				}
				cmd := exec.Command(filepath.Join(e.LaunchDir(), "probe"))
				cmd.Env = append(os.Environ(), "HOME="+home, "NPM_CONFIG_PREFIX="+e.NpmPrefix, "PATH="+f.tools+":/bin:/usr/bin", mode+"=1")
				if out, err := cmd.CombinedOutput(); err != nil {
					if !o.Update {
						t.Fatalf("ordinary legacy install failed: %v\n%s", err, out)
					}
					return 1
				}
				return 0
			}
			if rc := runRun([]string{"run", "--", "probe"}); rc != 0 {
				t.Fatal(rc)
			}
			if rc := runRun([]string{"run", "--update", "--", "probe"}); rc != 0 {
				t.Fatal("customized-runtime manual check failed", rc)
			}
			writeFile(t, filepath.Join(f.root, "legacy-fail"), "1")
			if rc := runRun([]string{"run", "--update", "--", "probe"}); rc == 0 {
				t.Fatal("failed customized-runtime fresh check served retained target")
			}
			if f.resolves.Load() != 0 || f.installs.Load() != 0 {
				t.Fatal("unsupported customized runtime started sealed producer")
			}
			resolutions, _ := filepath.Glob(filepath.Join(f.store.Dir, "program-resolutions", "*.json"))
			if len(resolutions) != 0 {
				t.Fatal("incompatible base artifacts admitted", resolutions)
			}
			counts, _ := os.ReadFile(filepath.Join(f.root, "counts"))
			if !strings.Contains(string(counts), "legacy-install") || !strings.Contains(string(counts), "legacy-view") {
				t.Fatalf("authorized legacy consumer route was disabled: %s", counts)
			}
			if !strings.Contains(disclosure.String(), "baseline runtime") {
				t.Fatal("unsupported runtime did not name its supported reuse route")
			}
		})
	}
}

func TestSelectedNPMHostPublicCustomizedRuntimeDoesNotMaterializeBaseArchive(t *testing.T) {
	f := newSelectedNPMFixture(t)
	req := selectedNPMUser(t, f, false)
	selectedNPMSharedWorker(t)
	if _, err := prepareOrdinaryForLaunch(req, io.Discard); err != nil {
		t.Fatal(err)
	}
	record, err := readNPMCaptureRecord(f.store, f.in)
	if err != nil {
		t.Fatal(err)
	}
	selectedNPMHostFloor(t, f, record.Resolution.Runtime.Node)
	data, err := os.ReadFile(paths.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, paths.UserConfigPath(), strings.TrimSuffix(string(data), "}")+`,"packages":["hello"]}`)
	got := captureHostExec(t)
	var out bytes.Buffer
	if rc := hostMain([]string{"--", "probe"}, io.Discard, &out, false, nil); rc != 0 || got.target == "" {
		t.Fatalf("customized public HOST fallback disabled: %d %+v\n%s", rc, got, out.String())
	}
	floor := newHostFloor(io.Discard, floorPrograms(req.Packs))
	st := floor.Status(floorPrograms(req.Packs)[0])
	if st.Record == nil || st.Record.Capture != "" {
		t.Fatalf("HOST reused incompatible base archive: %+v", st.Record)
	}
	if !strings.Contains(out.String(), "sealed artifact reuse is unsupported") {
		t.Fatal("HOST customized runtime fallback was not disclosed")
	}
	counts, _ := os.ReadFile(filepath.Join(f.root, "counts"))
	if !strings.Contains(string(counts), "host-install") {
		t.Fatal("HOST legacy installer was not invoked")
	}
	writeFile(t, filepath.Join(f.root, "host-fail"), "1")
	got = captureHostExec(t)
	out.Reset()
	if rc := hostMain([]string{"--update", "--", "probe"}, io.Discard, &out, false, nil); rc == 0 || got.target != "" {
		t.Fatalf("failed customized HOST fresh fallback executed retained bytes: %d %+v\n%s", rc, got, out.String())
	}
}

func TestSelectedNPMRunPublicUnsupportedDependencyGraphRetainsLegacyFreshChecks(t *testing.T) {
	packload.OverrideSkewTolerance(false)
	t.Cleanup(func() { packload.OverrideSkewTolerance(false) })
	f := newSelectedNPMFixture(t)
	req := selectedNPMUser(t, f, false)
	selectedNPMSharedWorker(t)
	writeFile(t, filepath.Join(f.root, "unsupported-lock"), "1")
	home, root, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(root, "fixture", "pack.json"), `{"contributes":[{"kind":"program","bin":"probe","via":"npm","package":"probe-pkg"}]}`)
	e := entrypoint.NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_WORKSPACE": workspace, "YOLO_PACK_ROOT": root, "YOLO_AGENT_UPDATES": "false"})
	if err := entrypoint.GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	previous := launchRunPipeline
	t.Cleanup(func() { launchRunPipeline = previous })
	launchRunPipeline = func(o run.Options) int {
		req.Act = &run.ActInterrupt{}
		req.Fresh = o.Update
		req.Stderr = io.Discard
		got, err := o.PreparePrograms(req)
		if err != nil || len(got) != 0 {
			t.Fatalf("unsupported graph required an artifact or reused one: %+v %v", got, err)
		}
		mode := entrypoint.InstallOnlyEnv
		if o.Update {
			mode = entrypoint.FreshRefreshEnv
		}
		cmd := exec.Command(filepath.Join(e.LaunchDir(), "probe"))
		cmd.Env = append(os.Environ(), "HOME="+home, "NPM_CONFIG_PREFIX="+e.NpmPrefix, "PATH="+f.tools+":/bin:/usr/bin", mode+"=1")
		if _, err := cmd.CombinedOutput(); err != nil {
			return 1
		}
		return 0
	}
	if rc := runRun([]string{"run", "--", "probe"}); rc != 0 {
		t.Fatal("existing legacy installation route was disabled", rc)
	}
	if rc := runRun([]string{"run", "--update", "--", "probe"}); rc != 0 {
		t.Fatal("legacy qualifying fresh check failed", rc)
	}
	writeFile(t, filepath.Join(f.root, "legacy-fail"), "1")
	if rc := runRun([]string{"run", "--update", "--", "probe"}); rc == 0 {
		t.Fatal("failed legacy fresh check executed retained target")
	}
	if f.installs.Load() != 0 {
		t.Fatal("unsupported dependency graph entered artifact installation")
	}
	receipts, err := os.ReadFile(filepath.Join(workspace, ".yolo", "receipts.jsonl"))
	if err != nil || !strings.Contains(string(receipts), `"bin":"probe"`) {
		t.Fatalf("generated launcher did not append its actual installation receipt: %s %v", receipts, err)
	}
	record, err := readNPMCaptureRecord(f.store, f.in)
	if err != nil || record.Entry != "" || record.Completed != 0 {
		t.Fatalf("unsupported graph admitted: %+v %v", record, err)
	}
}
