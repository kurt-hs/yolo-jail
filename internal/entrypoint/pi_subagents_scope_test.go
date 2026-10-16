package entrypoint

// pi_subagents_scope_test.go pins the maintainer's ruling of 2026-09-28 on pi-subagents'
// `subagents` block (docs/reference/providers.md#oq-xm3, which also answers
// the since-deleted pi-model-selection-ux.md's OQ-PM1): pi's settings derive writes it for EVERY
// provider a pi profile selects, not only openai-codex.
//
//   - subagents.defaultModel is the active profile's default model, as `provider/id`, so a
//     child agent that names no model starts there;
//   - subagents.modelScope is {enforce, strict, allow}: over the selected provider's
//     configured models under an `only` (and on openai-codex), else over the whole provider
//     (docs/design/model-lists-and-pickers.md OQ-MM3), so a child may name another model of
//     that provider and never one of another provider.
//
// Every case renders through ConfigurePackSurfaces, the boot's own loop, over the SHIPPED
// pi pack and providers composed from the shipped packs, so deleting the block from the
// derive, or the yolo.model_for call its default goes through, fails a test here. The
// ctx root is an empty directory (newPioencodeRender), so this jail's /ctx/host-pi
// settings cannot leak in.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/luahook"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// shippedScopeRender is a render harness over the providers the shipped pi, zai, openrouter,
// kilo and llamacpp packs compose (plus openai-auth, which declares openai-codex),
// with the profiles those packs ship resolved the way a launch resolves them. kilo's profile
// states a model, because kilo declares no model list and names none of its own.
func shippedScopeRender(t *testing.T) *pioencodeRender {
	t.Helper()
	packs := testPacksForAgent(t, "pi", "zai", "openrouter", "kilo", "llamacpp", "openai-auth")
	providers, err := packload.ComposeProviders(nil, packs)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := packload.ResolveProfiles(packs, map[string]packload.UserProfile{
		"kilo": {Provider: "kilo", Options: map[string]string{"model": "deepseek-v4.1-flash"}},
	}, providers)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex", "zai", "openrouter", "kilo", "llamacpp"} {
		if _, ok := resolved[name]; !ok {
			t.Fatalf("the shipped packs resolve no %q profile, so its case measures nothing", name)
		}
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(mustCompactJSON(t, packload.ProfilesWireTable(resolved)))
	return r
}

// wantPiSubagents is the block each shipped profile renders. The codex one is unchanged by
// the ruling (the exact declared ids, ML-D5). zai and openrouter are pi's own providers, so pi
// uses its own list and their scope is the whole provider, the default being the profile's
// `model` as pi's own id (docs/design/pi-codex-provider-shadowing.md OQ-3); kilo declares no
// model list, so its scope is the whole provider and nothing else.
func wantPiSubagents(profile string) map[string]any {
	scope := func(allow ...any) map[string]any {
		return map[string]any{"enforce": true, "strict": true, "allow": allow}
	}
	switch profile {
	case "codex":
		return map[string]any{
			"defaultProvider": "openai-codex",
			"defaultModel":    "openai-codex/gpt-6.1-sol",
			"modelScope":      scope(piCodexExactAllow...),
		}
	case "zai":
		return map[string]any{
			"defaultProvider": "zai",
			"defaultModel":    "zai/glm-5.3",
			"modelScope":      scope("zai/*"),
		}
	case "openrouter":
		return map[string]any{
			"defaultProvider": "openrouter",
			"modelScope":      scope("openrouter/*"),
		}
	case "kilo":
		return map[string]any{
			"defaultProvider": "kilo",
			"defaultModel":    "kilo/deepseek/deepseek-v4.1-flash",
			"modelScope":      scope("kilo/*"),
		}
	case "llamacpp":
		return map[string]any{
			"defaultProvider": "llamacpp",
			"defaultModel":    "llamacpp/llama",
			// A configured list no `only` narrowed scopes the whole provider (OQ-MM3).
			"modelScope": scope("llamacpp/*"),
		}
	}
	panic("no expectation for profile " + profile)
}

