package entrypoint

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// A MODELS.JSON ROW CARRIES ITS OPENROUTER PIN (the downstream handoff's fix 22). A provider pi does
// not ship is written to models.json, and a row's `openrouter_routing` was dropped there: only the
// model-lists registration lowered it. The boot render now writes it as the row's
// `compat.openRouterRouting`, which pi merges over the provider's compat and sends as the request's
// `provider` field, so the twin is pinned like its sibling on pi's own openrouter. A row with no
// pin carries no compat.
func TestPiWritesAModelsJSONRowsOpenRouterPinAsCompat(t *testing.T) {
	decoded, err := jsonx.Decode([]byte(`{"or200":{
	  "endpoints":{"openai":{"base_url":"https://openrouter.ai/api/v1","wire_api":"openai-chat-completions"}},
	  "api_key_env_name":"OPENROUTER_API_KEY",
	  "models":{
	    "floor-200k":{"id":"deepseek/deepseek-v4.1-flash:floor","context_window":204800,
	      "openrouter_routing":{"order":["streamlake","morph"],"allow_fallbacks":false}},
	    "plain":{"id":"deepseek/deepseek-v4.1-flash","context_window":204800}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	user, _ := decoded.(*jsonx.OrderedMap)
	providers, err := packload.ComposeProviders(user, testPacksForAgent(t, "pi"))
	if err != nil {
		t.Fatal(err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(`{"or200":{"provider":"or200"}}`)
	r.render(t, `{"pi":"or200"}`)
	m := r.piModels(t)
	row := piRow(t, m, "or200", "deepseek/deepseek-v4.1-flash:floor")
	compat, _ := row["compat"].(map[string]any)
	want := map[string]any{"order": []any{"streamlake", "morph"}, "allow_fallbacks": false}
	if !reflect.DeepEqual(compat["openRouterRouting"], want) {
		t.Errorf("pinned row = %v, want compat.openRouterRouting %v", row, want)
	}
	if _, has := row["openrouter_routing"]; has {
		t.Errorf("pinned row = %v carries the yolo spelling, which pi reads nothing from", row)
	}
	if plain := piRow(t, m, "or200", "deepseek/deepseek-v4.1-flash"); plain["compat"] != nil {
		t.Errorf("unpinned row = %v, want no compat", plain)
	}
}
