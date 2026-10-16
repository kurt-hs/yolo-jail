package entrypoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// pi_model_facts_test.go pins pi's half of three rulings in docs/design/model-lists-and-pickers.md,
// each through the boot render a launch runs and, where the extension takes part, the shipped
// extension under node:
//
//   - OQ-MM3: a list no `only` narrowed refuses nothing. pi registers a routed list with no `only`
//     without its refusing wrapper, and pi-subagents' scope is strict over a provider's ids only
//     under an `only` (and on openai-codex, whose list is everything the subscription serves).
//   - OQ-ML4 and ML-D10: a model row merges over pi's catalog field by field, and a fact written
//     as null is a tombstone that removes it, a user's null over a pack's fact included.
//   - OQ-ML5 and ML-D11: a model's routes may share facts written once, each route overriding any
//     of them, and the shared form reaches models.json only for a pi whose pack declares it
//     reads it.

// piModelListsFile decodes the pi/model-lists file a render wrote.
func piModelListsFile(t *testing.T, r *pioencodeRender) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.e.Home, filepath.FromSlash(piModelListsRel(t))))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("model-lists file %s: %v", raw, err)
	}
	return file.Providers
}

// OQ-MM3: A ROUTED LIST WITH NO `only` REFUSES NOTHING. The routing still makes the declared rows
// pi's openrouter menu (the registration is the one channel that carries a row's routing), but
// the registration is `models` alone, so a typed model outside it goes to OpenRouter. Before, it
// carried `enforce` and the refusing wrapper.
func TestPiRoutedListWithNoOnlyIsNotRefused(t *testing.T) {
	raw := renderedOpenRouterLists(t)
	var file struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if got := file.Providers["openrouter"]["enforce"]; got != false {
		t.Errorf("openrouter enforce = %v, want false: no `only` narrowed the list (OQ-MM3)", got)
	}
	run := runPiModelListsExtension(t, raw, piOpenRouterCatalogStub, piCompatStub)
	if len(run.Registrations) != 1 || strings.Join(run.Registrations[0].Keys, ",") != "models" {
		t.Errorf("registrations = %+v, want openrouter's `models` alone, no refusing streamSimple", run.Registrations)
	}

	// UNDER AN `only` THE REFUSAL STAYS.
	var narrowed struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(renderedZaiLists(t, false), &narrowed); err != nil {
		t.Fatal(err)
	}
	if got := narrowed.Providers["zai"]["enforce"]; got != true {
		t.Errorf("zai under an only: enforce = %v, want true", got)
	}
}

// acmeProviders is a provider pi does not ship, with a list and no `only`.
const acmeProviders = `{"acme":{"endpoints":{"openai":{"base_url":"https://acme.example/v1",
  "wire_api":"openai-chat-completions"}},"api_key_env_name":"ACME_API_KEY",
  "models":{"default":"acme-large","fast":"acme-mini"}}}`

