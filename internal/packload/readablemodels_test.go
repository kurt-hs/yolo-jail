package packload

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// readablemodels_test.go pins a model said once reaching the composed provider table
// (docs/design/readable-config.md RC-D29, docs/design/pack-manifests.md §4,
// docs/design/config-user.md §8 C7): the readable rows, `every_model`, `twin_of`, `compact_at`
// and the catalog's facts, lowered into the alias map and flat facts every derive reads.

func readablePack(t *testing.T, body string) *Pack {
	t.Helper()
	m, problems := packdecl.DecodeTOML([]byte("name = \"vendor\"\n" + body))
	if len(problems) != 0 {
		t.Fatalf("fixture pack.toml invalid: %v", problems)
	}
	return &Pack{Name: "vendor", Decl: m}
}

func composeReadable(t *testing.T, user string, packs []*Pack, opts ...ComposeOption) string {
	t.Helper()
	var providers = userProviders(t, `{}`)
	if user != "" {
		providers = userProviders(t, user)
	}
	got, err := ComposeProviders(providers, packs, opts...)
	if err != nil {
		t.Fatalf("composing: %v", err)
	}
	return dump(t, got)
}

func wantSubstrings(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("composed table:\n got %s\nwant a substring %s", got, want)
		}
	}
}

// A USER'S READABLE ROWS: each is keyed by the id it sends, takes its provider's `every_model`
// under its own facts, and leaves no `every_model` or readable field in the table a derive reads.
// A twin takes its base whole and sends the base's id; `compact_at` is the window each derive
// compacts at, the flat `context_window`.
func TestReadableRowsLowerIntoTheAliasMapAndFlatFacts(t *testing.T) {
	got := composeReadable(t, `{"bedrock":{
	  "every_model":{"context":200000,"reasoning":true,"accepts":["text","image"]},
	  "models":{
	    "global.anthropic.claude-opus-5-5":{"name":"Opus 5.5","context":1000000,"max_tokens":128000,
	      "price":{"input":5,"output":25,"cache_read":0.5,"cache_write":6.25}},
	    "opus-200k":{"twin_of":"global.anthropic.claude-opus-5-5","compact_at":200000}}}}`, nil)
	wantSubstrings(t, got,
		`"global.anthropic.claude-opus-5-5": "global.anthropic.claude-opus-5-5"`,
		`"opus-200k": "opus-200k"`,
		// The row's own window over every_model's; every_model's reasoning and input under it.
		`"name": "Opus 5.5"`,
		`"context_window": "1000000"`,
		`"reasoning": "true"`,
		`"input": "text,image"`,
		`"cost_input": "5"`,
		// The twin sends its base's id, at the window it compacts at.
		`"wire_id": "global.anthropic.claude-opus-5-5"`,
		`"context_window": "200000"`,
	)
	if strings.Contains(got, "every_model") || strings.Contains(got, "twin_of") || strings.Contains(got, "compact_at") {
		t.Errorf("a readable field reached the derives' table: %s", got)
	}
}

// `every_model` UNDER A LEGACY ALIAS (RC-D29): it fills only what the alias, its pack and its
// shared facts leave unsaid, and a tombstone counts as said.
func TestEveryModelFillsOnlyWhatALegacyAliasLeavesUnsaid(t *testing.T) {
	pack := &Pack{Name: "zai", Decl: declFrom(t, `{"contributes":[{"kind":"provider","name":"zai",
	 "models":{"glm":"glm-5.3","air":"glm-5.3-air"},
	 "model_options":{"glm":{"context_window":"128000"}}}]}`)}
	got := composeReadable(t, `{"zai":{"every_model":{"context":200000,"max_tokens":32000,"reasoning":true},
	  "models":{"air":{"id":"glm-5.3-air","reasoning":null}}}}`, []*Pack{pack})
	wantSubstrings(t, got,
		`"glm": "glm-5.3"`, `"air": "glm-5.3-air"`,
		// The pack's window stays; every_model fills the rest.
		`"glm": {"context_window": "128000", "max_tokens": "32000", "reasoning": "true"}`,
		// The user's tombstone stays a tombstone.
		`"air": {"reasoning": null, "context_window": "200000", "max_tokens": "32000"}`,
	)
}