func requirePiSubagents(t *testing.T, r *pioencodeRender, profile string) {
	t.Helper()
	got := r.piSettings(t)["subagents"]
	if want := wantPiSubagents(profile); !reflect.DeepEqual(got, want) {
		t.Errorf("after a -p %s render, pi subagents =\n  %#v\nwant\n  %#v", profile, got, want)
	}
}

// One provider class per case: the subscription (codex), providers pi has built in (zai, with a
// profile model, and openrouter, with none), a provider with configured models (llamacpp), and
// one with none whose profile names a model (kilo).
func TestPiSubagentsBlockFollowsEveryProvidersProfile(t *testing.T) {
	for _, profile := range []string{"codex", "zai", "openrouter", "kilo", "llamacpp"} {
		t.Run(profile, func(t *testing.T) {
			r := shippedScopeRender(t)
			r.render(t, `{"pi":"`+profile+`"}`)
			requirePiSubagents(t, r, profile)
		})
	}
}

// The child's default is the SAME model the chat selection starts on: one resolution of the
// profile's default, not two that can drift.
func TestPiSubagentsDefaultIsTheChatSelectionsModel(t *testing.T) {
	for _, profile := range []string{"codex", "zai", "kilo", "llamacpp"} {
		t.Run(profile, func(t *testing.T) {
			r := shippedScopeRender(t)
			r.render(t, `{"pi":"`+profile+`"}`)
			settings := r.piSettings(t)
			sub, _ := settings["subagents"].(map[string]any)
			want := absentOr(settings["defaultProvider"]) + "/" + absentOr(settings["defaultModel"])
			if sub["defaultModel"] != want {
				t.Errorf("subagents.defaultModel = %v, want the chat selection %q", sub["defaultModel"], want)
			}
		})
	}
}

// A profile switch between codex and a non-codex provider, both directions and on one home,
// leaves exactly the new provider's block: no codex id in another provider's scope, no
// codex default under a provider whose default yolo cannot name, and no stale block after
// a deselect.
func TestPiSubagentsBlockFollowsAProfileSwitch(t *testing.T) {
	for _, other := range []string{"zai", "openrouter"} {
		t.Run("codex to "+other+" and back", func(t *testing.T) {
			r := shippedScopeRender(t)
			for _, profile := range []string{"codex", other, "codex", other} {
				r.render(t, `{"pi":"`+profile+`"}`)
				requirePiSubagents(t, r, profile)
			}
			// A deselect: no profile, no policy. The block was yolo's computed output, so it
			// goes with the selection and pi-subagents' own default applies again.
			r.render(t, `{}`)
			if sub, present := r.piSettings(t)["subagents"]; present {
				t.Errorf("after an unprofiled render pi subagents = %#v, want it gone", sub)
			}
			r.render(t, `{"pi":"codex"}`)
			requirePiSubagents(t, r, "codex")
		})
	}
}

// For a provider whose default yolo cannot name, the block DELETES any subagents.defaultModel
// a lower layer holds, so the child inherits the parent session's model, which is on the
// selected provider. A host settings.json carrying a codex policy — this jail's maintainer
// keeps one — would otherwise hand every openrouter child a codex model, which
// pi-subagents' scope only WARNS about for an inherited model (model-scope.ts,
// checkModelScope: "inherited" is severity warn). The host's own keys under the block stay.
func TestPiSubagentsUnknownDefaultClearsAHostLayersCrossProviderModel(t *testing.T) {
	r := shippedScopeRender(t)
	writePiHostSettings(t, `{"subagents":{"defaultModel":"openai-codex/gpt-6-sol",`+
		`"modelScope":{"enforce":true,"allow":["openai-codex/gpt-6-sol"]},"disableBuiltins":true}}`)
	r.render(t, `{"pi":"openrouter"}`)
	sub, _ := r.piSettings(t)["subagents"].(map[string]any)
	if model, present := sub["defaultModel"]; present {
		t.Errorf("an openrouter launch kept the host layer's subagents.defaultModel %v — a "+
			"child with no model of its own would start on another provider", model)
	}
	want := wantPiSubagents("openrouter")["modelScope"]
	if !reflect.DeepEqual(sub["modelScope"], want) {
		t.Errorf("subagents.modelScope = %#v, want %#v over the host layer's", sub["modelScope"], want)
	}
	if sub["disableBuiltins"] != true {
		t.Errorf("the host layer's own subagents.disableBuiltins was lost: %#v", sub)
	}
}

