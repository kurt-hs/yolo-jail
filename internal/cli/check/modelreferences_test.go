package check

// modelreferences_test.go pins MM-D45 (docs/design/model-lists-and-pickers.md): `yolo check`
// resolves each model reference the shipped pi pack declares — a profile's `subagents` models,
// and a pack's modelScope pattern — against pi's registry, and warns about one that matches
// nothing, naming where it was written and the nearest ids. Every cell drives sectionPacks, so
// deleting the call in packs.go turns each one red. pi's catalog is a fixture on disk; no agent
// program runs.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installPiCatalog writes a fixture install of pi's package under prefix, its catalog naming ids.
func installPiCatalog(t *testing.T, prefix string, ids ...string) {
	t.Helper()
	pkg := filepath.Join(prefix, "lib", "node_modules", "@earendil-works", "pi-coding-agent")
	data := filepath.Join(pkg, "node_modules", "@earendil-works", "pi-ai", "dist", "providers", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"),
		[]byte(`{"name": "@earendil-works/pi-coding-agent", "version": "1.0.4"}`), 0o644); err != nil {
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

// scopePack is a local pack folding a modelScope pattern onto pi's settings.
func scopePack(t *testing.T, pattern string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name": "acme-scope", "contributes": [{"kind": "config-overlay", "surface": "pi/settings",
	  "config": {"managed": {"subagents": {"modelScope": {"allow": ["` + pattern + `"]}}}}}]}`
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runReferenceCheck runs sectionPacks over pi on an openrouter profile whose subagents declare
// subagents, in a jail whose npm prefix is prefix.
func runReferenceCheck(t *testing.T, prefix, subagents string, extraPacks ...string) (string, *reporter) {
	t.Helper()
	packs := `"pi", "openrouter"`
	for _, p := range extraPacks {
		packs += `, "file://` + p + `"`
	}
	packsFixture(t, `{"packs": [`+packs+`],
  "profiles": {"or": {"provider": "openrouter", "subagents": `+subagents+`}}}`)
	env := map[string]string{"YOLO_VERSION": "test", "NPM_CONFIG_PREFIX": prefix}
	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{Getenv: func(k string) string { return env[k] }}).sectionPacks(r, useProfiles("pi", "or"))
	return buf.String(), r
}

// A `subagents.default` naming a model pi's catalog does not hold, on a provider pi runs on its
// own catalog, is a WARNING naming the profile key, the file it was written in and the nearest
// id; a role pin the catalog holds is not.
func TestCheckWarnsAboutASubagentModelPisRegistryDoesNotHold(t *testing.T) {
	prefix := t.TempDir()
	installPiCatalog(t, prefix, "deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-pro")
	out, r := runReferenceCheck(t, prefix,
		`{"default": "deepseek/deepseek-v4-flsh", "roles": {"worker": "deepseek/deepseek-v4-pro"}}`)
	for _, want := range []string{
		`Model reference: profiles.or.subagents.default names the model "deepseek/deepseek-v4-flsh" ` +
			`(pi receives "openrouter/deepseek/deepseek-v4-flsh"), which matches no model in pi's registry`,
		"config.jsonc:",
		"Nearest: openrouter/deepseek/deepseek-v4-flash",
		"in your `profiles.or.subagents.default`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the check should say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "subagents.roles.worker names") {
		t.Errorf("a role pin the catalog holds must not be warned about:\n%s", out)
	}
	if r.failed != 0 {
		t.Errorf("an unresolved reference must WARN, never fail:\n%s", out)
	}
}

// With pi not installed the catalog cannot be read, so a provider pi runs itself is a SKIP that
// says why, never "matches nothing".
func TestCheckSaysItCouldNotAskWhenPisCatalogIsUnread(t *testing.T) {
	out, _ := runReferenceCheck(t, t.TempDir(), `{"default": "deepseek/anything"}`)
	if strings.Contains(out, "matches no model") {
		t.Fatalf("an unread catalog is not a miss:\n%s", out)
	}
	for _, want := range []string{"Model reference: 1 model reference not checked against pi's registry",
		"not installed in this jail yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("the check should say %q:\n%s", want, out)
		}
	}
}

// A pack's modelScope pattern that matches no model is warned about, named by the pack and the
// pointer it wrote; one that matches is not.
func TestCheckWarnsAboutAPacksScopePatternThatMatchesNothing(t *testing.T) {
	prefix := t.TempDir()
	installPiCatalog(t, prefix, "deepseek/deepseek-v4-flash")
	pack := scopePack(t, "openrouter/qwen/*")
	out, _ := runReferenceCheck(t, prefix, `{"default": "deepseek/deepseek-v4-flash"}`, pack)
	want := `Model reference: pack ` + filepath.Base(pack) + ` (config-overlay on pi/settings) /subagents/modelScope/allow/0 ` +
		`names the pattern "openrouter/qwen/*", which matches no model in pi's registry`
	if !strings.Contains(out, want) {
		t.Fatalf("the check should say %q:\n%s", want, out)
	}
	if !strings.Contains(out, "ask that pack's author") {
		t.Errorf("a pack's reference names who can fix it:\n%s", out)
	}

	out, _ = runReferenceCheck(t, prefix, `{"default": "deepseek/deepseek-v4-flash"}`,
		scopePack(t, "openrouter/deepseek/*"))
	if strings.Contains(out, "matches no model") {
		t.Errorf("a pattern that matches a catalog id must not be warned about:\n%s", out)
	}
	if !strings.Contains(out, "Model reference: every model pi's config names resolves in its registry (2 references checked)") {
		t.Errorf("a clean check says what it checked:\n%s", out)
	}
}

// On a provider with a declared list the list's ids are in the registry beside pi's catalog: an
// id of neither is warned about, and an id of the list is not.
func TestCheckResolvesASubagentAliasThroughTheProvidersList(t *testing.T) {
	prefix := t.TempDir()
	installPiCatalog(t, prefix, "glm-5.2")
	packsFixture(t, `{"packs": ["pi", "zai"],
  "profiles": {"z": {"provider": "zai", "subagents": {"default": "glm-5.3-flash", "roles": {"scout": "glm-9"}}}}}`)
	env := map[string]string{"YOLO_VERSION": "test", "NPM_CONFIG_PREFIX": prefix}
	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{Getenv: func(k string) string { return env[k] }}).sectionPacks(r, useProfiles("pi", "z"))
	out := buf.String()
	if !strings.Contains(out, `profiles.z.subagents.roles.scout names the model "glm-9" (pi receives "zai/glm-9")`) {
		t.Fatalf("an id off the list must be warned about:\n%s", out)
	}
	if !strings.Contains(out, "Nearest: zai/glm-") {
		t.Errorf("the warning names the list's nearest ids:\n%s", out)
	}
	if strings.Contains(out, `subagents.default names`) {
		t.Errorf("an id on the list must not be warned about:\n%s", out)
	}
}
