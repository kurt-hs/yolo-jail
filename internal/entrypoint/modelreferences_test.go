package entrypoint

// modelreferences_test.go pins the boot's half of MM-D45 and MM-D46
// (docs/design/model-lists-and-pickers.md): after the boot's own loop (ConfigurePackSurfaces)
// renders the SHIPPED pi pack, a model the user picked in pi that the derived modelScope excludes
// is named at that boot with the scope and the fix, and a subagent model pi's registry does not
// hold is named too. Deleting the call in ConfigurePackSurfaces turns both red. pi's catalog is a
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

// A pair pi's picker saved on another provider survives the re-render (a user edit is kept), and
// the boot says the scope excludes it, naming the model, the scope and the profile; before the
// edit, yolo's own pair is inside the scope and nothing is said.
func TestTheBootNamesAPickedModelTheDerivedScopeExcludes(t *testing.T) {
	r := shippedScopeRender(t)
	r.render(t, `{"pi":"zai"}`)
	if strings.Contains(r.errw.String(), "is outside the scope") {
		t.Fatalf("yolo's own selection is inside the scope it derives:\n%s", r.errw.String())
	}
	settings := []string{".pi", "agent", "settings.json"}
	r.edit(t, settings, "defaultProvider", "openrouter")
	r.edit(t, settings, "defaultModel", "deepseek/deepseek-v4-flash")
	r.render(t, `{"pi":"zai"}`)
	if got := r.piSettings(t)["defaultProvider"]; got != "openrouter" {
		t.Fatalf("the picked provider must survive the render, the premise of the warning; got %v", got)
	}
	out := r.errw.String()
	for _, want := range []string{
		"pi: the model saved in ~/.pi/agent/settings.json, openrouter/deepseek/deepseek-v4-flash, is outside the scope",
		"/subagents/modelScope/allow: zai/*",
		`which profile "zai" derives each boot`,
		"a child agent that inherits the session's model is refused",
		"pick one the scope admits there, or select a profile whose provider serves openrouter/deepseek/deepseek-v4-flash",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the boot should say %q:\n%s", want, out)
		}
	}
	if fails := r.e.GenFailures(); len(fails) != 0 {
		t.Errorf("the warning must never refuse the boot: %v", fails)
	}
}

// A profile's subagents.default that pi's catalog does not hold, on a provider pi runs on its own
// catalog, is written verbatim; the boot names it with the nearest catalog id.
func TestTheBootNamesASubagentModelPisRegistryDoesNotHold(t *testing.T) {
	packs := testPacksForAgent(t, "pi", "zai", "openrouter", "kilo", "llamacpp", "codex-auth")
	providers, err := packload.ComposeProviders(nil, packs)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveShippedProfiles(packs, map[string]packload.UserProfile{
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

// The derive's own values resolve in the registry it renders: no shipped profile or set, the
// openai-codex [1m] variants and kilo's respelled ids included, draws a reference warning, with
// pi's catalog installed and without it.
func TestTheBootNamesNothingForTheShippedProfilesOwnValues(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		for _, profiles := range []string{`"codex"`, `"zai"`, `"openrouter"`, `"kilo"`, `"llamacpp"`,
			`"bedrock"`, `"bedrock-bridge"`, `"cerebras"`, `"meta"`,
			`["codex","zai"]`, `["zai","openrouter"]`, `["kilo","zai"]`} {
			r := shippedScopeRender(t)
			if catalog {
				r.e.NpmPrefix = t.TempDir()
				writePiCatalog(t, r.e.NpmPrefix, "deepseek/deepseek-v4-flash")
			}
			r.render(t, `{"pi":`+profiles+`}`)
			for _, never := range []string{"matches no model", "is outside the scope"} {
				if out := r.errw.String(); strings.Contains(out, never) {
					t.Errorf("profile %s (catalog %t): the boot must not say %q of yolo's own values:\n%s",
						profiles, catalog, never, out)
				}
			}
		}
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
