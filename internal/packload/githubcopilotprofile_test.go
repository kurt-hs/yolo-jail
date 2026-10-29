package packload

import (
	"slices"
	"strings"
	"testing"
)

// githubcopilotprofile_test.go pins the `github-copilot` provider and profile packs/pi ships, the
// way packs/openai-auth ships `openai-codex` and `codex`: selecting pi alone declares the profile,
// it resolves to pi's own built-in client at both notches, and an agent with no GitHub Copilot
// client of its own is told the selection reaches nothing for it, by the platform rule every
// endpoint-less provider takes (profileReach), never silently left on its own login.

func TestSelectingPiAloneDeclaresTheGitHubCopilotProfile(t *testing.T) {
	pi := embeddedNamed(t, "pi")
	if names := DeclaredProfileNames(pi, nil); !slices.Contains(names, "github-copilot") {
		t.Fatalf("declared profiles with pi alone = %v, want github-copilot among them", names)
	}
	for _, n := range notches {
		if _, err := agentEnvAt(t, n, pi, nil, nil, "pi", "github-copilot"); err != nil {
			t.Errorf("%s: pi on its own github-copilot with no declaration refused: %v", n.name, err)
		}
	}
}

func TestABareGitHubCopilotProfileReachesOnlyTheAgentsThatHaveIt(t *testing.T) {
	table := map[string]string{}
	for _, a := range []string{"pi", "opencode", "oh-omp", "claude", "codex", "copilot"} {
		table[a] = "github-copilot"
	}
	d := disclose(t, []string{"pi", "opencode", "omp", "claude", "codex", "copilot", "wire-bridge"}, table, nil)
	if strings.Join(d.Declared, ",") != "pi" {
		t.Errorf("declared by %v, want the pi pack alone", d.Declared)
	}
	for _, agent := range []string{"pi", "opencode", "oh-omp"} {
		r := reachOf(t, d, agent)
		if r.Route != `through its own "github-copilot" client, with its own model list` || r.Unreached != "" {
			t.Errorf("%s: %+v, want its own github-copilot client", agent, r)
		}
	}
	for _, agent := range []string{"claude", "codex", "copilot"} {
		r := reachOf(t, d, agent)
		if r.Route != "" || !strings.Contains(r.Unreached, `only platform "github-copilot"`) ||
			!strings.Contains(r.Unreached, "`-p "+agent+"=<name>`") {
			t.Errorf("%s: %+v, want the selection to reach nothing for it, naming -p %s=<name>", agent, r, agent)
		}
	}
}

// THE MIXED SET'S PROVIDER POLICY: pi on ["bedrock", "github-copilot"], every table composed from
// the shipped packs alone, allows both of pi's own providers, so yolo's provider-policy extension
// does not block Copilot's requests.
func TestPiOnBedrockAndGitHubCopilotAllowsBothProviders(t *testing.T) {
	packs := embeddedNamed(t, "pi", "bedrock", "aws-auth")
	providers, resolved, _ := launchSelection(t, packs, nil, nil, map[string]string{"pi": "bedrock"})
	scope, err := ScopeCredentials(ScopeInput{Packs: packs, Providers: providers, Resolved: resolved,
		Profiles: map[string]string{"pi": "bedrock"},
		Sets:     map[string][]string{"pi": {"bedrock", "github-copilot"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":1,"mode":"allowlist","allowedProviderIds":["amazon-bedrock","github-copilot"],"profiles":["bedrock","github-copilot"]}`
	for _, v := range scope.Agent("pi").Shape {
		if v.Key == "YOLO_PI_PROVIDER_POLICY" {
			if v.Value != want {
				t.Errorf("YOLO_PI_PROVIDER_POLICY = %s, want %s", v.Value, want)
			}
			return
		}
	}
	t.Errorf("pi on a profile set got no provider policy")
}
