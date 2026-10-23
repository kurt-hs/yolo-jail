package check

// inertrouting_test.go drives sectionPacks, not the report function, so deleting the call in
// packs.go turns it red.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// A WIRE-ID TWIN IS LOOKED UP BY THE ID IT SENDS. The catalog-currency check reads the composed
// table, where a twin's `id` is a client spelling no catalog lists and its `wire_id` is the id a
// catalog can know.
func TestTheCatalogCheckLooksUpATwinByItsWireID(t *testing.T) {
	v, err := jsonx.Decode([]byte(`{"or":{"endpoints":{"openai":{"base_url":"https://openrouter.ai/api/v1"}},
	  "models":{"floor":"x:floor","floor-200k":"x:floor@200k"},
	  "model_options":{"floor-200k":{"wire_id":"x:floor","context_window":"204800"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	ids, total := listedModelIDs(v.(*jsonx.OrderedMap))
	if got := strings.Join(ids["or"], ","); got != "x:floor" || total != 1 {
		t.Errorf("listed ids = %q (total %d), want the one wire id x:floor", got, total)
	}
}

// AN OPENROUTER PIN NOTHING SENDS IS NAMED, NEVER SILENTLY ACCEPTED (the downstream handoff's
// fix 22). `openrouter_routing` is OpenRouter's chat-completions `provider` field: a provider on
// Bedrock, or one whose only OpenAI-protocol endpoint speaks the Responses wire, gets a warning
// naming the alias and the fix. A provider with a chat-completions endpoint, and one a selected
// agent runs on its own client (pi's built-in openrouter), say nothing.
func TestSectionPacksWarnsAboutAnOpenRouterPinNothingSends(t *testing.T) {
	packsFixture(t, `{"packs": ["pi", "openrouter"]}`)
	pin := `{"order": ["streamlake"], "allow_fallbacks": false}`
	v, err := jsonx.Decode([]byte(`{"providers": {
	  "openrouter": {"models": {"floor": {"id": "x:floor", "openrouter_routing": ` + pin + `}}},
	  "chat": {"endpoints": {"openai": {"base_url": "https://chat.example/v1"}},
	    "models": {"floor": {"id": "x:floor", "openrouter_routing": ` + pin + `}}},
	  "resp": {"endpoints": {"openai": {"base_url": "https://resp.example/v1", "wire_api": "openai-responses"}},
	    "models": {"twin": {"id": "x:floor@200k", "openrouter_routing": ` + pin + `}}},
	  "bed": {"platform": "aws-bedrock", "region": "us-east-1",
	    "models": {"gpt": {"id": "global.openai.gpt-6-luna", "openrouter_routing": ` + pin + `}}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	merged, _ := v.(*jsonx.OrderedMap)
	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{}).sectionPacks(r, merged)
	out := buf.String()
	for _, want := range []string{
		`providers.resp.models.twin.openrouter_routing`,
		`providers.bed.models.gpt.openrouter_routing`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the check should name %q:\n%s", want, out)
		}
	}
	for _, quiet := range []string{`providers.openrouter.models.floor.openrouter_routing`,
		`providers.chat.models.floor.openrouter_routing`} {
		if strings.Contains(out, quiet) {
			t.Errorf("the check should say nothing about %q, which is sent:\n%s", quiet, out)
		}
	}
	if r.failed != 0 {
		t.Errorf("an inert pin must WARN, never fail:\n%s", out)
	}
}

// A SHARED PIN NOTHING SENDS IS NAMED TOO (fix 27): a `model_facts` entry's `openrouter_routing`
// reaches every route extending it, so the check composes with the shared facts and warns about
// the Bedrock route that inherits one.
func TestSectionPacksWarnsAboutASharedOpenRouterPinNothingSends(t *testing.T) {
	packsFixture(t, `{"packs": ["pi", "openrouter"]}`)
	v, err := jsonx.Decode([]byte(`{
	  "model_facts": {"gpt": {"openrouter_routing": {"order": ["streamlake"]}}},
	  "providers": {"bed": {"platform": "aws-bedrock", "region": "us-east-1",
	    "models": {"gpt": {"id": "global.openai.gpt-6-luna", "extends": "gpt"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	merged, _ := v.(*jsonx.OrderedMap)
	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{}).sectionPacks(r, merged)
	if out := buf.String(); !strings.Contains(out, `providers.bed.models.gpt.openrouter_routing`) {
		t.Errorf("the check should name the inherited pin:\n%s", out)
	}
}
