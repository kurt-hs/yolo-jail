package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

func selectedNPMHostFloor(t *testing.T, f *selectedNPMFixture, v string) {
	t.Helper()
	original := newHostFloor
	newHostFloor = func(out io.Writer, p []hostfloor.Program) *hostfloor.Floor {
		floor := productionHostFloor(out, p)
		floor.Node.Shipped = strings.TrimPrefix(v, "v")
		floor.Outranked = nil
		return floor
	}
	t.Cleanup(func() { newHostFloor = original })
	dir := filepath.Join(paths.HostFloorDir(), "node", v)
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.node, filepath.Join(bin, "node")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".yolo-floor-complete"), "")
	script := `#!/bin/bash
set -eu
ROOT=` + shquote.Quote(f.root) + `
if [ "$1" = --version ]; then echo 10.0.0; exit 0; fi
printf 'host-%s\n' "$1" >> "$ROOT/counts"
[ ! -f "$ROOT/host-fail" ] || exit 39
version=$(cat "$ROOT/version")
if [ "$1" = view ]; then echo "$version"; exit 0; fi
[ "$1" = install ]
prefix=$NPM_CONFIG_PREFIX
mkdir -p "$prefix/bin" "$prefix/lib/node_modules/probe-pkg/bin"
printf '{"version":"%s"}\n' "$version" > "$prefix/lib/node_modules/probe-pkg/package.json"
printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$version" > "$prefix/lib/node_modules/probe-pkg/bin/probe"
chmod 755 "$prefix/lib/node_modules/probe-pkg/bin/probe"
ln -s ../lib/node_modules/probe-pkg/bin/probe "$prefix/bin/probe"
`
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedNPMHostPublicCompatibleReuseFreshAndMismatchFallback(t *testing.T) {
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
	floorCheck := newHostFloor(io.Discard, floorPrograms(req.Packs))
	if !floorCheck.Include("fixture") {
		t.Fatalf("pre-public floor policy unexpectedly refuses fixture; config=%s", config.HostFloorWire())
	}
	got := captureHostExec(t)
	var out bytes.Buffer
	if rc := hostMain([]string{"--", "probe"}, io.Discard, &out, false, nil); rc != 0 || got.target == "" {
		t.Fatalf("public host compatible: %d %+v\n%s", rc, got, out.String())
	}
	data, _ := os.ReadFile(filepath.Join(f.root, "counts"))
	if strings.Contains(string(data), "host-install") || strings.Contains(string(data), "host-view") {
		t.Fatalf("compatible host contacted installer/registry: %s", data)
	}
	floor := newHostFloor(io.Discard, floorPrograms(req.Packs))
	status := floor.Status(floorPrograms(req.Packs)[0])
	if status.Record == nil || status.Record.Capture != record.Entry || status.Record.Recipe != record.Identity {
		t.Fatalf("host did not use exact admitted identity: %+v", status)
	}
	if _, err := os.Stat(status.Record.NpmPackageDir("probe-pkg")); err != nil {
		t.Fatal("captured host package metadata missing", err)
	}
	out.Reset()
	got = captureHostExec(t)
	if rc := hostMain([]string{"--update", "--", "probe"}, io.Discard, &out, false, nil); rc != 0 || got.target == "" {
		t.Fatalf("public host fresh: %d %+v\n%s", rc, got, out.String())
	}
	if f.resolves.Load() != 2 || f.installs.Load() != 1 {
		t.Fatal("compatible fresh host did not check exactly once without rebuilding")
	}
	// Change an independently observed host fact. Captured bytes must no longer be
	// selected, and the already-authorized bounded floor updater remains the fallback.
	writeFile(t, npmCaptureImagePath, "different-actual-host-runtime")
	f.version(t, "2.0.0")
	out.Reset()
	got = captureHostExec(t)
	if rc := hostMain([]string{"--update", "--", "probe"}, io.Discard, &out, false, nil); rc != 0 || got.target == "" {
		t.Fatalf("public host mismatch fallback: %d %+v\n%s", rc, got, out.String())
	}
	data, _ = os.ReadFile(filepath.Join(f.root, "counts"))
	if !strings.Contains(string(data), "host-view") || !strings.Contains(string(data), "host-install") {
		t.Fatalf("mismatch lost legacy route: %s", data)
	}
	writeFile(t, filepath.Join(f.root, "host-fail"), "1")
	out.Reset()
	got = captureHostExec(t)
	if rc := hostMain([]string{"--update", "--", "probe"}, io.Discard, &out, false, nil); rc == 0 || got.target != "" {
		t.Fatalf("failed mismatching host fresh check executed retained bytes: %d %+v\n%s", rc, got, out.String())
	}
	// Already-cancelled compatible floor observation may not silently fall back.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	floor = newHostFloor(io.Discard, floorPrograms(req.Packs))
	floor.ManualUpdate = true
	if _, _, err := floor.Ensure(ctx, floorPrograms(req.Packs)[0]); err == nil {
		t.Fatal("cancelled public-floor fresh operation succeeded")
	}
}

