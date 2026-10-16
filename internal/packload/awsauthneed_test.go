package packload

import (
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	officialpacks "github.com/mschulkind-oss/yolo-jail/packs"
)

// awsauthneed_test.go pins who brings Amazon Bedrock into a launch
// (docs/design/bedrock-plumbing.md OQ-BR9, ruled 2026-09-29: one `bedrock` pack pulls in the
// provider, and every agent pack that binds Bedrock `needs` it, so `"packs": ["codex"]` alone
// gets `-p bedrock`). The `bedrock` pack in turn NEEDS aws-auth, unconditionally, the way it
// was packs/claude's need when the provider was claude's (the SSO design's build
// step 5, moved to packs/bedrock by bedrock-plumbing.md BR-D15): the credential service belongs
// with the provider it serves, so a codex-only selection gets it too. Selecting it changes
// nothing observable on its own: the pointer is gated on the provider's platform and the
// loophole ships disabled.

// shippedByName materializes the embedded official set, keyed by pack name.
func shippedByName(t *testing.T) map[string]*Pack {
	t.Helper()
	shipped, problems := MaterializeEmbedded(officialpacks.FS, t.TempDir())
	if len(problems) > 0 {
		t.Fatalf("materializing the shipped packs: %v", problems)
	}
	byName := map[string]*Pack{}
	for _, p := range shipped {
		byName[p.Name] = p
	}
	return byName
}

// TestBedrockNeedsAWSAuth reads the shipped bedrock pack's declaration, so it fails if the
// `needs` entry is removed, gains a condition, or names a pack that no longer ships.
func TestBedrockNeedsAWSAuth(t *testing.T) {
	byName := shippedByName(t)
	bedrock := byName["bedrock"]
	if bedrock == nil {
		t.Fatal("no shipped bedrock pack")
	}
	found := false
	for _, n := range bedrock.Decl.DeclaredNeeds() {
		if n.Pack == "aws-auth" {
			found = true
			if len(n.WhenBins) != 0 {
				t.Errorf("bedrock's aws-auth need has when_bins %v; it is unconditional, since "+
					"the provider that consumes it is bedrock's own", n.WhenBins)
			}
		}
	}
	if !found {
		t.Fatalf("packs/bedrock declares no need on aws-auth (needs = %+v) — "+
			"docs/design/bedrock-plumbing.md BR-D15, which moved sso-backed-bedrock.md §12 step 5 "+
			"here", bedrock.Decl.DeclaredNeeds())
	}
}

// bedrockBinders is the agent packs whose derives bind Amazon Bedrock — through the agent's own
// Bedrock client (docs/design/bedrock-plumbing.md §6.2). copilot and oh-omp have no native
// client and reach Bedrock only through the wire bridge, under a via profile (`bedrock-bridge`)
// or as the agents a plain profile's carrier carries (carrier.go, wire-bridge-gateway.md WG-I44),
// and agy has no Bedrock transport at all, so none of the three binds it and none needs the pack.
var bedrockBinders = []string{"claude", "codex", "opencode", "pi"}

// TestAgentPacksDoNotNeedBedrock pins that agent packs do not pull bedrock in through needs:
// Bedrock is an independent subscription/provider pack that a user selects when they use it.
func TestAgentPacksDoNotNeedBedrock(t *testing.T) {
	byName := shippedByName(t)
	for _, agent := range bedrockBinders {
		p := byName[agent]
		if p == nil {
			t.Fatalf("no shipped %s pack", agent)
		}
		for _, n := range p.Decl.DeclaredNeeds() {
			if n.Pack == "bedrock" {
				t.Errorf("packs/%s must not need bedrock — provider packs are selected explicitly", agent)
			}
		}
	}
}

// TestEveryBedrockAgentWithBedrockGetsTheBedrockProfile verifies that when an agent and bedrock
// are selected together, aws-auth joins through bedrock, the bedrock profile resolves, and the
// agent binds the aws-bedrock platform.
func TestEveryBedrockAgentWithBedrockGetsTheBedrockProfile(t *testing.T) {
	byName := shippedByName(t)
	embedded := func(name string) (*Pack, bool) {
		p, ok := byName[name]
		return p, ok
	}
	bedrock := byName["bedrock"]
	if bedrock == nil {
		t.Fatal("no shipped bedrock pack")
	}
	for _, agent := range bedrockBinders {
		t.Run(agent, func(t *testing.T) {
			p := byName[agent]
			if p == nil {
				t.Fatalf("no shipped %s pack", agent)
			}
			added, causes, err := Selection{Embedded: embedded}.Close([]*Pack{p, bedrock})
			if err != nil {
				t.Fatal(err)
			}
			set := append([]*Pack{p, bedrock}, added...)
			var names []string
			for _, q := range set {
				names = append(names, q.Name)
			}
			for _, want := range []string{"bedrock", "aws-auth"} {
				if !slices.Contains(names, want) {
					t.Errorf(`"packs": [%q, "bedrock"] closes over %v, want %s among them`, agent, names, want)
				}
			}
			if !strings.Contains(strings.Join(causes, "\n"), "aws-auth (needed by bedrock)") {
				t.Errorf("the closure's cause lines %q do not say bedrock brought aws-auth:\n%s", causes, strings.Join(causes, "\n"))
			}
			providers, err := ComposeProviders(nil, set)
			if err != nil {
				t.Fatal(err)
			}
			raw, ok := providers.Get("bedrock")
			entry, isMap := raw.(*jsonx.OrderedMap)
			if !ok || !isMap {
				t.Fatalf("the composed table for %v has no bedrock entry: %v", names, providers.Keys())
			}
			if platform, _ := entry.Get("platform"); platform != "aws-bedrock" {
				t.Errorf("the composed bedrock entry's platform = %v, want aws-bedrock", platform)
			}
			resolved, err := ResolveProfiles(set, nil, providers)
			if err != nil {
				t.Fatal(err)
			}
			if got := ProviderFor(resolved, "bedrock"); got != "bedrock" {
				t.Errorf("-p bedrock with [%q, bedrock] resolves to provider %q, want bedrock", agent, got)
			}
			if !AgentBindsPlatform(set, agent, "aws-bedrock") {
				t.Errorf("AgentBindsPlatform(%q, aws-bedrock) = false, want true", agent)
			}
		})
	}
}
