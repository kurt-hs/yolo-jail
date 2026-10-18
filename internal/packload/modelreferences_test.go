package packload

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/luahook"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
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
		{"OpenAI-Codex/GPT-6.1-SOL[1M]", "openai-codex/gpt-6.1-sol[1m]", true},
		{"openai-codex/*[thinking]", "openai-codex/gpt-6.1-sol[thinking]", true},
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

// A rendered model surface's ids join the registry under its declared provider, or the member
// its pointer's first `*` stands for; matching ignores case; a value naming no provider is not a
// reference.
func TestModelRegistryReadsRenderedSurfaces(t *testing.T) {
	reg := &ModelRegistry{byProvider: map[string]*registryProvider{}}
	reg.AddRendered(map[string]any{"providers": map[string]any{
		"kilo": map[string]any{"models": []any{map[string]any{"id": "deepseek/deepseek-v4.1-flash"}}},
	}}, packdecl.ModelRegistrySource{Surface: "pi/models", IDs: "/providers/*/models/*/id"})
	reg.AddRendered(map[string]any{"models": []any{map[string]any{"id": "gpt-6.1-sol[1m]"}}},
		packdecl.ModelRegistrySource{Surface: "pi/codex-models", IDs: "/models/*/id", Provider: "openai-codex"})
	for _, q := range []string{"kilo/deepseek/deepseek-v4.1-flash", "openai-codex/gpt-6.1-sol[1m]", "OpenAI-Codex/GPT-6.1-sol[1m]"} {
		if v, _ := reg.Pattern(q); v != ModelKnown {
			t.Errorf("Pattern(%q) = %v, want known", q, v)
		}
	}
	if v, _ := reg.Model("openai-codex/GPT-6.1-SOL[1m]"); v != ModelKnown {
		t.Errorf("a model id matches ignoring case, got %v", v)
	}
	doc := map[string]any{"allow": []any{"inherit", "zai/*"}}
	if got := ModelReferenceValuesIn(doc, &packdecl.ModelReferences{Patterns: []string{"/allow"}}, ""); len(got) != 1 {
		t.Errorf("a value naming no provider is not a reference, got %+v", got)
	}
	refs := &packdecl.ModelReferences{Scope: "/allow", Selection: &packdecl.ModelSelectionKeys{Provider: "/p", Model: "/m"}}
	if _, _, outside := SelectionOutsideScope(map[string]any{"p": "x", "m": "y", "allow": []any{"inherit"}}, refs); outside {
		t.Error("a scope entry naming no provider may admit anything, so it is not judged")
	}
	if _, _, outside := SelectionOutsideScope(map[string]any{"p": "x", "m": "y", "allow": []any{"zai/*"}}, refs); !outside {
		t.Error("x/y is outside zai/*")
	}
}

// Missing and malformed provider entries are not a reason for a check panic. With a subagent
// declaration and no usable list, the host reports the value as registry-resolvable input instead.
func TestProfileSubagentValuesHandlesNilProviderTablesAndEntries(t *testing.T) {
	owner := &Pack{Name: "pi", Decl: declFrom(t, `{"model_list_check":{"pi":{"references":
	  {"surface":"pi/settings","profile_subagents":true}}},"contributes":[{"kind":"program","bin":"pi",
	  "via":"npm","package":"@acme/pi"}]}`)}
	withNilEntry := jsonx.NewOrderedMap()
	withNilEntry.Set("missing", nil)
	for _, tc := range []struct {
		name      string
		providers *jsonx.OrderedMap
		subagents luahook.Subagents
		want      int
	}{
		{"nil table with declaration", nil, luahook.Subagents{Default: "model-id"}, 1},
		{"nil entry with declaration", withNilEntry, luahook.Subagents{Default: "model-id"}, 1},
		{"nil entry without declaration", withNilEntry, luahook.Subagents{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ProfileSubagentValues([]*Pack{owner}, tc.providers, map[string]string{"pi": "active"}, nil,
				map[string]ResolvedProfile{"active": {Provider: "missing", Subagents: tc.subagents}}, "pi")
			if len(got.Values) != tc.want || len(got.Findings) != 0 {
				t.Fatalf("ProfileSubagentValues = %+v, want %d pass-through values and no findings", got, tc.want)
			}
		})
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
