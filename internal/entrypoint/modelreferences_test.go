package entrypoint

// modelreferences_test.go pins the boot's half of MM-D45 (docs/design/model-lists-and-pickers.md):
// after the boot's own loop (ConfigurePackSurfaces) renders the SHIPPED pi pack, a subagent model
// pi's registry does not hold is named. Deleting the call in ConfigurePackSurfaces turns it red. pi's catalog is a
// fixture on disk; no agent runs.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/luahook"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// A profile's subagents.default that pi's catalog does not hold, on a provider pi runs on its own
// catalog, is written verbatim; the boot names it with the nearest catalog id.
func TestTheBootNamesASubagentModelPisRegistryDoesNotHold(t *testing.T) {
	packs := testPacksForAgent(t, "pi", "zai", "openrouter", "kilo", "llamacpp", "openai-auth")
	providers, err := packload.ComposeProviders(nil, packs)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := packload.ResolveProfiles(packs, map[string]packload.UserProfile{
		"or": {Provider: "openrouter", Subagents: luahook.Subagents{Default: "deepseek/deepseek-v4-flsh"}},
	}, providers)
	if err != nil {
		t.Fatal(err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(mustCompactJSON(t, packload.ProfilesWireTable(resolved)))
	r.e.NpmPrefix = t.TempDir()
	writePiCatalog(t, r.e.NpmPrefix, "deepseek/deepseek-v4-flash")
	r.render(t, `{"pi":"or"}`)
	want := `pi: ~/.pi/agent/settings.json /subagents/defaultModel names "openrouter/deepseek/deepseek-v4-flsh", ` +
		`which matches no model in pi's registry (nearest: openrouter/deepseek/deepseek-v4-flash)`
	if out := r.errw.String(); !strings.Contains(out, want) || !strings.Contains(out, "`yolo check` on the host") {
		t.Fatalf("the boot should say %q and point at `yolo check`:\n%s", want, out)
	}
}

// writePiCatalog is a fixture install of pi's package under prefix whose catalog names ids.
func writePiCatalog(t *testing.T, prefix string, ids ...string) {
	t.Helper()
	pkg := filepath.Join(prefix, "lib", "node_modules", "@earendil-works", "pi-coding-agent")
	data := filepath.Join(pkg, "node_modules", "@earendil-works", "pi-ai", "dist", "providers", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version": "1.0.4"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var models []map[string]string
	for _, id := range ids {
		models = append(models, map[string]string{"id": id})
	}
	b, _ := json.Marshal(map[string]any{"models": models})
	if err := os.WriteFile(filepath.Join(data, "openrouter.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}
