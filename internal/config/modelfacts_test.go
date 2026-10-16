package config

import (
	"strings"
	"testing"
)

// modelfacts_test.go pins the config half of docs/design/model-lists-and-pickers.md OQ-ML5 and
// ML-D10: the top-level `model_facts` map, the facts a model's routes share, and a model entry's
// `extends` naming one of them; and a null fact accepted as a tombstone at each place a fact is
// written.

func modelFactsErrors(t *testing.T, body string) []string {
	t.Helper()
	t.Setenv("YOLO_VERSION", "")
	errs, _ := ValidateConfig(decode(t, body), t.TempDir(), nil)
	var out []string
	for _, e := range errs {
		if strings.HasPrefix(e, "config.model_facts") || strings.HasPrefix(e, "config.providers") {
			out = append(out, e)
		}
	}
	return out
}

func TestModelFactsAndExtendsValidate(t *testing.T) {
	for _, body := range []string{
		`{"model_facts": {"opus-5.5": {"reasoning": true, "input": ["text", "image"], "context_window": 1000000,
		  "max_tokens": 128000, "cost": {"input": 5, "output": 25, "cache_read": 0.5, "cache_write": 6.25}}},
		  "providers": {"openrouter": {"models": {"opus": {"id": "anthropic/claude-opus-5-5", "extends": "opus-5.5"}}}}}`,
		// Tombstones, in the shared facts and in a route's own.
		`{"model_facts": {"m": {"reasoning": null, "cost": null, "description": null}},
		  "providers": {"kilo": {"models": {"x": {"id": "x", "extends": "m", "input": null, "max_tokens": null}}}}}`,
	} {
		if errs := modelFactsErrors(t, body); len(errs) != 0 {
			t.Errorf("valid %s: unexpected errors %v", body, errs)
		}
	}
	for _, tc := range []struct{ body, want string }{
		{`{"model_facts": []}`, "config.model_facts: expected an object"},
		{`{"model_facts": {"m": 5}}`, "config.model_facts.m: expected an object"},
		{`{"model_facts": {"m": {"id": "x"}}}`, "config.model_facts.m.id: unknown key"},
		{`{"model_facts": {"m": {"reasoning": "yes"}}}`, "config.model_facts.m.reasoning: expected a boolean"},
		{`{"providers": {"kilo": {"models": {"x": {"id": "x", "extends": ""}}}}}`, ".models.x.extends: expected the name of a `model_facts` entry"},
		{`{"providers": {"kilo": {"models": {"x": {"id": "x", "extends": "nowhere"}}}}}`,
			`.models.x.extends: names "nowhere", which config.model_facts does not declare`},
	} {
		errs := modelFactsErrors(t, tc.body)
		found := false
		for _, e := range errs {
			if strings.Contains(e, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want an error containing %q, got %v", tc.body, tc.want, errs)
		}
	}
}

// A NULL OVER A FACT SURVIVES THE CONFIG MERGE (ML-D10): the workspace's null over the user's fact
// is kept as a null, so the composition can lower it as a tombstone.
func TestAModelFactNullSurvivesTheConfigMerge(t *testing.T) {
	user := decode(t, `{"providers": {"kilo": {"models": {"x": {"id": "x", "reasoning": true}}}}}`)
	ws := decode(t, `{"providers": {"kilo": {"models": {"x": {"reasoning": null}}}}}`)
	merged := MergeConfig(user, ws)
	providers, _ := merged.Get("providers")
	x, _ := asMap(providers)
	kilo, _ := x.Get("kilo")
	km, _ := asMap(kilo)
	models, _ := km.Get("models")
	mm, _ := asMap(models)
	entry, _ := mm.Get("x")
	em, _ := asMap(entry)
	v, present := em.Get("reasoning")
	if !present || v != nil {
		t.Errorf("merged reasoning = %v (present %v), want a kept null", v, present)
	}
}
