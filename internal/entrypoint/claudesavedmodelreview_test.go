package entrypoint

// claudesavedmodelreview_test.go pins the saved-model rules the review of MM-D44 added
// (docs/design/model-lists-and-pickers.md MM-D44): a value is never rewritten without a change
// of the trigger; a value tied to another provider never survives a change; the trigger covers
// the bridge route and the profile's model, so switching either is a change.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// ON CLAUDE'S OWN LOGIN A CLAUDE ID IS ITS OWN EQUIVALENT. The user's host settings.json names a
// full id with its 1M suffix; boot after boot on claude's own login keeps it exactly, never the
// bare tier word.
func TestClaudeOwnLoginKeepsAHostImportedModelExactly(t *testing.T) {
	f := newProviderKeyFixture(t, nil)
	if err := os.WriteFile(filepath.Join(f.hostDir, "settings.json"),
		[]byte(`{"model":"claude-opus-5-5[1m]"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		got, _ := f.boot("")
		if got["model"] != "claude-opus-5-5[1m]" {
			t.Fatalf("boot %d: model = %v, want the host's claude-opus-5-5[1m] kept", i, got["model"])
		}
	}
}

// AT THE HOST the first apply on claude's own login leaves the user's real file's model alone.
func TestClaudeHostApplyKeepsTheUsersModel(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"model":"claude-opus-5-5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := embeddedPack("claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RenderHostPack(p, home, render.OwnershipOwn, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := decodeJSONFile(t, path)["model"]; got != "claude-opus-5-5" {
		t.Errorf("model = %v after a host apply, want the user's claude-opus-5-5 untouched", got)
	}
}

// A BEDROCK-ONLY PICK NEVER SURVIVES LEAVING BEDROCK, even one the saved model keeps as the
// user's: the provider keys drop it (packs/claude/pack.json's `/model`), and the keep must not
// re-save it out of their reach.
func TestClaudeBedrockOnlyPickLeavesWithBedrock(t *testing.T) {
	f := newProviderKeyFixture(t, map[string]string{"company": `{"name":"company","description":"d",
	  "contributes":[{"kind":"models","provider":"bedrock","add":[
	    {"id":"global.anthropic.claude-opus-5-5","vendor":"anthropic","alias":"opus"}]}]}`})
	f.boot("")
	f.edit(func(m map[string]any) { m["model"] = "claude-opus-5-5" })
	if got, _ := f.boot("bedrock"); got["model"] != "global.anthropic.claude-opus-5-5" {
		t.Fatalf("model = %v on Bedrock, want its Opus written", got["model"])
	}
	const arn = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	f.edit(func(m map[string]any) { m["model"] = arn })
	if got, _ := f.boot("bedrock"); got["model"] != arn {
		t.Fatalf("model = %v on Bedrock, want the user's ARN kept", got["model"])
	}
	if got, _ := f.boot(""); got["model"] == arn {
		t.Errorf("model = %v off Bedrock, want the Bedrock-only ARN gone", got["model"])
	}
}

// THE FIRST LAUNCH AFTER AN UPGRADE IS A CHANGE: no trigger was recorded, so a stale value from
// another provider is replaced by its equivalent here.
func TestClaudeUpgradeReplacesAStaleModel(t *testing.T) {
	h := newClaudeHome(t, bedrockTierPack(t))
	if err := os.MkdirAll(h.e.ClaudeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.e.ClaudeDir(), "settings.json"),
		[]byte(`{"model":"global.anthropic.claude-opus-5-5"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, _ := savedModel(h.boot("zai")); m != "glm-5.3[1m]" {
		t.Errorf("model = %q on the first z.ai launch after an upgrade, want z.ai's default", m)
	}
}

// THE BRIDGE ROUTE IS PART OF THE TRIGGER (bedrock and bedrock-bridge share the provider): a
// non-Anthropic pick made through the bridge does not follow claude onto its own Bedrock client,
// which cannot call it.
func TestClaudeLeavingTheBridgeIsAProviderChange(t *testing.T) {
	company := companyModelsPack(t, `{"kind":"models","provider":"bedrock","add":[
	    {"id":"global.anthropic.claude-opus-5-5","vendor":"anthropic","alias":"default"},
	    {"id":"global.moonshot.kimi-k3","vendor":"moonshot"}]}`)
	h := newClaudeHome(t, append(testPacksForAgent(t, "claude", "bedrock"), company))
	h.boot("bedrock-bridge")
	const other = "us.meta.llama-5-maverick"
	h.pick(other)
	if m, _ := savedModel(h.boot("bedrock-bridge")); m != other {
		t.Fatalf("model = %q through the bridge, want the user's pick kept", m)
	}
	if m, _ := savedModel(h.boot("bedrock")); m != "global.anthropic.claude-opus-5-5" {
		t.Errorf("model = %q on claude's own Bedrock client, want the list's default written", m)
	}
}

// A PROFILE'S `model` CHANGING ON THE SAME PROVIDER IS A CHANGE: the new model is written once,
// and a /model pick after it stands.
func TestClaudeProfileModelChangeWritesTheNewModelOnce(t *testing.T) {
	packs := testPacksForAgent(t, "claude", "zai")
	h := newClaudeHome(t, packs)
	profiles := func(model string) map[string]packload.UserProfile {
		return map[string]packload.UserProfile{"zai": {Provider: "zai", Options: map[string]string{"model": model}}}
	}
	h.profiles = profiles("glm-5.3")
	if m, _ := savedModel(h.boot("zai")); m != "glm-5.3[1m]" {
		t.Fatalf("model = %q, want glm-5.3[1m]", m)
	}
	h.pick("glm-4.6[1m]")
	h.profiles = profiles("glm-5.3-flash")
	if m, _ := savedModel(h.boot("zai")); m != "glm-5.3-flash[1m]" {
		t.Errorf("model = %q after the profile's model changed, want the new glm-5.3-flash[1m]", m)
	}
	h.pick("glm-4.6[1m]")
	if m, _ := savedModel(h.boot("zai")); m != "glm-4.6[1m]" {
		t.Errorf("model = %q, want the /model pick after the change kept", m)
	}
}

// A VALUE VALID ON THE ROUTE IS ITS OWN EQUIVALENT, on every route, so a first launch after an
// upgrade (no recorded trigger, a change) rewrites none of these: each was kept before the saved
// model existed, and each is a model the route can call.
func TestClaudeUpgradeKeepsAValueValidOnItsRoute(t *testing.T) {
	bedrockList := companyModelsPack(t, `{"kind":"models","provider":"bedrock","add":[
	    {"id":"global.anthropic.claude-opus-5-5","vendor":"anthropic","alias":"opus"}]}`)
	for _, tc := range []struct {
		name, profile, value string
		packs                []*packload.Pack
	}{
		{"native Bedrock, a 1M id", "bedrock", "global.anthropic.claude-opus-5-5[1m]", testPacksForAgent(t, "claude", "bedrock")},
		{"native Bedrock, a dated id", "bedrock", "us.anthropic.claude-sonnet-4-5-20250929-v1:0", testPacksForAgent(t, "claude", "bedrock")},
		{"native Bedrock, a tier with 1M", "bedrock", "opus[1m]", testPacksForAgent(t, "claude", "bedrock")},
		{"a Bedrock list naming opus, its id with 1M", "bedrock", "global.anthropic.claude-opus-5-5[1m]",
			append(testPacksForAgent(t, "claude", "bedrock"), bedrockList)},
		{"z.ai, a typed id off the list", "zai", "glm-4.5-air", testPacksForAgent(t, "claude", "zai")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newClaudeHome(t, tc.packs)
			if err := os.MkdirAll(h.e.ClaudeDir(), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(h.e.ClaudeDir(), "settings.json"),
				[]byte(`{"model":"`+tc.value+`"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if m, _ := savedModel(h.boot(tc.profile)); m != tc.value {
				t.Errorf("model = %q, want %q kept: it is valid on this route", m, tc.value)
			}
		})
	}
}

// A VALUE THE NEW ROUTE CANNOT CALL NEVER SURVIVES A CHANGE: a z.ai pick on claude's own login,
// which has no equivalent for it and no default, is removed, so claude uses its own.
func TestClaudeChangeRemovesAPickTheNewRouteCannotCall(t *testing.T) {
	h := newClaudeHome(t, testPacksForAgent(t, "claude", "zai"))
	h.boot("zai")
	h.pick("glm-4.6[1m]")
	if m, ok := savedModel(h.boot("")); ok {
		t.Errorf("model = %q on claude's own login, want the z.ai pick removed", m)
	}
}

// ON CLAUDE'S OWN BEDROCK CLIENT a profile naming the Anthropic entry is the saved model, and one
// naming another maker's entry, which that client cannot call, saves nothing.
func TestClaudeNativeBedrockSavesOnlyAnAnthropicModel(t *testing.T) {
	company := companyModelsPack(t, `{"kind":"models","provider":"bedrock","add":[
	    {"id":"global.anthropic.claude-opus-5-5","vendor":"anthropic"},
	    {"id":"us.openai.gpt-6.1-sol","vendor":"openai"}]}`)
	packs := append(testPacksForAgent(t, "claude", "bedrock"), company)
	for _, tc := range []struct{ model, want string }{
		{"global.anthropic.claude-opus-5-5", "global.anthropic.claude-opus-5-5"},
		{"us.openai.gpt-6.1-sol", ""},
	} {
		got := renderClaudeModels(t, packs, nil, map[string]packload.UserProfile{
			"mine": {Provider: "bedrock", Options: map[string]string{"model": tc.model}}}, "mine")
		m, _ := got.settings["model"].(string)
		if m != tc.want {
			t.Errorf("profile model %s: settings model = %q, want %q", tc.model, m, tc.want)
		}
	}
}
