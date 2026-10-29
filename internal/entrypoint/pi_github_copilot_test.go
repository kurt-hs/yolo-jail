package entrypoint

import (
	"slices"
	"testing"
)

// pi_github_copilot_test.go pins pi on its own GitHub Copilot provider with NO user declaration:
// packs/github-copilot ships the `github-copilot` provider and the `github-copilot` profile over
// it, the way packs/openai-auth ships `openai-codex` and `codex`, so selecting pi and that pack,
// with no copilot, is enough (BI-D14). pi has the
// provider built in (`built_in_providers`), so yolo writes it no models.json row and pi runs it on
// its own client and its own `/login`. The case the shipped profile exists for is the MIXED set,
// `"profile": {"pi": ["bedrock", "github-copilot"]}`: Bedrock's scoped list and pi-subagents'
// scope must not keep pi off Copilot's models. Over the real embedded packs (pi, github-copilot, bedrock and the
// needs closure), through the boot render, with no user provider or profile.

func TestPiRunsOnItsOwnGitHubCopilotWithNoDeclaration(t *testing.T) {
	providersJSON, wire := bedrockTables(t, "pi", "", nil, "github-copilot")
	r := newPioencodeRender(t, providersJSON)
	r.wireProfiles(wire)
	r.render(t, `{"pi":"github-copilot"}`)

	rows, _ := r.piModels(t)["providers"].(map[string]any)
	if rows["github-copilot"] != nil {
		t.Errorf("pi's own github-copilot got a models.json row, which would shadow pi's client: %v", rows["github-copilot"])
	}
	s := r.piSettings(t)
	if s["defaultProvider"] != "github-copilot" {
		t.Errorf("defaultProvider = %v, want pi's own github-copilot", s["defaultProvider"])
	}
	if got := strs(s["enabledModels"]); !slices.Equal(got, []string{"github-copilot/**"}) {
		t.Errorf("enabledModels = %v, want pi's whole github-copilot list", got)
	}
}

// The mixed set, with Bedrock's list both pi's own (none supplied) and narrowed by a pack (a
// supplied list, which scopes pi to amazon-bedrock ids): either way Copilot's models stay in
// pi's scoped picker and in pi-subagents' scope, and the session starts on Bedrock, the primary.
func TestPiOnBedrockAndGitHubCopilotKeepsCopilotsModelsInScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		list bool
	}{{"bedrock's own list", false}, {"a supplied bedrock list", true}} {
		t.Run(tc.name, func(t *testing.T) {
			tables := bedrockTables
			if tc.list {
				tables = bedrockListTables
			}
			providersJSON, wire := tables(t, "pi", "", nil, "github-copilot")
			r := newPioencodeRender(t, providersJSON)
			r.wireProfiles(wire)
			r.render(t, `{"pi":["bedrock","github-copilot"]}`)

			s := r.piSettings(t)
			if s["defaultProvider"] != "amazon-bedrock" {
				t.Errorf("a fresh session starts on the primary: defaultProvider = %v", s["defaultProvider"])
			}
			enabled := strs(s["enabledModels"])
			if !slices.Contains(enabled, "github-copilot/**") {
				t.Errorf("enabledModels %v keeps pi off Copilot's models", enabled)
			}
			sub, _ := s["subagents"].(map[string]any)
			scope, _ := sub["modelScope"].(map[string]any)
			allow := strs(scope["allow"])
			if !slices.Contains(allow, "github-copilot/*") {
				t.Errorf("pi-subagents' allow %v keeps a child off Copilot's models", allow)
			}
			if len(allow) < 2 {
				t.Errorf("pi-subagents' allow %v lost the Bedrock entry", allow)
			}
			rows, _ := r.piModels(t)["providers"].(map[string]any)
			if rows["github-copilot"] != nil {
				t.Errorf("pi's own github-copilot got a models.json row: %v", rows["github-copilot"])
			}
		})
	}
}
