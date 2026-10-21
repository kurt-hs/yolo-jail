package entrypoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func hostImportWire(t *testing.T, rendered bool, exclusions map[string][]string) string {
	t.Helper()
	path := "/ctx/host-claude/settings.json"
	w := HostLayerWire{
		HostLayerReport:      packload.HostLayerReport{Delivery: packload.HostLayersSupported, Delivered: []string{path}},
		HostImportExclusions: exclusions,
	}
	if rendered {
		w.Rendered = []string{path}
	}
	wire, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func bootClaudeWithHostImport(t *testing.T, e *Env) map[string]any {
	t.Helper()
	ConfigurePackSurfaces(e, testPacksForAgent(t, "claude"))
	if failures := e.GenFailures(); len(failures) != 0 {
		t.Fatalf("claude boot failed: %v", failures)
	}
	return decodeJSONFile(t, filepath.Join(e.ClaudeDir(), "settings.json"))
}

func TestConfigurePackSurfacesFiltersOnlyDeliveredHostBytes(t *testing.T) {
	for _, editCapturedValue := range []bool{false, true} {
		name := "unchanged last-render value drops"
		if editCapturedValue {
			name = "genuine captured edit survives"
		}
		t.Run(name, func(t *testing.T) {
			e, ctx := newClaudePrismEnv(t, nil)
			const source = `{"hooks":[{"command":"host-hook"}],"theme":"host-theme","permissions":{"allow":["host-permission"]}}`
			hostPath := filepath.Join(ctx, "settings.json")
			if err := os.WriteFile(hostPath, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			bootClaudeWithHostImport(t, e)

			settingsPath := filepath.Join(e.ClaudeDir(), "settings.json")
			if editCapturedValue {
				got := decodeJSONFile(t, settingsPath)
				got["hooks"] = []any{map[string]any{"command": "captured-edit"}}
				edited, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settingsPath, edited, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			e.Vars[packload.HostLayerEnvVar] = hostImportWire(t, false,
				map[string][]string{"claude/settings": {"/hooks", "/permissions"}})
			got := bootClaudeWithHostImport(t, e)
			if got["theme"] != "host-theme" {
				t.Errorf("unexcluded host sibling theme = %v, want host-theme", got["theme"])
			}
			if editCapturedValue {
				hooks, _ := got["hooks"].([]any)
				if len(hooks) != 1 || hooks[0].(map[string]any)["command"] != "captured-edit" {
					t.Errorf("captured hooks = %#v, want captured edit", got["hooks"])
				}
			} else if _, present := got["hooks"]; present {
				t.Errorf("unchanged previous host hooks survived exclusion: %#v", got["hooks"])
			}
			permissions, ok := got["permissions"].(map[string]any)
			if !ok || permissions["defaultMode"] != "acceptEdits" {
				t.Errorf("pack-managed permissions were suppressed with the host member: %#v", got["permissions"])
			}
			actual, err := os.ReadFile(hostPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(actual) != source {
				t.Errorf("host source changed: %s", actual)
			}
		})
	}
}

func TestConfigurePackSurfacesFailsClosedOnHostImportCodecError(t *testing.T) {
	e, ctx := newClaudePrismEnv(t, nil)
	const source = `not-json`
	hostPath := filepath.Join(ctx, "settings.json")
	if err := os.WriteFile(hostPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	e.Vars[packload.HostLayerEnvVar] = hostImportWire(t, false,
		map[string][]string{"claude/settings": {"/hooks"}})
	ConfigurePackSurfaces(e, testPacksForAgent(t, "claude"))
	failures := e.GenFailures()
	if len(failures) == 0 {
		t.Fatal("malformed delivered host bytes succeeded despite an active exclusion")
	}
	actual, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != source {
		t.Errorf("filter changed source bytes on failure: %q", actual)
	}
}

func TestConfigurePackSurfacesDoesNotFilterRenderedBaseline(t *testing.T) {
	e, ctx := newClaudePrismEnv(t, nil)
	if err := os.WriteFile(filepath.Join(ctx, "settings.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.Vars[packload.HostLayerEnvVar] = hostImportWire(t, true,
		map[string][]string{"claude/settings": {"/hooks"}})
	bootClaudeWithHostImport(t, e)
}
