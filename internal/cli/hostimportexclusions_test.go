package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func pinHostImportPolicy(t *testing.T, surface manifest.Surface, pointers []string, rendered bool) {
	t.Helper()
	w := entrypoint.HostLayerWire{
		HostLayerReport: packload.HostLayerReport{
			Delivery:  packload.HostLayersSupported,
			Delivered: []string{surface.HostSource},
		},
		HostImportExclusions: map[string][]string{surface.Agent + "/" + surface.Name: pointers},
	}
	if rendered {
		w.Rendered = []string{surface.HostSource}
	}
	wire, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(packload.HostLayerEnvVar, wire)
}

func TestRenderUsesStagedHostImportPolicy(t *testing.T) {
	withHomeAndCwd(t)
	tgt := withStagedHostLayer(t, "pi", "settings",
		`{"x_host_import_exclusion":"remove","x_host_import_sibling":"keep"}`, usersOwnBytes)
	s, _ := surfaceManifest().Lookup("pi", "settings")
	pinHostImportPolicy(t, s, []string{"/x_host_import_exclusion"}, false)

	var out, errw bytes.Buffer
	if rc := configRender(tgt, []string{"pi/settings"}, &out, &errw, false); rc != 0 {
		t.Fatalf("configRender rc=%d, stderr=%s", rc, errw.String())
	}
	if strings.Contains(out.String(), "x_host_import_exclusion\"") || !strings.Contains(out.String(), "x_host_import_sibling") {
		t.Fatalf("preview did not apply only the staged policy:\n%s", out.String())
	}
}

func TestRenderFailsClosedOnActiveHostImportCodecError(t *testing.T) {
	withHomeAndCwd(t)
	tgt := withStagedHostLayer(t, "pi", "settings", `not-json`, usersOwnBytes)
	s, _ := surfaceManifest().Lookup("pi", "settings")
	pinHostImportPolicy(t, s, []string{"/anything"}, false)

	var out, errw bytes.Buffer
	if rc := configRender(tgt, []string{"pi/settings"}, &out, &errw, false); rc == 0 {
		t.Fatalf("malformed active host bytes rendered successfully:\n%s", out.String())
	}
	if !strings.Contains(errw.String(), "host import exclusions") {
		t.Fatalf("preview error does not name the failed filter: %s", errw.String())
	}
}

func TestRenderFailsClosedWhenDeliveredHostImportSourceCannotBeRead(t *testing.T) {
	withHomeAndCwd(t)
	tgt := withStagedHostLayer(t, "pi", "settings", `{"x_host_import_exclusion":"remove"}`, usersOwnBytes)
	s, _ := surfaceManifest().Lookup("pi", "settings")
	staged, disposition := entrypoint.StagedHostLayer(s)
	if disposition != packload.HostLayerDelivered {
		t.Fatalf("staged host disposition = %v, want delivered", disposition)
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}
	pinHostImportPolicy(t, s, []string{"/x_host_import_exclusion"}, false)

	var out, errw bytes.Buffer
	if rc := configRender(tgt, []string{"pi/settings"}, &out, &errw, false); rc == 0 {
		t.Fatalf("preview silently omitted a missing delivered host layer with active exclusions:\n%s", out.String())
	}
	if !strings.Contains(errw.String(), "host import exclusions") {
		t.Fatalf("preview error does not identify the active host import filter: %s", errw.String())
	}
}

func TestRenderKeepsMissingDeliveredHostSourceQuietWithoutImportExclusions(t *testing.T) {
	withHomeAndCwd(t)
	tgt := withStagedHostLayer(t, "pi", "settings", `{"x_host_import_exclusion":"remove"}`, usersOwnBytes)
	s, _ := surfaceManifest().Lookup("pi", "settings")
	staged, disposition := entrypoint.StagedHostLayer(s)
	if disposition != packload.HostLayerDelivered {
		t.Fatalf("staged host disposition = %v, want delivered", disposition)
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}

	var out, errw bytes.Buffer
	if rc := configRender(tgt, []string{"pi/settings"}, &out, &errw, false); rc != 0 {
		t.Fatalf("quietly absent host layer without exclusions returned rc=%d: %s", rc, errw.String())
	}
	if strings.Contains(errw.String(), "host import exclusions") {
		t.Fatalf("absence without exclusions was not quiet: %s", errw.String())
	}
}

