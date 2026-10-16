package entrypoint

// claudesavedmodel_test.go pins claude's saved `model` (the `/model` choice Claude Code keeps in
// ~/.claude/settings.json), which yolo writes ONCE per provider change
// (docs/design/model-lists-and-pickers.md OQ-MM3, MM-D43): the previous model's equivalent on
// the new provider, matched by claude's tier word, else the new provider's default, else no key
// at all. While the provider stays the same the user's own pick stands.
//
// Every boot runs the production call site, the boot render (ConfigurePackByName), over one
// jail home, so the selection record and the file a boot leaves are what the next one reads.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// claudeHome is one jail home claude boots in, again and again, on whatever profile the
// boot names.
type claudeHome struct {
	t     *testing.T
	e     *Env
	packs []*packload.Pack
	// profiles are the user profiles each boot resolves, nil for the packs' own alone.
	profiles map[string]packload.UserProfile
}

func newClaudeHome(t *testing.T, packs []*packload.Pack) *claudeHome {
	t.Helper()
	e, _ := newClaudePrismEnv(t, map[string]string{})
	return &claudeHome{t: t, e: e, packs: packs}
}

// boot renders claude's surfaces with profile active ("" is claude's own login) and returns
// settings.json as the boot left it.
func (h *claudeHome) boot(profile string) map[string]any {
	h.t.Helper()
	providers, err := packload.ComposeProviders(nil, h.packs)
	if err != nil {
		h.t.Fatal(err)
	}
	resolved, err := packload.ResolveProfiles(h.packs, h.profiles, providers)
	if err != nil {
		h.t.Fatal(err)
	}
	h.e.Vars["YOLO_PROVIDERS"] = mustCompactJSON(h.t, providers)
	h.e.Vars["YOLO_USE_PROFILES"] = `{"claude":"` + profile + `"}`
	h.e.Vars["YOLO_PROFILES"] = mustCompactJSON(h.t, packload.ProfilesWireTable(resolved))
	if err := ConfigurePackByName(h.e, "claude"); err != nil {
		h.t.Fatal(err)
	}
	return h.settings()
}

func (h *claudeHome) settings() map[string]any {
	h.t.Helper()
	return decodeJSONFile(h.t, filepath.Join(h.e.ClaudeDir(), "settings.json"))
}

// pick is the user's `/model` choice inside the session: Claude Code rewrites its own settings
// file with the key set.
func (h *claudeHome) pick(model string) {
	h.t.Helper()
	s := h.settings()
	s["model"] = model
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.e.ClaudeDir(), "settings.json"), data, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func savedModel(s map[string]any) (string, bool) {
	v, ok := s["model"]
	m, _ := v.(string)
	return m, ok
}

// bedrockTierPack adds two Anthropic models to `bedrock`, each under claude's tier alias, with
// no `only`: claude's own Bedrock client then has an opus and a sonnet model to map to, and
// bedrock still declares no default (MM-D32).
func bedrockTierPack(t *testing.T) []*packload.Pack {
	company := companyModelsPack(t, `{"kind":"models","provider":"bedrock","add":[
	    {"id":"global.anthropic.claude-opus-5-5","vendor":"anthropic","alias":"opus","name":"Claude Opus 5.5"},
	    {"id":"us.anthropic.claude-sonnet-5","vendor":"anthropic","alias":"sonnet","name":"Claude Sonnet 5"}]}`)
	return append(testPacksForAgent(t, "claude", "bedrock", "zai"), company)
}

// A PROVIDER CHANGE WRITES THE PREVIOUS MODEL'S TIER EQUIVALENT, ONCE. The walk: claude's own
// login with no saved model (nothing written, since that login declares no default), the user
// picks Opus, a move to Bedrock writes Bedrock's Opus, the user's Sonnet pick on Bedrock stands
// through the next launch, a move to z.ai (no Sonnet-tier model) writes z.ai's default, and a
// move back to claude's own login, which has no default, removes the value yolo wrote.
func TestClaudeSavedModelFollowsAProviderChange(t *testing.T) {
	h := newClaudeHome(t, bedrockTierPack(t))

	if m, ok := savedModel(h.boot("")); ok {
		t.Fatalf("model = %q on claude's own login with nothing saved, want no key", m)
	}
	h.pick("claude-opus-5-5")

	if m, _ := savedModel(h.boot("bedrock")); m != "global.anthropic.claude-opus-5-5" {
		t.Errorf("model = %q after the move to Bedrock, want Bedrock's Opus, the saved Opus's equivalent", m)
	}

	h.pick("us.anthropic.claude-sonnet-5")
	if m, _ := savedModel(h.boot("bedrock")); m != "us.anthropic.claude-sonnet-5" {
		t.Errorf("model = %q on the same provider, want the user's /model pick kept", m)
	}

	if m, _ := savedModel(h.boot("zai")); m != "glm-5.3[1m]" {
		t.Errorf("model = %q after the move to z.ai, want z.ai's default: it lists no Sonnet-tier model", m)
	}

	if m, ok := savedModel(h.boot("")); ok {
		t.Errorf("model = %q after the move back to claude's own login, want yolo's value removed: "+
			"the login declares no default and the value was yolo's", m)
	}
}

// ON A PROVIDER WITH NO LIST THE EQUIVALENT IS THE TIER WORD, which claude resolves itself.
func TestClaudeSavedModelIsTheTierWordWhereTheProviderHasNoList(t *testing.T) {
	h := newClaudeHome(t, bedrockTierPack(t))
	h.boot("")
	h.pick("claude-opus-5-5")
	if m, _ := savedModel(h.boot("bedrock")); m != "global.anthropic.claude-opus-5-5" {
		t.Fatalf("model = %q on Bedrock, want its Opus", m)
	}
	if m, _ := savedModel(h.boot("")); m != "opus" {
		t.Errorf("model = %q back on claude's own login, want the tier word opus", m)
	}
}

// THE CHOICE OF claude's DEFAULT STANDS. Choosing Default in /model removes the key; the next
// launch on the same provider does not put it back.
func TestClaudeSavedModelLeavesARemovedKeyAloneOnTheSameProvider(t *testing.T) {
	h := newClaudeHome(t, bedrockTierPack(t))
	h.boot("zai")
	s := h.settings()
	delete(s, "model")
	data, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(h.e.ClaudeDir(), "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if m, ok := savedModel(h.boot("zai")); ok {
		t.Errorf("model = %q, want the key the user removed left removed", m)
	}
}
