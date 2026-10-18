package entrypoint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// pi_bedrock_overrides_test.go pins the models.json half of OQ-ML4 (docs/design/
// model-lists-and-pickers.md, ruled 2026-10-16; the downstream handoff's patch series 9): a pi
// model row on pi's own Bedrock client keeps every catalog fact it does not state, thinking
// levels included, in models.json itself and so for a pi with no patch and no yolo extension.
// pi's models.json `models` entry REPLACES the catalog entry of its id, and its `modelOverrides`
// entry merges over it field by field (pi 1.0.1 core/provider-composer.js, applyModelOverride), so
// a row naming a catalog id is written as an override. A row with a `base`, a `wire_id` or a
// tombstone stays a `models` row: a variant the catalog lacks, or a fact the row must remove.

const lunaProviders = `{"bedrock":{"region":"us-east-1","models":{
  "luna":{"id":"global.openai.gpt-6-luna","vendor":"openai","context_window":272000},
  "luna-1m":{"id":"global.openai.gpt-6-luna[1m]","base":"global.openai.gpt-6-luna","vendor":"openai",
    "context_window":1000000},
  "plain":{"id":"global.openai.gpt-6-astra","vendor":"openai","reasoning":null}}}}`

func TestPiOnBedrockKeepsTheCatalogsThinkingLevelsInModelsJSON(t *testing.T) {
	providersJSON, wire := bedrockTables(t, "pi", lunaProviders, nil)
	r := newPioencodeRender(t, providersJSON)
	r.wireProfiles(wire)
	r.render(t, `{"pi":"bedrock"}`)
	rows, _ := r.piModels(t)["providers"].(map[string]any)
	native, _ := rows["amazon-bedrock"].(map[string]any)
	if native == nil {
		t.Fatalf("no amazon-bedrock row: %v", rows)
	}
	overrides, _ := native["modelOverrides"].(map[string]any)
	if got, want := overrides["global.openai.gpt-6-luna"], map[string]any{"contextWindow": float64(272000)}; !reflect.DeepEqual(got, want) {
		t.Errorf("modelOverrides[luna] = %v, want %v: the stated facts alone, over pi's catalog entry", got, want)
	}
	ids := map[string]map[string]any{}
	list, _ := native["models"].([]any)
	for _, m := range list {
		row, _ := m.(map[string]any)
		id, _ := row["id"].(string)
		ids[id] = row
	}
	if _, replaced := ids["global.openai.gpt-6-luna"]; replaced {
		t.Errorf("luna is a models row, which replaces pi's catalog entry whole: %v", list)
	}
	if ids["global.openai.gpt-6-luna[1m]"] == nil || ids["global.openai.gpt-6-astra"] == nil {
		t.Errorf("models = %v, want the [1m] variant (a base) and astra (a tombstone) as full rows", list)
	}
	if _, overridden := overrides["global.openai.gpt-6-astra"]; overridden {
		t.Errorf("astra tombstones reasoning, which an override cannot remove: %v", overrides)
	}

	// AND pi ITSELF, where it is installed: stock pi loads the file and its own catalog, and the
	// luna it composes still offers xhigh with yolo's window.
	pkg := installedPiPackage(t)
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.json")
	raw, err := json.Marshal(r.piModels(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelsPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(dir, "harness.mjs")
	if err := os.WriteFile(harness, []byte(piComposeHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(requireNode(t, "pi's own provider composition"), harness, pkg, modelsPath,
		"global.openai.gpt-6-luna").CombinedOutput()
	if err != nil {
		t.Fatalf("harness: %v\n%s", err, out)
	}
	var composed map[string]struct {
		Error            string         `json:"error"`
		ThinkingLevelMap map[string]any `json:"thinkingLevelMap"`
		ContextWindow    float64        `json:"contextWindow"`
	}
	if err := json.Unmarshal(out, &composed); err != nil {
		t.Fatalf("harness output %s: %v", out, err)
	}
	luna := composed["global.openai.gpt-6-luna"]
	if luna.Error != "" {
		t.Fatalf("pi refused the rendered models.json: %s", luna.Error)
	}
	if luna.ThinkingLevelMap["xhigh"] == nil || luna.ContextWindow != 272000 {
		t.Errorf("pi's composed luna = %+v, want the catalog's xhigh and yolo's 272000 window", luna)
	}
}

// piComposeHarness loads a models.json through pi's own ModelConfig and composes the built-in
// amazon-bedrock provider over it with pi's own composer and catalog, printing each named model's
// composed facts (or the load error under every name).
const piComposeHarness = `
const [pkg, modelsPath, ...ids] = process.argv.slice(2);
const { ModelConfig } = await import(pkg + "/dist/core/model-config.js");
const { composeModelProvider } = await import(pkg + "/dist/core/provider-composer.js");
const all = await import(pkg + "/node_modules/@earendil-works/pi-ai/dist/providers/all.js");
const cfg = await ModelConfig.load(modelsPath);
const out = {};
if (cfg.getError()) {
	for (const id of ids) out[id] = { error: cfg.getError() };
} else {
	const base = all.builtinProviders().find((p) => p.id === "amazon-bedrock");
	const provider = composeModelProvider("amazon-bedrock", base, cfg, undefined);
	const models = provider.getAllModels ? provider.getAllModels() : provider.getModels();
	for (const m of models) {
		if (ids.includes(m.id)) out[m.id] = { thinkingLevelMap: m.thinkingLevelMap, contextWindow: m.contextWindow };
	}
}
console.log(JSON.stringify(out));
`