func TestSelectedNPMHostPublicInvalidatedCaptureReinstallsSameVersion(t *testing.T) {
	for _, tc := range []struct {
		name                string
		fixed, fail, recent bool
	}{{"updates-off", false, false, false}, {"recent-check", false, false, true}, {"fixed", true, false, false}, {"failed-replacement", false, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSelectedNPMFixture(t)
			req := selectedNPMUser(t, f, tc.recent)
			if tc.fixed {
				pack := req.Packs[0].Root
				writeFile(t, filepath.Join(pack, "pack.json"), `{"name":"fixture","contributes":[{"kind":"program","bin":"probe","via":"npm","package":"probe-pkg@1.0.0"}]}`)
				req.Packs = selectConfiguredHostPacks().packs
				cfg, err := config.UserScopeConfig(true, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Env = ordinarySelectionEnvironment(cfg, req.Packs)
				vector, err := selectedOrdinaryInputs(req.Env, req.Packs, entrypoint.ProgramInstallOnly)
				if err != nil || len(vector) != 1 {
					t.Fatalf("fixed selection: %v %v", vector, err)
				}
				f.in = vector[0]
			}
			selectedNPMSharedWorker(t)
			if _, err := prepareOrdinaryForLaunch(req, io.Discard); err != nil {
				t.Fatal(err)
			}
			record, err := readNPMCaptureRecord(f.store, f.in)
			if err != nil {
				t.Fatal(err)
			}
			selectedNPMHostFloor(t, f, record.Resolution.Runtime.Node)
			got := captureHostExec(t)
			var out bytes.Buffer
			if rc := hostMain([]string{"--", "probe"}, io.Discard, &out, false, nil); rc != 0 || got.target == "" {
				t.Fatalf("initial capture: %d target=%q %s", rc, got.target, out.String())
			}
			writeFile(t, npmCaptureImagePath, "changed-observed-host-runtime")
			if tc.fail {
				writeFile(t, filepath.Join(f.root, "host-fail"), "1")
			}
			got = captureHostExec(t)
			out.Reset()
			rc := hostMain([]string{"--", "probe"}, io.Discard, &out, false, nil)
			if tc.fail {
				if rc == 0 || got.target != "" {
					t.Fatalf("invalid capture executed after failed replacement: %d target=%q %s", rc, got.target, out.String())
				}
			} else {
				if rc != 0 || got.target == "" {
					t.Fatalf("native same-version replacement: %d target=%q %s", rc, got.target, out.String())
				}
				floor := newHostFloor(io.Discard, floorPrograms(req.Packs))
				st := floor.Status(floorPrograms(req.Packs)[0])
				if st.Record == nil || st.Record.Capture != "" || st.Record.Version != "1.0.0" {
					t.Fatalf("invalid captured record retained: %+v", st.Record)
				}
			}
			counts, _ := os.ReadFile(filepath.Join(f.root, "counts"))
			if !strings.Contains(string(counts), "host-install") {
				t.Fatalf("invalidation never attempted native replacement: %s", counts)
			}
		})
	}
}
