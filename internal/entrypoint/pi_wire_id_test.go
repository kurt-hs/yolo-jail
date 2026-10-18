package entrypoint

import (
	"encoding/json"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// pi_wire_id_test.go pins `wire_id` on an object-form `providers.<name>.models.<alias>` (the
// downstream handoff's fix 14 and fix 17): one provider carries one model at two windows, the
// full-window alias and a narrowed twin whose `id` is a client spelling and whose `wire_id` is the
// id both send. Both rows reach pi, each with its own facts, through the boot render a launch runs
// and the shipped model-lists extension.

// wireTwins declares the twin pair on the built-in openrouter (rendered to the model-lists file,
// the routing making it a registration) and on acme2, a provider pi does not ship (rendered to
// models.json).
const wireTwins = `{
 "openrouter":{"models":{
  "floor":{"id":"deepseek/deepseek-v4.1-flash:floor","base":"deepseek/deepseek-v4.1-flash",
    "openrouter_routing":{"order":["streamlake"],"allow_fallbacks":false}},
  "floor-200k":{"id":"deepseek/deepseek-v4.1-flash:floor@200k","wire_id":"deepseek/deepseek-v4.1-flash:floor",
    "base":"deepseek/deepseek-v4.1-flash","context_window":204800,
    "openrouter_routing":{"order":["streamlake"],"allow_fallbacks":false}}}},
 "acme2":{"endpoints":{"openai":{"base_url":"https://acme2.example/v1","wire_api":"openai-chat-completions"}},
  "api_key_env_name":"ACME2_API_KEY",
  "models":{"default":{"id":"acme-big","context_window":1000000},
   "big-200k":{"id":"acme-big@200k","wire_id":"acme-big","context_window":200000}}}}`

func wireTwinsRender(t *testing.T) *pioencodeRender {
	t.Helper()
	decoded, err := jsonx.Decode([]byte(wireTwins))
	if err != nil {
		t.Fatal(err)
	}
	user, _ := decoded.(*jsonx.OrderedMap)
	providers, err := packload.ComposeProviders(user, testPacksForAgent(t, "pi"))
	if err != nil {
		t.Fatal(err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(`{"openrouter":{"provider":"openrouter"}}`)
	r.render(t, `{"pi":"openrouter"}`)
	return r
}

// A TWIN WITH NO PIN ON pi'S OWN openrouter: pi's built-in provider gets no models.json row, so
// the twin reaches pi only through a registration, which a `wire_id` (or a `base`) now triggers on
// its own, with no routing and no `only`.
func TestPiRegistersAWireIdTwinWithNoPinOnABuiltInProvider(t *testing.T) {
	decoded, err := jsonx.Decode([]byte(`{"openrouter":{"models":{
	  "floor":{"id":"deepseek/deepseek-v4.1-flash:floor","base":"deepseek/deepseek-v4.1-flash"},
	  "floor-200k":{"id":"deepseek/deepseek-v4.1-flash:floor@200k","wire_id":"deepseek/deepseek-v4.1-flash:floor",
	    "base":"deepseek/deepseek-v4.1-flash","context_window":204800}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	user, _ := decoded.(*jsonx.OrderedMap)
	providers, err := packload.ComposeProviders(user, testPacksForAgent(t, "pi"))
	if err != nil {
		t.Fatal(err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(`{"openrouter":{"provider":"openrouter"}}`)
	r.render(t, `{"pi":"openrouter"}`)
	rows, _ := piModelListsFile(t, r)["openrouter"]["models"].([]any)
	ids := map[any]bool{}
	for _, row := range rows {
		ids[row.(map[string]any)["id"]] = true
	}
	if !ids["deepseek/deepseek-v4.1-flash:floor"] || !ids["deepseek/deepseek-v4.1-flash:floor@200k"] {
		t.Errorf("openrouter registration rows = %v, want the full-window alias and its twin", rows)
	}
}

// A MODELS.JSON PROVIDER: both rows, the twin carrying `wireId` and its own window.
func TestPiWritesAWireIdTwinIntoModelsJSON(t *testing.T) {
	m := wireTwinsRender(t).piModels(t)
	full := piRow(t, m, "acme2", "acme-big")
	if full["contextWindow"] != float64(1000000) {
		t.Errorf("acme-big = %v, want its full window", full)
	}
	if _, has := full["wireId"]; has {
		t.Errorf("acme-big = %v, states no wire_id and must carry no wireId", full)
	}
	twin := piRow(t, m, "acme2", "acme-big@200k")
	if twin["wireId"] != "acme-big" || twin["contextWindow"] != float64(200000) {
		t.Errorf("acme-big@200k = %v, want wireId acme-big and the 200k window", twin)
	}
}

// A REGISTERED LIST: both rows in the model-lists file, the twin with its `wireId` and `base`, and
// the shipped extension forwards `wireId` into the definition pi registers.
func TestPiRegistersAWireIdTwinBesideItsFullWindowAlias(t *testing.T) {
	r := wireTwinsRender(t)
	lists := piModelListsFile(t, r)
	rows, _ := lists["openrouter"]["models"].([]any)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		m, _ := row.(map[string]any)
		id, _ := m["id"].(string)
		byID[id] = m
	}
	full, twin := byID["deepseek/deepseek-v4.1-flash:floor"], byID["deepseek/deepseek-v4.1-flash:floor@200k"]
	if full == nil || twin == nil {
		t.Fatalf("openrouter rows = %v, want the full-window alias and its twin", rows)
	}
	if _, has := full["wireId"]; has {
		t.Errorf("full row = %v, states no wire_id", full)
	}
	if twin["wireId"] != "deepseek/deepseek-v4.1-flash:floor" || twin["base"] != "deepseek/deepseek-v4.1-flash" ||
		twin["contextWindow"] != float64(204800) {
		t.Errorf("twin = %v, want wireId, base and the 200k window", twin)
	}

	raw, err := json.Marshal(map[string]any{"providers": lists})
	if err != nil {
		t.Fatal(err)
	}
	run := runPiModelListsExtension(t, raw, piOpenRouterCatalogStub, piCompatStub)
	if len(run.Registrations) != 1 {
		t.Fatalf("registrations = %+v, want openrouter's", run.Registrations)
	}
	var registered map[string]any
	for _, m := range run.Registrations[0].Models {
		if m["id"] == "deepseek/deepseek-v4.1-flash:floor@200k" {
			registered = m
		}
	}
	if registered == nil || registered["wireId"] != "deepseek/deepseek-v4.1-flash:floor" ||
		registered["contextWindow"] != float64(204800) {
		t.Errorf("registered twin = %v, want wireId forwarded and the 200k window kept", registered)
	}
}