// composedFor composes user providers JSON (and shared facts JSON, may be "") over pi's packs.
func composedFor(t *testing.T, userJSON, sharedJSON string, extra ...string) string {
	t.Helper()
	decode := func(s string) *jsonx.OrderedMap {
		if s == "" {
			return nil
		}
		v, err := jsonx.Decode([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		m, _ := v.(*jsonx.OrderedMap)
		return m
	}
	table, err := packload.ComposeProviders(decode(userJSON), testPacksForAgent(t, "pi", extra...),
		packload.WithModelFacts(decode(sharedJSON)))
	if err != nil {
		t.Fatal(err)
	}
	return mustCompactJSON(t, table)
}

func subagentScope(t *testing.T, settings map[string]any) []any {
	t.Helper()
	sub, _ := settings["subagents"].(map[string]any)
	scope, _ := sub["modelScope"].(map[string]any)
	allow, _ := scope["allow"].([]any)
	return allow
}

// OQ-MM3: PI-SUBAGENTS' SCOPE IS THE WHOLE PROVIDER WITHOUT AN `only`. A child may still never
// cross providers, but a model id the user names on this provider is not refused for being off a
// list nobody narrowed. Under an `only` the scope stays strict over the list.
func TestPiSubagentsScopeIsStrictOnlyUnderAnOnly(t *testing.T) {
	r := newPioencodeRender(t, composedFor(t, acmeProviders, ""))
	r.render(t, `{"pi":"acme"}`)
	if got, want := subagentScope(t, r.piSettings(t)), []any{"acme/*"}; !reflect.DeepEqual(got, want) {
		t.Errorf("acme (no only) subagent scope = %v, want %v", got, want)
	}

	providers, profiles := zaiNarrowed(t, "pi", nil)
	r = newPioencodeRender(t, providers)
	r.wireProfiles(profiles)
	r.render(t, `{"pi":"zai"}`)
	if got, want := subagentScope(t, r.piSettings(t)), []any{"zai/glm-5.3", "zai/glm-5.3-flash"}; !reflect.DeepEqual(got, want) {
		t.Errorf("zai under an only: subagent scope = %v, want %v", got, want)
	}
}

// piBedrockCatalogStub stands in for pi-ai's catalog on amazon-bedrock: it knows Opus, with the
// thinking levels and a reasoning flag no yolo row states, and one model no yolo list names.
const piBedrockCatalogStub = `
const opus = { id: "global.anthropic.claude-opus-5-5", name: "Catalog Opus", api: "bedrock-converse-stream",
	provider: "amazon-bedrock", baseUrl: "https://bedrock.example", reasoning: true, input: ["text", "image"],
	contextWindow: 200000, maxTokens: 64000, cost: { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
	thinkingLevelMap: { off: null, high: "high", xhigh: "xhigh" } };
const other = { id: "amazon.nova-pro", name: "Nova Pro", api: "bedrock-converse-stream", provider: "amazon-bedrock",
	baseUrl: "https://bedrock.example", reasoning: false, input: ["text"], contextWindow: 300000, maxTokens: 5000,
	cost: { input: 1, output: 4, cacheRead: 0, cacheWrite: 0 } };
export const calls = [];
export function getBuiltinModel(provider, id) {
	if (provider !== "amazon-bedrock") return undefined;
	return [opus, other].find((m) => m.id === id);
}
export function getBuiltinModels(provider) { return provider === "amazon-bedrock" ? [opus, other] : []; }
export function builtinProviders() { return []; }
`

// OQ-ML4 AND ML-D10: A NATIVE BEDROCK ROW KEEPS EVERY CATALOG FACT IT DOES NOT STATE, AND A NULL
// REMOVES ONE. The derive cannot see pi's catalog, so the merge runs in the shipped extension
// (ML-D12): the boot render hands it the rows it wrote into models.json, marked to complete pi's
// catalog rather than replace it, and the registration is pi's catalog with each row merged over
// its own catalog entry. The user's null over the pack's reasoning flag removes it there too.
func TestPiBedrockRowsMergeOverPisCatalogAndTombstonesRemove(t *testing.T) {
	providersJSON, wire := bedrockListTables(t, "pi", `{"bedrock":{"region":"eu-west-1","models":{
	  "global.anthropic.claude-opus-5-5":{"id":"global.anthropic.claude-opus-5-5","reasoning":null}}}}`, nil)
	r := newPioencodeRender(t, providersJSON)
	r.wireProfiles(wire)
	r.render(t, `{"pi":"bedrock"}`)

	// models.json's row states what yolo knows and leaves the tombstoned fact out.
	rows, _ := r.piModels(t)["providers"].(map[string]any)
	native, _ := rows["amazon-bedrock"].(map[string]any)
	models, _ := native["models"].([]any)
	if len(models) == 0 {
		t.Fatalf("no native Bedrock row: %v", rows)
	}
	opusRow, _ := models[0].(map[string]any)
	if _, has := opusRow["reasoning"]; has || opusRow["id"] != "global.anthropic.claude-opus-5-5" {
		t.Errorf("models.json opus row = %v, want it without reasoning, the user's tombstone", opusRow)
	}

	lists := piModelListsFile(t, r)
	entry := lists["amazon-bedrock"]
	if entry == nil || entry["keep_catalog"] != true || entry["enforce"] != false {
		t.Fatalf("model-lists amazon-bedrock = %v, want the rows to complete pi's catalog, unenforced", entry)
	}

	raw, err := os.ReadFile(filepath.Join(r.e.Home, filepath.FromSlash(piModelListsRel(t))))
	if err != nil {
		t.Fatal(err)
	}
	run := runPiModelListsExtension(t, raw, piBedrockCatalogStub, piCompatStub)
	if len(run.Registrations) != 1 || run.Registrations[0].Name != "amazon-bedrock" ||
		strings.Join(run.Registrations[0].Keys, ",") != "models" {
		t.Fatalf("registrations = %+v, want amazon-bedrock's models alone", run.Registrations)
	}
	byID := map[string]map[string]any{}
	var ids []string
	for _, m := range run.Registrations[0].Models {
		id, _ := m["id"].(string)
		byID[id] = m
		ids = append(ids, id)
	}
	if !slices.Contains(ids, "amazon.nova-pro") {
		t.Errorf("registered ids = %v, want pi's own catalog model kept beside yolo's rows", ids)
	}
	opus := byID["global.anthropic.claude-opus-5-5"]
	if opus == nil {
		t.Fatalf("registered ids = %v, want yolo's opus row", ids)
	}
	if opus["contextWindow"] != float64(1000000) || opus["maxTokens"] != float64(128000) {
		t.Errorf("opus limits = %v/%v, want the row's own 1000000/128000", opus["contextWindow"], opus["maxTokens"])
	}
	if levels, _ := opus["thinkingLevelMap"].(map[string]any); levels["xhigh"] != "xhigh" {
		t.Errorf("opus thinkingLevelMap = %v, want the catalog's, which the row does not state", opus["thinkingLevelMap"])
	}
	if cost, _ := opus["cost"].(map[string]any); cost["output"] != float64(25) {
		t.Errorf("opus cost = %v, want the catalog's", opus["cost"])
	}
	if opus["reasoning"] != false {
		t.Errorf("opus reasoning = %v, want false: the user's null removes the catalog's true", opus["reasoning"])
	}
	for _, k := range []string{"api", "baseUrl", "provider", "tombstones"} {
		if _, has := opus[k]; has {
			t.Errorf("registered opus carries %s", k)
		}
	}
}

// namedFixturePack loads a pack named name whose contributions are contributes.
func namedFixturePack(t *testing.T, name, contributes string) *packload.Pack {
	t.Helper()
	dir := t.TempDir()
	writeHostFile(t, filepath.Join(dir, "pack.json"), `{"name":"`+name+`","description":"d","contributes":[`+contributes+`]}`)
	p, problems := packload.LoadDir(dir, name)
	if p == nil || len(problems) != 0 {
		t.Fatalf("the %s fixture did not load: %v", name, problems)
	}
	return p
}

// A PARTIAL COST INHERITS THE CATALOG'S OTHER RATES (OQ-ML4: "don't drop things unnecessarily").
// A pack states one rate; models.json, whose schema needs all four, fills the rest with 0, but the
// menu row states that one rate alone and the extension merges it over the catalog's cost rate by
// rate, so the three rates nobody stated keep pi's values.
func TestPiPartialCostKeepsTheCatalogsOtherRates(t *testing.T) {
	br := namedFixturePack(t, "mybr", `{"kind":"provider","name":"mybr","platform":"aws-bedrock",
	  "models":{"global.anthropic.claude-opus-5-5":"global.anthropic.claude-opus-5-5"},
	  "model_options":{"global.anthropic.claude-opus-5-5":{"cost_input":"9"}}}`)
	table, err := packload.ComposeProviders(nil, append(testPacksForAgent(t, "pi"), br))
	if err != nil {
		t.Fatal(err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, table))
	r.render(t, `{"pi":"mybr"}`)

	row := piRow(t, r.piModels(t), "amazon-bedrock", "global.anthropic.claude-opus-5-5")
	if want := map[string]any{"input": float64(9), "output": float64(0), "cacheRead": float64(0), "cacheWrite": float64(0)}; !reflect.DeepEqual(row["cost"], want) {
		t.Errorf("models.json cost = %v, want %v (its schema needs all four)", row["cost"], want)
	}
	entry := piModelListsFile(t, r)["amazon-bedrock"]
	models, _ := entry["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("menu entry = %v", entry)
	}
	if got := models[0].(map[string]any)["cost"]; !reflect.DeepEqual(got, map[string]any{"input": float64(9)}) {
		t.Errorf("menu row cost = %v, want the stated rate alone", got)
	}
	raw, err := os.ReadFile(filepath.Join(r.e.Home, filepath.FromSlash(piModelListsRel(t))))
	if err != nil {
		t.Fatal(err)
	}
	run := runPiModelListsExtension(t, raw, piBedrockCatalogStub, piCompatStub)
	if len(run.Registrations) != 1 {
		t.Fatalf("registrations = %+v", run.Registrations)
	}
	for _, m := range run.Registrations[0].Models {
		if m["id"] != "global.anthropic.claude-opus-5-5" {
			continue
		}
		want := map[string]any{"input": float64(9), "output": float64(25), "cacheRead": 0.5, "cacheWrite": 6.25}
		if !reflect.DeepEqual(m["cost"], want) {
			t.Errorf("registered cost = %v, want %v: the stated rate over the catalog's others", m["cost"], want)
		}
	}
}

// BEDROCK UNDER AN `only` IS A NARROWED MENU, NOT A CATALOG COMPLETION: the model-lists entry is the
// ordinary narrowed one (no `keep_catalog`, enforced while the switch is on), and pi-subagents'
// scope is strict over the narrowed list (OQ-MM3).
func TestPiBedrockUnderAnOnlyIsNarrowedAndStrict(t *testing.T) {
	const opus, sol = "global.anthropic.claude-opus-5-5", "us.openai.gpt-6.1-sol"
	packs := append(testPacksForAgent(t, "pi", "bedrock"), companyModelsPack(t, bedrockListAdd),
		namedFixturePack(t, "narrower", `{"kind":"models","provider":"bedrock","only":["`+opus+`","`+sol+`"]}`))
	providersJSON, wire := bedrockTablesFor(t, packs, `{"bedrock":{"region":"eu-west-1"}}`, nil)
	r := newPioencodeRender(t, providersJSON)
	r.wireProfiles(wire)
	r.render(t, `{"pi":"bedrock"}`)
	entry := piModelListsFile(t, r)["amazon-bedrock"]
	if entry == nil {
		t.Fatal("no narrowed amazon-bedrock entry")
	}
	if _, has := entry["keep_catalog"]; has || entry["enforce"] != true {
		t.Errorf("narrowed entry = %v, want no keep_catalog and enforce true", entry)
	}
	got := subagentScope(t, r.piSettings(t))
	if len(got) != 2 || !slices.Contains(got, any("amazon-bedrock/"+opus)) || !slices.Contains(got, any("amazon-bedrock/"+sol)) {
		t.Errorf("subagent scope = %v, want strict over the two narrowed ids", got)
	}
}

// A `keep_catalog` LIST WITH NO CATALOG TO COMPLETE REGISTERS NOTHING: the registration would only
// replace pi's list with yolo's rows, and the models.json rows pi already loaded say as much
// without narrowing the menu.
func TestPiKeepCatalogWithNoCatalogRegistersNothing(t *testing.T) {
	lists := []byte(`{"providers":{"amazon-bedrock":{"models":[{"id":"x","contextWindow":1000}],` +
		`"enforce":false,"keep_catalog":true}}}`)
	run := runPiModelListsExtension(t, lists, piZaiCatalogStub, piCompatStub)
	if len(run.Registrations) != 0 {
		t.Errorf("registrations = %+v, want none", run.Registrations)
	}
}

// sharedRoutes is one model on two routes pi does not ship, sharing the facts written once in
// `model_facts`: acme states its own smaller window, and acme2 tombstones the shared reasoning.
const sharedRoutes = `{
  "acme":{"endpoints":{"openai":{"base_url":"https://acme.example/v1","wire_api":"openai-chat-completions"}},
    "api_key_env_name":"ACME_API_KEY","models":{"big":{"id":"acme-big","extends":"big","context_window":64000}}},
  "acme2":{"endpoints":{"openai":{"base_url":"https://acme2.example/v1","wire_api":"openai-chat-completions"}},
    "api_key_env_name":"ACME2_API_KEY","models":{"big":{"id":"big-2","extends":"big"},
      "odd":{"id":"odd-2","extends":"big","reasoning":null}}}}`

const sharedBig = `{"big":{"reasoning":true,"input":["text","image"],"context_window":128000,"max_tokens":32000}}`

func piRow(t *testing.T, models map[string]any, provider, id string) map[string]any {
	t.Helper()
	p, _ := models["providers"].(map[string]any)
	row, _ := p[provider].(map[string]any)
	list, _ := row["models"].([]any)
	for _, m := range list {
		if mm, _ := m.(map[string]any); mm["id"] == id {
			return mm
		}
	}
	t.Fatalf("models.json has no %s/%s row: %v", provider, id, models)
	return nil
}

// OQ-ML5 ON STOCK PI: FULL ROWS. Each route's row carries the shared facts under its own, and no
// `sharedModels` or `extends` reaches a pi that would silently ignore them.
func TestPiRoutesShareFactsAsFullRowsByDefault(t *testing.T) {
	r := newPioencodeRender(t, composedFor(t, sharedRoutes, sharedBig))
	r.render(t, `{"pi":"acme"}`)
	m := r.piModels(t)
	if _, has := m["sharedModels"]; has {
		t.Errorf("models.json carries sharedModels for a pi that does not read it: %v", m["sharedModels"])
	}
	want := map[string]map[string]any{
		"acme-big": {"contextWindow": float64(64000), "maxTokens": float64(32000), "reasoning": true},
		"big-2":    {"contextWindow": float64(128000), "maxTokens": float64(32000), "reasoning": true},
		"odd-2":    {"contextWindow": float64(128000), "maxTokens": float64(32000)},
	}
	for id, facts := range want {
		provider := "acme2"
		if id == "acme-big" {
			provider = "acme"
		}
		row := piRow(t, m, provider, id)
		for k, v := range facts {
			if row[k] != v {
				t.Errorf("%s %s = %v, want %v", id, k, row[k], v)
			}
		}
		if !reflect.DeepEqual(row["input"], []any{"text", "image"}) {
			t.Errorf("%s input = %v, want the shared text,image", id, row["input"])
		}
		if _, has := row["extends"]; has {
			t.Errorf("%s carries extends on a stock pi", id)
		}
	}
	if _, has := piRow(t, m, "acme2", "odd-2")["reasoning"]; has {
		t.Errorf("odd-2 carries reasoning; its own tombstone removes the shared flag")
	}
}

// piReadingSharedModels is the shipped pi pack with its program declaring it reads the shared
// form, as a pack shipping such a pi build would (ML-D11).
func piReadingSharedModels(t *testing.T) *packload.Pack {
	t.Helper()
	pi, err := embeddedPack("pi")
	if err != nil {
		t.Fatal(err)
	}
	cp := *pi
	decl := *pi.Decl
	decl.Contributes = slices.Clone(decl.Contributes)
	found := false
	for i := range decl.Contributes {
		if decl.Contributes[i].Kind == "program" && decl.Contributes[i].Bin == "pi" {
			decl.Contributes[i].ReadsSharedModels = true
			found = true
		}
	}
	if !found {
		t.Fatal("packs/pi installs no pi program")
	}
	cp.Decl = &decl
	return &cp
}

// OQ-ML5 + ML-D11: THE SHARED FORM, ONLY WHERE THE PI THAT RUNS READS IT. The shared facts are
// written once as `sharedModels`, each row says `extends` and states only what it overrides, and a
// row that tombstones a shared fact is written in full, since leaving a fact out of an `extends`
// row inherits it.
func TestPiWritesTheSharedFormOnlyForAPiThatReadsIt(t *testing.T) {
	r := newPioencodeRender(t, composedFor(t, sharedRoutes, sharedBig))
	r.e.Vars["YOLO_USE_PROFILES"] = `{"pi":"acme"}`
	r.wireProfiles(nameAsProviderTable(t, `{"pi":"acme"}`))
	ocode, err := embeddedPack("opencode")
	if err != nil {
		t.Fatal(err)
	}
	ConfigurePackSurfaces(r.e, []*packload.Pack{piReadingSharedModels(t), ocode})
	if fails := r.e.GenFailures(); len(fails) != 0 {
		t.Fatalf("boot render failed: %v\n%s", fails, r.errw.String())
	}
	m := r.piModels(t)
	shared, _ := m["sharedModels"].(map[string]any)
	big, _ := shared["big"].(map[string]any)
	if big["contextWindow"] != float64(128000) || big["maxTokens"] != float64(32000) || big["reasoning"] != true ||
		!reflect.DeepEqual(big["input"], []any{"text", "image"}) {
		t.Fatalf("sharedModels.big = %v, want the shared facts once", shared)
	}
	acme := piRow(t, m, "acme", "acme-big")
	if acme["extends"] != "big" || acme["contextWindow"] != float64(64000) {
		t.Errorf("acme-big = %v, want extends big with its own window", acme)
	}
	for _, k := range []string{"maxTokens", "reasoning", "input"} {
		if _, has := acme[k]; has {
			t.Errorf("acme-big restates the shared %s: %v", k, acme)
		}
	}
	if b2 := piRow(t, m, "acme2", "big-2"); b2["extends"] != "big" || b2["contextWindow"] != nil {
		t.Errorf("big-2 = %v, want extends big and nothing of its own", b2)
	}
	odd := piRow(t, m, "acme2", "odd-2")
	if _, has := odd["extends"]; has || odd["maxTokens"] != float64(32000) {
		t.Errorf("odd-2 = %v, want a full row: its tombstone cannot be said by leaving a fact out", odd)
	}
	if _, has := odd["reasoning"]; has {
		t.Errorf("odd-2 = %v, want no reasoning", odd)
	}
}
