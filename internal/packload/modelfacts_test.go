package packload

import (
	"strings"
	"testing"
)

// modelfacts_test.go pins the composition half of docs/design/model-lists-and-pickers.md OQ-ML4,
// OQ-ML5 and ML-D10: a fact written as null is a TOMBSTONE that reaches the derive as a null in
// the composed `model_options`, where leaving it out inherits; and a model's routes may share the
// facts the user's top-level `model_facts` declares once, each route's own value winning.

// A USER'S NULL OVER A PACK'S FACT REMOVES IT (ML-D10). Before, the null was skipped at the
// lowering, so the pack's fact survived and the derive could not tell the user had removed it.
func TestAUserNullOverAPackFactIsCarriedAsATombstone(t *testing.T) {
	pack := &Pack{Name: "zai", Decl: declFrom(t, `{"contributes":[{"kind":"provider","name":"zai",
	 "models":{"glm-5.3":"glm-5.3"},
	 "model_options":{"glm-5.3":{"reasoning":"true","input":"text,image","context_window":"200000"}}}]}`)}
	user := userProviders(t, `{"zai":{"models":{"glm-5.3":{"id":"glm-5.3","reasoning":null,"cost":null}}}}`)
	got := dump(t, compose(t, user, []*Pack{pack}))
	want := `"glm-5.3": {"context_window": "200000", "input": "text,image", "reasoning": null, ` +
		`"cost_input": null, "cost_output": null, "cost_cache_read": null, "cost_cache_write": null}`
	if !strings.Contains(got, want) {
		t.Errorf("the user's nulls must reach the table as tombstones over the pack's facts:\n got %s\nwant a substring %s", got, want)
	}
}

// A PACK'S OWN NULL IS A TOMBSTONE TOO (ML-D10: "a pack's model_options.<alias>").
func TestAPackNullFactIsCarriedAsATombstone(t *testing.T) {
	pack := &Pack{Name: "zai", Decl: declFrom(t, `{"contributes":[{"kind":"provider","name":"zai",
	 "models":{"glm-5.3":"glm-5.3"},"model_options":{"glm-5.3":{"reasoning":null,"input":"text"}}}]}`)}
	got := dump(t, compose(t, nil, []*Pack{pack}))
	if !strings.Contains(got, `"glm-5.3": {"input": "text", "reasoning": null}`) {
		t.Errorf("a pack's null fact must be carried as a null, got %s", got)
	}
}

// SHARED FACTS UNDER EACH ROUTE'S OWN (OQ-ML5): the route's value wins, then the shared facts,
// and a tombstone works at each layer. The shared facts also ride the entry as `model_shared`, so
// a derive writing the shared form can state them once.
func TestRoutesInheritSharedFactsAndOverrideThem(t *testing.T) {
	user := userProviders(t, `{
	  "bedrock":{"models":{"opus":{"id":"global.anthropic.claude-opus-5-5","extends":"opus-5.5","context_window":200000}}},
	  "openrouter":{"models":{"opus":{"id":"anthropic/claude-opus-5-5","extends":"opus-5.5","reasoning":null}}}}`)
	shared := userProviders(t, `{"opus-5.5":{"reasoning":true,"input":["text","image"],"context_window":1000000,
	  "max_tokens":128000,"description":null}}`)
	got, err := ComposeProviders(user, nil, WithModelFacts(shared))
	if err != nil {
		t.Fatal(err)
	}
	s := dump(t, got)
	for _, want := range []string{
		// Bedrock: its own window over the shared one, everything else shared.
		`"bedrock": {"models": {"opus": "global.anthropic.claude-opus-5-5"}, "model_options": {"opus": ` +
			`{"reasoning": "true", "input": "text,image", "context_window": "200000", "max_tokens": "128000", ` +
			`"description": null, "extends": "opus-5.5"}}, "model_shared": {"opus-5.5": {"reasoning": "true", ` +
			`"input": "text,image", "context_window": "1000000", "max_tokens": "128000", "description": null}}}`,
		// OpenRouter: its own tombstone over the shared reasoning flag.
		`"model_options": {"opus": {"reasoning": null, "input": "text,image", "context_window": "1000000", ` +
			`"max_tokens": "128000", "description": null, "extends": "opus-5.5"}}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("composed table:\n got %s\nwant a substring %s", s, want)
		}
	}
}

// AN `extends` WITH NO SHARED FACTS HANDED IN keeps the route's own facts and its pointer, and
// writes no `model_shared`: config validation is where a pointer at nothing is refused.
func TestAnExtendsWithNoSharedFactsKeepsTheRoutesOwn(t *testing.T) {
	user := userProviders(t, `{"openrouter":{"models":{"opus":{"id":"anthropic/claude-opus-5-5","extends":"opus-5.5","max_tokens":64000}}}}`)
	s := dump(t, compose(t, user, nil))
	if !strings.Contains(s, `"model_options": {"opus": {"max_tokens": "64000", "extends": "opus-5.5"}}`) || strings.Contains(s, "model_shared") {
		t.Errorf("composed table = %s", s)
	}
}

// A SHARED OPENROUTER PIN LANDS IN EACH EXTENDING ROUTE'S `model_routing` (fix 27), and a null
// on the route is a TOMBSTONE (ML-D10): that route sends no pin, the shared one included.
func TestASharedRoutingPinReachesEachRouteAndARouteNullDropsIt(t *testing.T) {
	user := userProviders(t, `{"openrouter":{"models":{
	  "a":{"id":"x/y:floor","extends":"xy"},
	  "b":{"id":"x/y:floor@200k","extends":"xy","openrouter_routing":null}}}}`)
	shared := userProviders(t, `{"xy":{"openrouter_routing":{"order":["streamlake"]}}}`)
	got, err := ComposeProviders(user, nil, WithModelFacts(shared))
	if err != nil {
		t.Fatal(err)
	}
	s := dump(t, got)
	if !strings.Contains(s, `"model_routing": {"a": {"order": ["streamlake"]}}`) {
		t.Errorf("route a must inherit the shared pin and route b's null must drop it, got %s", s)
	}
	if strings.Contains(s, `"openrouter_routing"`) {
		t.Errorf("the pin must not leak into the flat facts, got %s", s)
	}
}