func TestRenderFailsClosedWhenDeliveredHostImportSourceIsDirectory(t *testing.T) {
	withHomeAndCwd(t)
	tgt := withStagedHostLayer(t, "pi", "settings", `{"x_host_import_exclusion":"remove"}`, usersOwnBytes)
	s, _ := surfaceManifest().Lookup("pi", "settings")
	staged, disposition := entrypoint.StagedHostLayer(s)
	if disposition != packload.HostLayerDelivered {
		t.Fatalf("staged host disposition = %v, want delivered", disposition)
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	pinHostImportPolicy(t, s, []string{"/x_host_import_exclusion"}, false)

	var out, errw bytes.Buffer
	if rc := configRender(tgt, []string{"pi/settings"}, &out, &errw, false); rc == 0 {
		t.Fatalf("preview silently omitted an unreadable delivered host layer with active exclusions:\n%s", out.String())
	}
	if !strings.Contains(errw.String(), "host import exclusions") {
		t.Fatalf("preview error does not identify the active host import filter: %s", errw.String())
	}
}

func TestConfigResetRefusesMissingDeliveredHostImportSourceBeforeMutatingSidecars(t *testing.T) {
	home := withScratchHome(t)
	tgt, sidecars := withLocalSidecarDir(t)
	writeSidecar(t, sidecars, "pi", "settings", `{"captured":"mine"}`, `{"last":"render"}`)
	listCapture := tgt.listCapturePath("pi", "settings")
	writeFile(t, listCapture, `{"/packages":{"add":["npm:mine"],"remove":[]}}`)
	destination := filepath.Join(home, ".pi", "agent", "settings.json")
	writeFile(t, destination, `{"captured":"mine"}`)

	s, ok := surfaceManifest().Lookup("pi", "settings")
	if !ok {
		t.Fatal("missing pi/settings")
	}
	ctx := t.TempDir()
	t.Setenv("YOLO_CTX_ROOT", ctx)
	staged, _ := entrypoint.StagedHostLayer(s)
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, staged, `{"x_host_import_exclusion":"remove"}`)
	pinHostImportPolicy(t, s, []string{"/x_host_import_exclusion"}, false)
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		tgt.overlayPath("pi", "settings"),
		tgt.lastRenderPath("pi", "settings"),
		listCapture,
		destination,
	}
	type fileState struct {
		bytes []byte
		mode  os.FileMode
		mtime int64
	}
	before := make(map[string]fileState, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read fixture %s: %v", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat fixture %s: %v", path, err)
		}
		before[path] = fileState{bytes: data, mode: info.Mode(), mtime: info.ModTime().UnixNano()}
	}

	var out, errw bytes.Buffer
	if rc := configReset(tgt, []string{"pi/settings"}, &out, &errw, false); rc == 0 {
		t.Fatalf("reset accepted a missing delivered host layer with active exclusions:\n%s", out.String())
	}
	if !strings.Contains(errw.String(), "host import exclusions") {
		t.Fatalf("reset error does not identify the active host import filter: %s", errw.String())
	}
	for _, path := range paths {
		state := before[path]
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("reset removed prior state %s: %v", path, err)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat prior state %s: %v", path, err)
			continue
		}
		if !bytes.Equal(data, state.bytes) || info.Mode() != state.mode || info.ModTime().UnixNano() != state.mtime {
			t.Errorf("reset mutated prior state %s: bytes=%q mode=%v mtime=%d; want bytes=%q mode=%v mtime=%d",
				path, data, info.Mode(), info.ModTime().UnixNano(), state.bytes, state.mode, state.mtime)
		}
	}
}

func TestConfigResetRefusesHostImportCodecErrorBeforeMutatingSidecars(t *testing.T) {
	home := withScratchHome(t)
	tgt, sidecars := withLocalSidecarDir(t)
	writeSidecar(t, sidecars, "pi", "settings", `{"captured":"mine"}`, `{"last":"render"}`)
	destination := filepath.Join(home, ".pi", "agent", "settings.json")
	writeFile(t, destination, `{"captured":"mine"}`)
	before, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}

	s, ok := surfaceManifest().Lookup("pi", "settings")
	if !ok {
		t.Fatal("missing pi/settings")
	}
	ctx := t.TempDir()
	t.Setenv("YOLO_CTX_ROOT", ctx)
	staged, _ := entrypoint.StagedHostLayer(s)
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, staged, `not-json`)
	pinHostImportPolicy(t, s, []string{"/anything"}, false)

	var out, errw bytes.Buffer
	if rc := configReset(tgt, []string{"pi/settings"}, &out, &errw, false); rc == 0 {
		t.Fatalf("reset accepted malformed filtered host bytes:\n%s", out.String())
	}
	if !strings.Contains(errw.String(), "host import exclusions") {
		t.Fatalf("reset error does not name the failed filter: %s", errw.String())
	}
	for _, path := range []string{tgt.overlayPath("pi", "settings"), tgt.lastRenderPath("pi", "settings")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("reset removed sidecar before refusal: %s: %v", path, err)
		}
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("reset changed destination before refusal: before=%s after=%s", before, after)
	}
}
