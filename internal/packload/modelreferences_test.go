package packload

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// A pattern's `*` matches any run, slashes included, and nothing else is special; a pointer's `*`
// token fans out over every member (MM-D45).
func TestModelReferencePatternsAndPointers(t *testing.T) {
	for _, tc := range []struct {
		pattern, id string
		want        bool
	}{
		{"zai/*", "zai/glm-5.3", true},
		{"kilo/*", "kilo/deepseek/deepseek-v4.1-flash", true},
		{"zai/*", "openrouter/glm-5.3", false},
		{"openai-codex/gpt-6[1m]", "openai-codex/gpt-6[1m]", true},
		{"openai-codex/gpt-6[1m]", "openai-codex/gpt-61", false},
		{"a.b/c", "aXb/c", false},
	} {
		if got := MatchesAny(tc.id, []string{tc.pattern}); got != tc.want {
			t.Errorf("MatchesAny(%q, %q) = %v, want %v", tc.id, tc.pattern, got, tc.want)
		}
	}
	doc := map[string]any{"subagents": map[string]any{
		"defaultModel":   "zai/glm-5.3",
		"agentOverrides": map[string]any{"worker": map[string]any{"model": "zai/glm-9"}},
		"modelScope":     map[string]any{"allow": []any{"zai/*"}},
	}}
	refs := &packdecl.ModelReferences{Surface: "pi/settings",
		Models:   []string{"/subagents/defaultModel", "/subagents/agentOverrides/*/model"},
		Patterns: []string{"/subagents/modelScope/allow"}}
	got := ModelReferenceValuesIn(doc, refs, "f ")
	want := []ModelReferenceValue{
		{Where: "f /subagents/defaultModel", Value: "zai/glm-5.3"},
		{Where: "f /subagents/agentOverrides/worker/model", Value: "zai/glm-9"},
		{Where: "f /subagents/modelScope/allow/0", Value: "zai/*", Pattern: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ModelReferenceValuesIn() = %+v, want %+v", got, want)
	}
}

// The registry answers known, unknown with the nearest ids, or unasked when a provider's catalog
// could not be read.
func TestModelRegistryVerdicts(t *testing.T) {
	reg := &ModelRegistry{byProvider: map[string]*registryProvider{
		"zai":  {ids: map[string]bool{"glm-5.3": true, "glm-4.6": true}},
		"open": {ids: map[string]bool{}, unasked: true},
	}}
	if v, _ := reg.Model("zai/glm-5.3"); v != ModelKnown {
		t.Errorf("a listed id is known, got %v", v)
	}
	if v, near := reg.Model("zai/glm-5.2"); v != ModelUnknown || len(near) == 0 || near[0] != "zai/glm-5.3" {
		t.Errorf("an unlisted id is unknown with the nearest ids, got %v %v", v, near)
	}
	if v, _ := reg.Model("open/anything"); v != ModelUnasked {
		t.Errorf("an unread catalog is unasked, got %v", v)
	}
	if v, _ := reg.Model("nobody/x"); v != ModelUnknown {
		t.Errorf("a provider the registry lacks is unknown, got %v", v)
	}
	if v, _ := reg.Pattern("zai/glm-*"); v != ModelKnown {
		t.Errorf("a matching pattern is known, got %v", v)
	}
	if v, _ := reg.Pattern("zai/qwen*"); v != ModelUnknown {
		t.Errorf("a pattern matching nothing is unknown, got %v", v)
	}
	if v, _ := reg.Pattern("*/qwen*"); v != ModelUnasked {
		t.Errorf("a cross-provider pattern over an unread catalog is unasked, got %v", v)
	}
}
