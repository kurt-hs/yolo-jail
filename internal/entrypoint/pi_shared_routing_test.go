package entrypoint

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// A `model_facts` ENTRY CARRIES THE OPENROUTER PIN (the downstream handoff's fix 27). A 1M route
// and its 200k twin send the same route and must send byte-identical requests, so both extend one
// shared entry stating `openrouter_routing` once, and each pi models.json row carries it as
// `compat.openRouterRouting`. A row stating its own pin REPLACES the shared one whole, and a row
// extending an entry with no pin renders with no compat, as before.
func TestPiRowsExtendingASharedEntryCarryItsOpenRouterPin(t *testing.T) {
	decoded, err := jsonx.Decode([]byte(`{"or":{
	  "endpoints":{"openai":{"base_url":"https://openrouter.ai/api/v1","wire_api":"openai-chat-completions"}},
	  "api_key_env_name":"OPENROUTER_API_KEY",
	  "models":{
	    "floor":{"id":"deepseek/deepseek-v4.1-flash:floor","extends":"flash-floor","context_window":1048576},
	    "floor-200k":{"id":"deepseek/deepseek-v4.1-flash:floor@200k","wire_id":"deepseek/deepseek-v4.1-flash:floor",
	      "extends":"flash-floor","context_window":204800},
	    "own":{"id":"deepseek/deepseek-v4.1-flash:own","extends":"flash-floor",
	      "openrouter_routing":{"order":["deepinfra"]}},
	    "plain":{"id":"deepseek/deepseek-v4.1-flash","extends":"flash-plain"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	user, _ := decoded.(*jsonx.OrderedMap)
	sharedDecoded, err := jsonx.Decode([]byte(`{
	  "flash-floor":{"reasoning":true,"openrouter_routing":{"order":["streamlake","morph"],"allow_fallbacks":false}},
	  "flash-plain":{"reasoning":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := sharedDecoded.(*jsonx.OrderedMap)
	providers, err := packload.ComposeProviders(user, testPacksForAgent(t, "pi"), packload.WithModelFacts(shared))
	if err != nil {
		t.Fatal(err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(`{"or":{"provider":"or"}}`)
	r.render(t, `{"pi":"or"}`)
	m := r.piModels(t)

	pin := func(id string) any {
		compat, _ := piRow(t, m, "or", id)["compat"].(map[string]any)
		return compat["openRouterRouting"]
	}
	want := map[string]any{"order": []any{"streamlake", "morph"}, "allow_fallbacks": false}
	for _, id := range []string{"deepseek/deepseek-v4.1-flash:floor", "deepseek/deepseek-v4.1-flash:floor@200k"} {
		if got := pin(id); !reflect.DeepEqual(got, want) {
			t.Errorf("row %s compat.openRouterRouting = %v, want the shared entry's %v", id, got, want)
		}
	}
	if got, want := pin("deepseek/deepseek-v4.1-flash:own"), map[string]any{"order": []any{"deepinfra"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("a row's own pin must replace the shared one whole: got %v, want %v", got, want)
	}
	if plain := piRow(t, m, "or", "deepseek/deepseek-v4.1-flash"); plain["compat"] != nil {
		t.Errorf("a row extending an entry with no pin = %v, want no compat", plain)
	}

	// The pi/model-lists registration carries the same inherited pin on each row, which the
	// extension lowers into the request's `provider` field.
	lists, _ := r.surface(t, ".pi", "agent", "yolo-model-lists.json")["providers"].(map[string]any)
	or, _ := lists["or"].(map[string]any)
	listed, _ := or["models"].([]any)
	registered := map[string]any{}
	for _, row := range listed {
		mm, _ := row.(map[string]any)
		id, _ := mm["id"].(string)
		registered[id] = mm["openrouter_routing"]
	}
	for id, w := range map[string]any{
		"deepseek/deepseek-v4.1-flash:floor":      want,
		"deepseek/deepseek-v4.1-flash:floor@200k": want,
		"deepseek/deepseek-v4.1-flash:own":        map[string]any{"order": []any{"deepinfra"}},
		"deepseek/deepseek-v4.1-flash":            nil,
	} {
		got, ok := registered[id]
		if !ok {
			t.Errorf("model-lists registration has no %s row: %v", id, listed)
			continue
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("model-lists row %s openrouter_routing = %v, want %v", id, got, w)
		}
	}
}