// The default goes through yolo.model_for, whose missing-alias note is a WARNING at boot:
// a provider with a model list and no `default` alias, under a profile that names no model,
// resolves no default, says so on the launch's stderr, and still renders its scope. This
// pins the boot's Warn wiring (deriveComputedLayer) as well as pi's call.
func TestPiSubagentsMissingDefaultAliasWarnsAndStillScopes(t *testing.T) {
	r := newPioencodeRender(t, noDefaultJSON)
	r.render(t, `{"pi":"solo"}`)
	want := map[string]any{
		"defaultProvider": "solo",
		// solo's list is no `only`, so the scope is the whole provider (OQ-MM3).
		"modelScope": map[string]any{"enforce": true, "strict": true, "allow": []any{"solo/*"}},
	}
	if got := r.piSettings(t)["subagents"]; !reflect.DeepEqual(got, want) {
		t.Errorf("pi subagents = %#v, want %#v", got, want)
	}
	note := luahook.MissingTierAliasNote("solo", "default")
	if !strings.Contains(r.errw.String(), note) {
		t.Errorf("the boot did not warn about solo's missing default alias; want %q in:\n%s",
			note, r.errw.String())
	}
}

// The codex branch takes the same rule when its declared list is empty (a user who nulled
// the models): the whole provider, never an empty allow, which pi-subagents refuses as a
// settings error. Before the ruling this case wrote no scope at all, so a child could name
// any provider's model.
func TestPiSubagentsCodexWithNoDeclaredModelsScopesTheWholeProvider(t *testing.T) {
	r := newPioencodeRender(t, `{"openai-codex":{"endpoints":{"openai-responses":`+
		`{"base_url":"https://chatgpt.com/backend-api/codex","wire_api":"openai-responses"}}}}`)
	r.render(t, `{"pi":"openai-codex"}`)
	want := map[string]any{
		"defaultProvider": "openai-codex",
		"modelScope": map[string]any{
			"enforce": true, "strict": true, "allow": []any{"openai-codex/*"},
		},
	}
	if got := r.piSettings(t)["subagents"]; !reflect.DeepEqual(got, want) {
		t.Errorf("pi subagents = %#v, want %#v", got, want)
	}
}

