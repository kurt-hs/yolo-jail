package packload

import (
	"slices"
	"strings"
	"testing"
)

// githubcopilotprofile_test.go pins the `github-copilot` provider and profile packs/github-copilot
// ships, the way packs/openai-auth ships `openai-codex` and `codex` (BI-D14, the maintainer's
// ruling of 2026-10-29: "a pack that copilot brings in by default, but that you could include
// without copilot to use it directly in places like pi"). packs/copilot needs it, so selecting the
// Copilot CLI brings it; selected without copilot, beside pi, it declares the profile pi runs on
// its own built-in client; pi alone no longer declares it. The Copilot CLI, which needs the pack,
// runs the profile on its own GitHub login, and an agent with no GitHub Copilot client of its own
// is told the selection reaches nothing for it, by the platform rule every endpoint-less provider
// takes (profileReach), never silently left on its own login.

func embeddedLookup(name string) (*Pack, bool) {
	for _, p := range Embedded() {
		if p.Name == name {
			return p, true
		}
	}
	return nil, false
}

func TestSelectingCopilotBringsTheGitHubCopilotPack(t *testing.T) {
	added, causes, err := ResolveNeeds(embeddedNamed(t, "copilot"), embeddedLookup)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range added {
		names = append(names, p.Name)
	}
	if !slices.Contains(names, "github-copilot") {
		t.Fatalf("copilot's closure added %v, want github-copilot among them", names)
	}
	if !slices.Contains(causes, "+ github-copilot (needed by copilot)") {
		t.Errorf("causes %v do not disclose the join", causes)
	}
	// pi has the provider built in but does not bring the pack: the ruling makes it something
	// copilot brings and anyone else selects.
	added, _, err = ResolveNeeds(embeddedNamed(t, "pi"), embeddedLookup)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range added {
		if p.Name == "github-copilot" {
			t.Errorf("pi's closure brought github-copilot; only copilot needs it")
		}
	}
}

func TestPiAloneNoLongerDeclaresTheGitHubCopilotProfile(t *testing.T) {
	if names := DeclaredProfileNames(embeddedNamed(t, "pi"), nil); slices.Contains(names, "github-copilot") {
		t.Errorf("declared profiles with pi alone = %v, want no github-copilot: the github-copilot pack declares it", names)
	}
	if got := ShippedProfileDeclarers("github-copilot", Embedded()); !slices.Equal(got, []string{"github-copilot"}) {
		t.Errorf("shipped packs declaring github-copilot = %v, want the github-copilot pack alone", got)
	}
}

func TestTheGitHubCopilotPackDeclaresTheProfileWithoutCopilot(t *testing.T) {
	packs := embeddedNamed(t, "pi", "github-copilot")
	if names := DeclaredProfileNames(packs, nil); !slices.Contains(names, "github-copilot") {
		t.Fatalf("declared profiles with pi and github-copilot = %v, want github-copilot among them", names)
	}
	for _, n := range notches {
		if _, err := agentEnvAt(t, n, packs, nil, nil, "pi", "github-copilot"); err != nil {
			t.Errorf("%s: pi on its own github-copilot refused: %v", n.name, err)
		}
	}
}

// THE UNDECLARED REFUSAL NAMES THE PACK THAT WOULD DECLARE IT (the Happy Path): with pi alone,
// `-p pi=github-copilot` is refused, and the message says to add the github-copilot pack.
func TestAnUndeclaredShippedProfileNamesThePackThatShipsIt(t *testing.T) {
	declared := DeclaredProfileNames(embeddedNamed(t, "pi"), nil)
	msg := UndeclaredProfileMessage("github-copilot", declared, ShippedProfileDeclarers("github-copilot", Embedded()))
	if !strings.Contains(msg, `no profile named "github-copilot" is declared`) ||
		!strings.Contains(msg, `add "github-copilot" to your config's `+"`packs`") {
		t.Errorf("message %q does not name adding the github-copilot pack", msg)
	}
	if typo := UndeclaredProfileMessage("zai-fst", declared, ShippedProfileDeclarers("zai-fst", Embedded())); strings.Contains(typo, "packs`") {
		t.Errorf("a name no shipped pack declares names a pack: %q", typo)
	}
}

func TestABareGitHubCopilotProfileReachesOnlyTheAgentsThatHaveIt(t *testing.T) {
	table := map[string]string{}
	for _, a := range []string{"pi", "opencode", "oh-omp", "claude", "codex", "copilot"} {
		table[a] = "github-copilot"
	}
	d := disclose(t, []string{"pi", "opencode", "omp", "claude", "codex", "copilot", "github-copilot",
		"wire-bridge"}, table, nil)
	if strings.Join(d.Declared, ",") != "github-copilot" {
		t.Errorf("declared by %v, want the github-copilot pack alone", d.Declared)
	}
	for _, agent := range []string{"pi", "opencode", "oh-omp"} {
		r := reachOf(t, d, agent)
		if r.Route != `through its own "github-copilot" client, with its own model list` || r.Unreached != "" {
			t.Errorf("%s: %+v, want its own github-copilot client", agent, r)
		}
	}
	// The Copilot CLI's own client IS GitHub Copilot: its pack needs the github-copilot pack, which
	// ships a provider of the platform, so the profile runs on its own login, with no warning.
	if r := reachOf(t, d, "copilot"); r.Route != `through copilot's own "github-copilot" client` ||
		r.Unreached != "" || len(r.Warnings) != 0 {
		t.Errorf("copilot: %+v, want its own github-copilot client and no warning", r)
	}
	for _, agent := range []string{"claude", "codex"} {
		r := reachOf(t, d, agent)
		if r.Route != "" || !strings.Contains(r.Unreached, `only platform "github-copilot"`) ||
			!strings.Contains(r.Unreached, "`-p "+agent+"=<name>`") {
			t.Errorf("%s: %+v, want the selection to reach nothing for it, naming -p %s=<name>", agent, r, agent)
		}
	}
}

// The Copilot CLI on the github-copilot profile composes nothing that re-points it: the provider
// names no endpoint and no key, so copilot stays on its own GitHub login at both notches.
func TestCopilotOnGitHubCopilotStaysOnItsOwnLogin(t *testing.T) {
	packs := embeddedNamed(t, "copilot", "github-copilot")
	for _, n := range notches {
		vars, err := agentEnvAt(t, n, packs, nil, nil, "copilot", "github-copilot")
		if err != nil {
			t.Errorf("%s: copilot on github-copilot refused: %v", n.name, err)
			continue
		}
		for _, v := range vars {
			if strings.HasPrefix(v.Key, "COPILOT_PROVIDER") || strings.HasPrefix(v.Key, "COPILOT_MODEL") {
				t.Errorf("%s: copilot on its own client got %s=%q", n.name, v.Key, v.Value)
			}
		}
	}
}

// THE MIXED SET'S PROVIDER POLICY: pi on ["bedrock", "github-copilot"], every table composed from
// the shipped packs alone, allows both of pi's own providers, so yolo's provider-policy extension
// does not block Copilot's requests.
func TestPiOnBedrockAndGitHubCopilotAllowsBothProviders(t *testing.T) {
	packs := embeddedNamed(t, "pi", "github-copilot", "bedrock", "aws-auth")
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
