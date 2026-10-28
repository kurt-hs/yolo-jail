package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func graphicsFixture(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func writeGraphicsFixture(t *testing.T, root, path, contents string) {
	t.Helper()
	full := rootedPath(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirGraphicsFixture(t *testing.T, root, path string) {
	t.Helper()
	if err := os.MkdirAll(rootedPath(root, path), 0o755); err != nil {
		t.Fatal(err)
	}
}

func graphicsConfig(t *testing.T, text string) *jsonx.OrderedMap {
	t.Helper()
	value, err := jsonx.Decode([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.OrderedMap)
}

func graphicsReport(t *testing.T, cfg *jsonx.OrderedMap, root, profiles string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	r := newReporter(&out, false)
	o := &Options{Getenv: func(key string) string {
		if key == "YOLO_STORE_PROFILES" {
			return profiles
		}
		return ""
	}}
	o.reportJailGraphicsState(r, cfg, root, o.getenv)
	return out.String(), r.warned
}

func TestGraphicsRuntimeMetadataReportsSelectedTreesAndOptional32Bit(t *testing.T) {
	root := graphicsFixture(t)
	writeGraphicsFixture(t, root, graphicsRuntimeManifest, `{"drivers":[`+
		`{"name":"mesa","version":"26.2.5","arch":"64","lib":"/nix/store/mesa/lib","share":"/nix/store/mesa/share","preserved":false},`+
		`{"name":"mesa-32","version":"26.2.5","arch":"32","lib":"/nix/store/mesa32/lib","share":"/nix/store/mesa32/share","preserved":false}]}`)
	for _, p := range []string{
		graphics64Tree + "/lib", graphics64Tree + "/share",
		graphics32Tree + "/lib", graphics32Tree + "/share",
	} {
		mkdirGraphicsFixture(t, root, p)
	}
	out, warned := graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`), root, "")
	for _, want := range []string{"Selected driver mesa 26.2.5 exposed at /run/opengl-driver",
		"/nix/store/mesa/lib", "Selected driver mesa-32 26.2.5 exposed at /run/opengl-driver-32"} {
		if !strings.Contains(out, want) {
			t.Errorf("graphics report missing %q:\n%s", want, out)
		}
	}
	if warned != 0 {
		t.Errorf("complete 64-bit and supplied 32-bit trees produced %d warnings:\n%s", warned, out)
	}
}

func TestGraphicsRuntimeMetadataDistinguishesPreservedUserTree(t *testing.T) {
	root := graphicsFixture(t)
	writeGraphicsFixture(t, root, graphicsRuntimeManifest, `{"drivers":[{"name":"mesa","version":"26.2.5","arch":"64","lib":"/nix/store/mesa/lib","share":"/nix/store/mesa/share","preserved":true}]}`)
	mkdirGraphicsFixture(t, root, graphics64Tree+"/lib")
	mkdirGraphicsFixture(t, root, graphics64Tree+"/share")
	out, warned := graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`), root, "")
	if !strings.Contains(out, "Selected driver mesa 26.2.5; existing user-supplied tree preserved at /run/opengl-driver") ||
		!strings.Contains(out, "not asserted to belong to the selected driver") {
		t.Fatalf("preserved tree must be distinguished from selected metadata:\n%s", out)
	}
	if strings.Contains(out, "exposed at /run/opengl-driver (") {
		t.Fatalf("preserved tree was falsely credited to selected driver:\n%s", out)
	}
	if warned == 0 {
		t.Fatalf("preserving a user-supplied tree should be reported as a caveat:\n%s", out)
	}
	if strings.Contains(out, graphics32Tree) {
		t.Errorf("unsupplied 32-bit driver should not be reported as required:\n%s", out)
	}
}

func TestGraphicsBakedManifestNamesDriverWithoutClaimingExposure(t *testing.T) {
	root := graphicsFixture(t)
	writeGraphicsFixture(t, root, graphicsBakedManifest, `{"drivers":[{"name":"mesa","version":"26.2.5","arch":"64","lib":"/nix/store/mesa/lib","share":"/nix/store/mesa/share"}]}`)
	out, warned := graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`), root, "")
	if !strings.Contains(out, "Selected driver metadata: mesa 26.2.5 (64-bit)") ||
		!strings.Contains(out, "Runtime provenance is absent") {
		t.Fatalf("baked manifest fallback must identify driver without asserting exposure:\n%s", out)
	}
	if strings.Contains(out, "exposed at /run/opengl-driver (") {
		t.Fatalf("baked metadata was falsely treated as runtime provenance:\n%s", out)
	}
	if warned == 0 {
		t.Fatalf("missing runtime provenance should be actionable:\n%s", out)
	}
}

func TestGraphicsProfileManifestUsesFirstSelectedDriverPerArch(t *testing.T) {
	root := graphicsFixture(t)
	first := "/profiles/first/" + graphicsStoreManifest
	second := "/profiles/second/" + graphicsStoreManifest
	writeGraphicsFixture(t, root, first, `{"drivers":[{"name":"mesa","version":"26.2.5","arch":"64","lib":"/nix/store/one/lib","share":"/nix/store/one/share"}]}`)
	writeGraphicsFixture(t, root, second, `{"drivers":[{"name":"mesa-new","version":"27.0","arch":"64","lib":"/nix/store/two/lib","share":"/nix/store/two/share"},{"name":"mesa32","version":"26","arch":"32","lib":"/nix/store/three/lib","share":"/nix/store/three/share"}]}`)
	got, err := readProfileGraphicsManifests(root, "/profiles/first:/profiles/second", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "mesa" || got[1].Name != "mesa32" {
		t.Fatalf("profile order/first-per-arch selection = %#v", got)
	}
}

func TestGraphicsMalformedAndAbsentMetadataAreActionable(t *testing.T) {
	for _, tc := range []struct {
		name, manifest string
	}{
		{name: "absent"},
		{name: "malformed", manifest: `{"drivers":[{"name":"mesa","version":"26","arch":"64","lib":"relative/lib","share":"/nix/share"}]}`},
		{name: "unknown field", manifest: `{"drivers":[],"guess":"/nix/store"}`},
		{name: "missing preserved", manifest: `{"drivers":[{"name":"mesa","version":"26","arch":"64","lib":"/nix/lib","share":"/nix/share"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := graphicsFixture(t)
			if tc.manifest != "" {
				writeGraphicsFixture(t, root, graphicsRuntimeManifest, tc.manifest)
			}
			out, warned := graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`), root, "")
			if warned == 0 {
				t.Fatalf("%s metadata should warn with an action:\n%s", tc.name, out)
			}
			if !strings.Contains(strings.ToLower(out), "graphics") || !strings.Contains(out, "yolo check") {
				t.Fatalf("%s metadata warning lacks actionable diagnosis:\n%s", tc.name, out)
			}
		})
	}
}

func TestGraphicsProfileMetadataDoesNotFallBackToBakedImage(t *testing.T) {
	root := graphicsFixture(t)
	writeGraphicsFixture(t, root, graphicsBakedManifest, `{"drivers":[{"name":"stale-mesa","version":"25.0","arch":"64","lib":"/nix/store/stale/lib","share":"/nix/store/stale/share"}]}`)
	writeGraphicsFixture(t, root, "/profiles/current/"+graphicsStoreManifest, `{"drivers":[{"name":"mesa","version":"26","arch":"64","lib":"relative/lib","share":"/nix/store/current/share"}]}`)
	out, warned := graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`), root, "/profiles/current")
	if warned == 0 || !strings.Contains(out, "profile metadata is unavailable or malformed") {
		t.Fatalf("malformed active profile metadata should be actionable:\n%s", out)
	}
	if strings.Contains(out, "stale-mesa") {
		t.Fatalf("store-profile mode fell back to stale baked metadata:\n%s", out)
	}

	root = graphicsFixture(t)
	writeGraphicsFixture(t, root, graphicsBakedManifest, `{"drivers":[{"name":"stale-mesa","version":"25.0","arch":"64","lib":"/nix/store/stale/lib","share":"/nix/store/stale/share"}]}`)
	out, warned = graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`), root, "/profiles/absent")
	if warned == 0 || strings.Contains(out, "stale-mesa") {
		t.Fatalf("missing store-profile metadata must not consume baked metadata:\n%s", out)
	}
}

func TestGraphicsNoDriverAndDisabledConfig(t *testing.T) {
	root := graphicsFixture(t)
	writeGraphicsFixture(t, root, graphicsRuntimeManifest, `{"drivers":[]}`)
	out, warned := graphicsReport(t, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":[]}`), root, "")
	if warned == 0 || !strings.Contains(out, "No graphics driver was selected") || !strings.Contains(out, "mesa") {
		t.Fatalf("no-driver report must offer a package hint:\n%s", out)
	}

	var disabled bytes.Buffer
	r := newReporter(&disabled, false)
	o := &Options{Getenv: func(string) string { return "" }}
	o.sectionGPUGraphics(r, graphicsConfig(t, `{"gpu":{"enabled":false}}`))
	if disabled.Len() != 0 {
		t.Fatalf("disabled GPU emitted a graphics section:\n%s", disabled.String())
	}
}

func TestGraphicsHostReportDoesNotInspectHostTreeAsJailState(t *testing.T) {
	var out bytes.Buffer
	r := newReporter(&out, false)
	o := &Options{Getenv: func(string) string { return "" }}
	o.reportHostGraphicsSelection(r, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`))
	if strings.Contains(out.String(), "host /run") || strings.Contains(out.String(), "exposed at") {
		t.Fatalf("host report claimed jail path state:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "yolo check` inside the jail") {
		t.Fatalf("host report lacks the in-jail next step:\n%s", out.String())
	}
}

func TestCheckRunsGraphicsDiagnosticsForEnabledGPU(t *testing.T) {
	manifest := []byte(`{"drivers":[{"name":"mesa","version":"26.2.5","arch":"64","lib":"/nix/store/mesa/lib","share":"/nix/store/mesa/share","preserved":false}]}`)
	reads := 0
	got := runCheckOverConfigWith(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`, false, nil, func(o *Options) {
		o.readFile = func(path string) ([]byte, error) {
			if path == graphicsRuntimeManifest {
				reads++
				return manifest, nil
			}
			return nil, os.ErrNotExist
		}
		pathExists := o.PathExists
		o.PathExists = func(path string) bool {
			if strings.HasPrefix(path, graphics64Tree+"/") {
				return true
			}
			return pathExists(path)
		}
	})
	if reads == 0 {
		t.Fatalf("Check did not read runtime graphics metadata; the section caller is missing:\n%s", got)
	}
	if !strings.Contains(got, "Selected driver mesa 26.2.5 exposed at /run/opengl-driver") {
		t.Fatalf("Check did not report the selected resolved graphics driver:\n%s", got)
	}
}

func TestGraphicsHostAndMacOSDoNotReadJailGraphicsMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		isMacOS bool
	}{
		{name: "host"},
		{name: "macOS", isMacOS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			var out bytes.Buffer
			r := newReporter(&out, false)
			o := &Options{
				IsMacOS:  tc.isMacOS,
				Getenv:   func(string) string { return "" },
				readFile: func(string) ([]byte, error) { reads++; return nil, os.ErrNotExist },
			}
			o.sectionGPUGraphics(r, graphicsConfig(t, `{"gpu":{"enabled":true},"packages":["mesa"]}`))
			if reads != 0 {
				t.Fatalf("outside-jail section read runtime metadata %d times:\n%s", reads, out.String())
			}
			if tc.isMacOS && strings.Contains(out.String(), "/run/opengl-driver") {
				t.Fatalf("macOS report claimed Linux jail paths:\n%s", out.String())
			}
		})
	}
}

func TestDecodeGraphicsManifestRejectsInvalidArchAndNonAbsoluteOutput(t *testing.T) {
	for _, data := range []string{
		`{"drivers":[{"name":"mesa","version":"26","arch":"unknown","lib":"/nix/lib","share":"/nix/share"}]}`,
		`{"drivers":[{"name":"mesa","version":"26","arch":"64","lib":"relative","share":"/nix/share"}]}`,
	} {
		if _, err := decodeGraphicsManifest([]byte(data)); err == nil {
			t.Errorf("accepted invalid graphics metadata: %s", data)
		}
	}
}