// openrouterAliasRender is shippedScopeRender with the SHIPPED openrouter provider extended by a
// user entry that declares the two OpenRouter routing aliases the fix-6/fix-7 cases are about —
// the shape `providers.<p>.models` takes, alias -> real id (docs/reference/providers.md §3.4).
// The profile `or` selects openrouter, which pi has BUILT IN, so the built-in-provider branch is
// the one under test. Everything renders through ConfigurePackSurfaces over the shipped pi and
// openrouter packs, so the resolution under test is the derive's own.
func openrouterAliasRender(t *testing.T, profileModel string) *pioencodeRender {
	t.Helper()
	packs := testPacksForAgent(t, "pi", "openrouter")
	user, err := jsonx.Decode([]byte(`{"openrouter":{"models":{` +
		`"deepseek-floor":"deepseek/deepseek-v4.1-flash:floor",` +
		`"deepseek-nitro":"deepseek/deepseek-v4.1-flash:nitro"}}}`))
	if err != nil {
		t.Fatalf("fixture providers: %v", err)
	}
	userMap, _ := user.(*jsonx.OrderedMap)
	providers, err := packload.ComposeProviders(userMap, packs)
	if err != nil {
		t.Fatal(err)
	}
	opts := map[string]string{}
	if profileModel != "" {
		opts["model"] = profileModel
	}
	resolved, err := packload.ResolveProfiles(packs, map[string]packload.UserProfile{
		"or": {Provider: "openrouter", Options: opts},
	}, providers)
	if err != nil {
		t.Fatalf("resolve the or profile: %v", err)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(mustCompactJSON(t, packload.ProfilesWireTable(resolved)))
	return r
}

// A profile that names a yolo ALIAS under a provider pi has BUILT IN must render pi's REAL id,
// not the alias. `deepseek-floor` is a yolo alias whose declared id is
// `deepseek/deepseek-v4.1-flash:floor`; the built-in-provider branch used to write the alias
// VERBATIM (`openrouter/deepseek-floor`), which pi cannot resolve against its provider's list,
// so every subagent child died with `Unknown subagent model`. Both the selection and the
// pi-subagents default must carry the resolved id.
//
// NEGATIVE CONTROL: against the pre-fix derive this fails on the first check — the rendered
// defaultModel is `deepseek-floor`, the alias itself.
func TestPiBuiltInProviderResolvesAProfileModelAlias(t *testing.T) {
	r := openrouterAliasRender(t, "deepseek-floor")
	r.render(t, `{"pi":"or"}`)
	settings := r.piSettings(t)
	if got := settings["defaultProvider"]; got != "openrouter" {
		t.Errorf("defaultProvider = %v, want openrouter", got)
	}
	if got := settings["defaultModel"]; got != "deepseek/deepseek-v4.1-flash:floor" {
		t.Errorf("defaultModel = %v, want the RESOLVED id deepseek/deepseek-v4.1-flash:floor "+
			"(the alias must never reach pi: `Unknown subagent model`)", got)
	}
	sub, _ := settings["subagents"].(map[string]any)
	if got := sub["defaultModel"]; got != "openrouter/deepseek/deepseek-v4.1-flash:floor" {
		t.Errorf("subagents.defaultModel = %v, want openrouter/deepseek/deepseek-v4.1-flash:floor", got)
	}
	if got := sub["defaultProvider"]; got != "openrouter" {
		t.Errorf("subagents.defaultProvider = %v, want openrouter", got)
	}
}

// The tombstone half: a profile that names NO model under a provider whose declared list carries
// aliases writes NO defaultModel, so a child inherits the parent session's model rather than
// being pinned to a value the profile never asked for.
func TestPiBuiltInProviderWithNoProfileModelWritesNoDefaultModel(t *testing.T) {
	r := openrouterAliasRender(t, "")
	r.render(t, `{"pi":"or"}`)
	settings := r.piSettings(t)
	if got, present := settings["defaultModel"]; present {
		t.Errorf("defaultModel = %v, want it ABSENT: the profile names no model", got)
	}
	sub, _ := settings["subagents"].(map[string]any)
	if got, present := sub["defaultModel"]; present {
		t.Errorf("subagents.defaultModel = %v, want it ABSENT: the profile names no model", got)
	}
	if got := sub["defaultProvider"]; got != "openrouter" {
		t.Errorf("subagents.defaultProvider = %v, want openrouter", got)
	}
}

// A profile model that cannot be resolved to a registered id REFUSES the render — it is never
// written. The refusal must name the provider, the `model` option and the ids the provider
// declares, and the settings surface must not be written at all.
//
// NEGATIVE CONTROL: against the pre-fix derive no failure is recorded at all (the alias was
// written), so this fails on the `len(fails) == 0` guard.
func TestPiUnresolvableProfileModelRefusesTheRender(t *testing.T) {
	r := openrouterAliasRender(t, "deepseek-flooor") // a typo of deepseek-floor
	fails := r.renderWith(t, `{"pi":"or"}`)

	settings := filepath.Join(r.e.Home, ".pi", "agent", "settings.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings.json must NOT be written when the derive refuses (stat err = %v)", err)
	}
	if len(fails) == 0 {
		t.Fatalf("an unresolvable profile model must REFUSE the render; no failure was "+
			"recorded:\n%s", r.errw.String())
	}
	msg := strings.Join(fails, "\n")
	for _, want := range []string{
		"openrouter",                         // the provider
		"model",                              // the option it came from
		"deepseek-flooor",                    // the value that could not resolve
		"deepseek/deepseek-v4.1-flash:floor", // an available id
		"deepseek/deepseek-v4.1-flash:nitro", // the other available id
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q; got:\n%s", want, msg)
		}
	}
}