// A PACK'S `pack.toml` ROWS AND `every_model` compose with no user layer, and a user's price over
// a pack row's merges per rate.
func TestPackReadableRowsComposeAndAUserPriceMergesPerRate(t *testing.T) {
	pack := readablePack(t, `
[providers.acme]
api_key_env_name = "ACME_KEY"
[providers.acme.endpoints.openai]
base_url = "https://acme.example/v1"
[providers.acme.every_model]
max_tokens = 64000
[providers.acme.models."acme/one"]
name = "One"
price = { input = 1, output = 2, cache_read = 0.1, cache_write = 1.5 }
`)
	alone := composeReadable(t, "", []*Pack{pack})
	wantSubstrings(t, alone, `"acme/one": "acme/one"`, `"name": "One"`, `"max_tokens": "64000"`, `"cost_output": "2"`)
	merged := composeReadable(t, `{"acme":{"models":{"acme/one":{"price":{"output":3}}}}}`, []*Pack{pack})
	wantSubstrings(t, merged, `"cost_input": "1"`, `"cost_output": "3"`, `"name": "One"`)
}

// C7: A ROW NAMED WITHOUT FACTS takes the catalog's, under every_model and its own; a legacy alias
// never reads the catalog, and a table with no readable row never loads it.
func TestACatalogFillsAReadableRowAndOnlyAReadableRow(t *testing.T) {
	loads := 0
	catalog := WithModelCatalog(func() (map[string]packdecl.ProviderModel, error) {
		loads++
		reasoning := true
		return map[string]packdecl.ProviderModel{
			"zai.glm-5": {Name: "GLM 5", Context: 202752, MaxTokens: 131072, Reasoning: &reasoning},
			"legacy-id": {Name: "Not for legacy", Context: 1},
		}, nil
	})
	got := composeReadable(t, `{"bedrock":{"every_model":{"max_tokens":8192},
	  "models":{"zai.glm-5":{"description":"mine"},"old":"legacy-id"}}}`, nil, catalog)
	wantSubstrings(t, got, `"name": "GLM 5"`, `"context_window": "202752"`, `"max_tokens": "8192"`, `"description": "mine"`,
		`"old": {"max_tokens": "8192"}`)
	if strings.Contains(got, "Not for legacy") {
		t.Errorf("a legacy alias took catalog facts: %s", got)
	}
	if loads != 1 {
		t.Errorf("catalog loaded %d times, want once", loads)
	}
	_ = composeReadable(t, `{"bedrock":{"models":{"old":"legacy-id"}}}`, nil, catalog)
	if loads != 1 {
		t.Errorf("a table with no readable row read the catalog")
	}
}

// A READABLE ROW OVER A PACK'S ALIAS FOR ANOTHER ID refuses: the row would send the alias name as
// the model id (RC-D29), the rewrite a legacy row's required `id` prevents.
func TestAReadableRowOverAPackAliasForAnotherIDRefuses(t *testing.T) {
	pack := &Pack{Name: "kilo", Decl: declFrom(t, `{"contributes":[{"kind":"provider","name":"kilo",
	 "models":{"default":"qwen-3.8-27b"}}]}`)}
	_, err := ComposeProviders(userProviders(t, `{"kilo":{"models":{"default":{"reasoning":true}}}}`), []*Pack{pack})
	if err == nil || !strings.Contains(err.Error(), `write `+"`id = \"qwen-3.8-27b\"`") {
		t.Fatalf("want a refusal naming the id to write, got %v", err)
	}
}

// FACTS NO DERIVE READS YET are a note, not a refusal: the model still launches.
func TestUnreadReadableFactsAreANoteNotARefusal(t *testing.T) {
	var notes []string
	got := composeReadable(t, `{"acme":{"models":{"acme/x":{"api":"openai-responses","thinking":["low"]}}}}`, nil,
		WithModelNotes(func(s string) { notes = append(notes, s) }))
	wantSubstrings(t, got, `"acme/x": "acme/x"`)
	if len(notes) != 1 || !strings.Contains(notes[0], "providers.acme.models.acme/x: api, thinking") {
		t.Errorf("notes = %q", notes)
	}
}
