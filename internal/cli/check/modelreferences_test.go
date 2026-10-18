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

// runPacksCheck runs sectionPacks over cfg (the user config) with pi on profile, no catalog read.
func runPacksCheck(t *testing.T, cfg, profile string) (string, *reporter) {
	t.Helper()
	packsFixture(t, cfg)
	env := map[string]string{"YOLO_VERSION": "test", "NPM_CONFIG_PREFIX": t.TempDir()}
	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{Getenv: func(k string) string { return env[k] }}).sectionPacks(r, useProfiles("pi", profile))
	return buf.String(), r
}

// On a provider with a declared list the list alone decides, catalog or none: an id on it is
// fine, and one off it with no `default` alias to fall back to FAILS, since pi's render refuses
// it and the launch stops.
func TestCheckFailsASubagentModelOffAListWithNoDefault(t *testing.T) {
	out, r := runPacksCheck(t, `{"packs": ["pi", "zai"],
  "profiles": {"z": {"provider": "zai", "subagents": {"default": "glm-5.3-flash", "roles": {"scout": "glm-9"}}}}}`, "z")
	for _, want := range []string{
		`[FAIL] Model reference: profiles.z.subagents.roles.scout names the model "glm-9", which is not on its provider's list`,
		"refuses it and the launch stops", "Nearest: zai/glm-",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the check should say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `subagents.default names`) || r.failed != 1 {
		t.Errorf("only the off-list role fails (failed=%d):\n%s", r.failed, out)
	}
}

// Off a list that declares a `default` alias, the derive starts the children on that instead: a
// warning naming the model they get.
func TestCheckWarnsASubagentModelOffAListFallsBackToItsDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(`{"name": "acme", "contributes": [
	  {"kind": "provider", "name": "acme", "endpoints": {"openai": {"base_url": "https://acme.example.test/v1"}},
	   "models": {"default": "acme-large", "acme-large": "acme-large", "acme-mini": "acme-mini"}},
	  {"kind": "profile", "name": "acme", "provider": "acme"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, r := runPacksCheck(t, `{"packs": ["pi", "file://`+dir+`"],
  "profiles": {"a": {"provider": "acme", "subagents": {"default": "acme-mni"}}}}`, "a")
	want := "child agents start on the list's `default`, acme/acme-large, instead"
	if !strings.Contains(out, `[WARN] Model reference: profiles.a.subagents.default names the model "acme-mni"`) ||
		!strings.Contains(out, want) || !strings.Contains(out, "Nearest: acme/acme-mini") {
		t.Fatalf("the check should warn and say %q:\n%s", want, out)
	}
	if r.failed != 0 {
		t.Errorf("a fallback is not a refusal:\n%s", out)
	}
}

// The list's exact id wins over its `default` alias at the host check, the same ordering the
// selected profile's derive uses. This drives the production sectionPacks report with the model
// named verbatim in the profile rather than an alias.
func TestCheckAcceptsExactSubagentIDBeforeListDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(`{"name": "acme", "contributes": [
	  {"kind": "provider", "name": "acme", "endpoints": {"openai": {"base_url": "https://acme.example.test/v1"}},
	   "models": {"default": "acme/model-large", "large": "acme/model-large", "mini": "acme/model-mini"}},
	  {"kind": "profile", "name": "acme", "provider": "acme"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, r := runPacksCheck(t, `{"packs": ["pi", "file://`+dir+`"],
  "profiles": {"a": {"provider": "acme", "subagents": {"default": "acme/model-mini"}}}}`, "a")
	if strings.Contains(out, `profiles.a.subagents.default names`) || r.failed != 0 {
		t.Fatalf("an exact id on the provider list must not be treated as an off-list fallback:\n%s", out)
	}
}

// Every shipped profile usable by Pi reaches the production host check. The registry is derived
// from the same pack surfaces and provider/profile composition each selected profile uses at boot.
func TestCheckResolvesEveryShippedPiProfileModelReference(t *testing.T) {
	cfg := `{"packs": ["pi", "openai-auth", "zai", "openrouter", "kilo", "llamacpp", "bedrock",
  "cerebras", "meta-provider", "wire-bridge"]}`
	for _, profile := range []string{"codex", "zai", "openrouter", "kilo", "llamacpp", "bedrock",
		"bedrock-bridge", "cerebras", "meta"} {
		t.Run(profile, func(t *testing.T) {
			out, _ := runPacksCheck(t, cfg, profile)
			if strings.Contains(out, "Model reference:") &&
				(strings.Contains(out, "matches no model") || strings.Contains(out, "[FAIL] Model reference:")) {
				t.Fatalf("shipped profile %q produces an unresolved model reference at the host:\n%s", profile, out)
			}
		})
	}
}

// A profile whose provider has no row in the composed table, or that declares no subagents, reads
// nothing and never stops the check.
func TestCheckReadsNothingForAProfileWithNoProviderRow(t *testing.T) {
	out, _ := runPacksCheck(t, `{"packs": ["pi", "zai"], "profiles": {"d": {"provider": "deepseek",
  "subagents": {"default": "deepseek-v4-flash"}}}}`, "d")
	if strings.Contains(out, "is not on its provider's list") {
		t.Errorf("a provider with no list passes the value through:\n%s", out)
	}
	runPacksCheck(t, `{"packs": ["pi"], "profiles": {"cx": {"provider": "openai-codex"}}}`, "cx")
}

// A pack pattern naming one of the openai-codex [1m] variants resolves: the registry holds the ids
// pi's own codex-models surface renders, which the table's list does not spell.
func TestCheckResolvesAPatternAgainstPisRenderedModelFiles(t *testing.T) {
	out, _ := runPacksCheck(t, `{"packs": ["pi", "openai-auth", "file://`+scopePack(t, "openai-codex/gpt-6.1-sol[1m]")+`"]}`,
		"codex")
	if strings.Contains(out, "matches no model") {
		t.Fatalf("a variant pi's codex-models file holds must resolve:\n%s", out)
	}
	if !strings.Contains(out, "Model reference: every model pi's config names resolves in its registry") {
		t.Errorf("the check says what it checked:\n%s", out)
	}
}