// subagentsDeclRender is openrouterAliasRender's fixture for the `subagents` DECLARATION: the
// shipped openrouter provider extended by two user aliases (the shape `providers.<p>.models`
// takes, alias -> real id), over a profile that carries the declaration and, optionally, a
// `model` option. Everything renders through ConfigurePackSurfaces over the shipped pi and
// openrouter packs, and the declaration reaches the derive the way a launch delivers it — the
// resolved YOLO_PROFILES table, through ResolveProfiles and ProfilesWireTable — so the wire
// encode/decode is under test as well as the derive.
func subagentsDeclRender(t *testing.T, sub luahook.Subagents, profileModel string) *pioencodeRender {
	t.Helper()
	packs := testPacksForAgent(t, "pi", "openrouter")
	user, err := jsonx.Decode([]byte(`{"openrouter":{"models":{` +
		`"deepseek-floor":"deepseek/deepseek-v4.1-flash:floor",` +
		`"deepseek-whale":"deepseek/deepseek-v4.1-flash:whale"}}}`))
	if err != nil {
		t.Fatalf("fixture providers: %v", err)
	}
	userMap, _ := user.(*jsonx.OrderedMap)
	providers, err := packload.ComposeProviders(userMap, packs)
	if err != nil {
		t.Fatal(err)
	}
	opts := map[string]string{}
	if profileModel != "" {
		opts["model"] = profileModel
	}
	resolved, err := packload.ResolveProfiles(packs, map[string]packload.UserProfile{
		"or": {Provider: "openrouter", Options: opts, Subagents: sub},
	}, providers)
	if err != nil {
		t.Fatalf("resolve the or profile: %v", err)
	}
	if got := resolved["or"].Subagents; got.Default != sub.Default || len(got.Roles) != len(sub.Roles) {
		t.Fatalf("ResolveProfiles dropped the declaration: got %+v, want %+v", got, sub)
	}
	r := newPioencodeRender(t, mustCompactJSON(t, providers))
	r.wireProfiles(mustCompactJSON(t, packload.ProfilesWireTable(resolved)))
	return r
}

// A profile that declares `subagents` writes pi-subagents' per-role keys: `agentOverrides.
// <role>.model` and `modelScope.agents.<role>.allow`, both as `<provider>/<id>`, and the
// declared `default` replaces the model a child with none of its own starts on. Both values
// are yolo ALIASES here, so the assertion is also that they resolved through fix 6's ladder
// (profileModel / yolo.model_for) to the ids the provider declares — the alias itself would
// be `Unknown subagent model`.
//
// NEGATIVE CONTROL: against the pre-change derive none of the four keys exist (the block was
// defaultProvider/defaultModel/modelScope only), so this fails on the first assertion.
func TestPiSubagentsDeclarationRoutesChildModels(t *testing.T) {
	r := subagentsDeclRender(t, luahook.Subagents{
		Default: "deepseek-floor",
		Roles:   map[string]string{"worker": "deepseek-floor", "reviewer": "deepseek-whale"},
	}, "deepseek-whale")
	r.render(t, `{"pi":"or"}`)

	got := r.piSettings(t)["subagents"]
	want := map[string]any{
		"defaultProvider": "openrouter",
		"defaultModel":    "openrouter/deepseek/deepseek-v4.1-flash:floor",
		"agentOverrides": map[string]any{
			"worker":   map[string]any{"model": "openrouter/deepseek/deepseek-v4.1-flash:floor"},
			"reviewer": map[string]any{"model": "openrouter/deepseek/deepseek-v4.1-flash:whale"},
		},
		"modelScope": map[string]any{
			"enforce": true,
			"strict":  true,
			"allow":   []any{"openrouter/*"},
			"agents": map[string]any{
				"worker": map[string]any{
					"enforce": true, "strict": true,
					"allow": []any{"openrouter/deepseek/deepseek-v4.1-flash:floor"},
				},
				"reviewer": map[string]any{
					"enforce": true, "strict": true,
					"allow": []any{"openrouter/deepseek/deepseek-v4.1-flash:whale"},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after a declared subagents render, pi subagents =\n  %#v\nwant\n  %#v", got, want)
	}
}

// The declared `default` is what a child that names no model of its own starts on, and it is
// resolved the same way the `model` option is: an ALIAS becomes pi's real id, an exact id and
// a provider with no declared list pass through verbatim. The role half is omitted when the
// profile pins none, so pi-subagents' own per-role configuration is untouched.
func TestPiSubagentsDeclarationDefaultAloneLeavesRolesAlone(t *testing.T) {
	r := subagentsDeclRender(t, luahook.Subagents{Default: "deepseek-floor"}, "deepseek-whale")
	r.render(t, `{"pi":"or"}`)
	sub, _ := r.piSettings(t)["subagents"].(map[string]any)
	if got := sub["defaultModel"]; got != "openrouter/deepseek/deepseek-v4.1-flash:floor" {
		t.Errorf("defaultModel = %v, want the DECLARED default resolved to the real id", got)
	}
	if _, present := sub["agentOverrides"]; present {
		t.Errorf("a profile pinning no role wrote agentOverrides: %#v", sub["agentOverrides"])
	}
	scope, _ := sub["modelScope"].(map[string]any)
	if _, present := scope["agents"]; present {
		t.Errorf("a profile pinning no role wrote modelScope.agents: %#v", scope["agents"])
	}
}

// A profile that declares nothing about its children writes exactly the block it always did:
// no agentOverrides, no modelScope.agents, and defaultModel still the model the chat selection
// starts on. This is what "DEFAULT = children inherit the parent's model" means at the
// render — the declaration is the ONLY thing that moves a child off the parent's model.
func TestPiSubagentsWithNoDeclarationWritesNoRoleKeys(t *testing.T) {
	r := subagentsDeclRender(t, luahook.Subagents{}, "deepseek-whale")
	r.render(t, `{"pi":"or"}`)
	got := r.piSettings(t)["subagents"]
	want := map[string]any{
		"defaultProvider": "openrouter",
		"defaultModel":    "openrouter/deepseek/deepseek-v4.1-flash:whale",
		"modelScope": map[string]any{
			"enforce": true, "strict": true, "allow": []any{"openrouter/*"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with no declaration, pi subagents =\n  %#v\nwant\n  %#v", got, want)
	}
}

// A declared value the provider's list cannot resolve REFUSES the render — the fix-6 rule
// applied to the declaration — and the refusal names the declaration path (`subagents.roles.
// worker`), the value, the provider and the ids it does declare. The settings surface is not
// written at all, so a launch never runs children on a model pi would reject.
//
// NEGATIVE CONTROL: against the pre-change derive no failure is recorded (the declaration was
// not read at all), so this fails on the `len(fails) == 0` guard.
func TestPiSubagentsDeclarationRefusesAnUnresolvableModel(t *testing.T) {
	r := subagentsDeclRender(t, luahook.Subagents{
		Roles: map[string]string{"worker": "deepseek-flooor"},
	}, "deepseek-whale")
	fails := r.renderWith(t, `{"pi":"or"}`)

	settings := filepath.Join(r.e.Home, ".pi", "agent", "settings.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings.json must NOT be written when the derive refuses (stat err = %v)", err)
	}
	if len(fails) == 0 {
		t.Fatalf("an unresolvable declared subagent model must REFUSE the render; no failure "+
			"was recorded:\n%s", r.errw.String())
	}
	msg := strings.Join(fails, "\n")
	for _, want := range []string{
		"subagents.roles.worker",             // the declaration that named the value
		"deepseek-flooor",                    // the value that could not resolve
		"openrouter",                         // the provider
		"deepseek/deepseek-v4.1-flash:floor", // an available id
		"deepseek/deepseek-v4.1-flash:whale", // the other available id
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q; got:\n%s", want, msg)
		}
	}
}
