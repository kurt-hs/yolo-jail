package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/luahook"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestProjectProviderSemanticsOverwritesSpoofedSelectionFromCanonicalInputs(t *testing.T) {
	pack := providerDeriveFixture(t, `yolo.derive("probe", "snapshot", function(ctx)
	return {
	  selected_provider = ctx.selected_provider,
	  profile_name = ctx.profile_name,
	  profile = ctx.profile,
	  subagents = ctx.subagents,
	  active_set = ctx.active_set,
	  enforce_models = ctx.enforce_models,
	  capabilities = ctx.providers.alpha.capabilities,
	  legacy_id = ctx.providers.alpha.models.legacy,
	  legacy_name = ctx.providers.alpha.model_options.legacy.name
	}
end)`)
	provider := packdecl.ProviderContribution{Name: "alpha", Model: "provider-default",
		Options:      map[string]packdecl.OptionDefault{"region": {Defaulted: true, Value: "east"}, "model": {Defaulted: true, Value: "base-model"}, "tier": {}},
		Capabilities: []string{"base-cap"}, Variants: map[string]packdecl.ProviderVariant{
			"compact": {Model: "client-model", Options: map[string]packdecl.OptionDefault{"tier": {Defaulted: true, Value: "compact"}},
				Subagents:     map[string]any{"default": "client-model", "roles": map[string]any{"worker": "client-model"}},
				EnforceModels: boolp(false), Capabilities: []string{"variant-cap"}},
		},
	}
	second := packdecl.ProviderContribution{Name: "beta", Model: "beta-model", Options: map[string]packdecl.OptionDefault{"region": {Defaulted: true, Value: "west"}}}
	input := ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{provider, second},
		Selection: ActiveProviderSelection{Default: []string{"alpha/compact", "beta"}}, Receivers: ProviderReceivers([]*packload.Pack{pack})}
	context := luahook.DeriveCtx{SelectedProvider: "spoof", ProfileName: "spoof", Profile: map[string]string{"model": "spoof-model"},
		Subagents: luahook.Subagents{Default: "spoof-model"}, ModelsNotEnforced: false,
		ActiveSet: []luahook.SetEntry{{ProfileName: "spoof", Provider: "spoof"}},
		Tables: map[string]map[string]any{"providers": {
			"alpha": map[string]any{"models": map[string]any{"legacy": "legacy-id"}, "model_options": map[string]any{"legacy": map[string]any{"name": "legacy name"}}, "capabilities": []any{"stale-cap"}},
			"beta":  map[string]any{},
		}}}
	_, outputs, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "probe", Pack: pack, Surface: "snapshot", Context: context}})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("derive outputs = %#v", outputs)
	}
	got := outputs[0].Value
	if got["selected_provider"] != "alpha" || got["profile_name"] != "alpha/compact" || got["enforce_models"] != false || got["legacy_id"] != "legacy-id" || got["legacy_name"] != "legacy name" {
		t.Fatalf("canonical primary/legacy coexistence was not projected: %#v", got)
	}
	profile, _ := got["profile"].(map[string]any)
	if profile["model"] != "client-model" || profile["region"] != "east" || profile["tier"] != "compact" {
		t.Fatalf("canonical profile options = %#v", profile)
	}
	subs, _ := got["subagents"].(map[string]any)
	roles, _ := subs["roles"].(map[string]any)
	if subs["default"] != "client-model" || roles["worker"] != "client-model" {
		t.Fatalf("canonical subagents = %#v", subs)
	}
	set, _ := got["active_set"].([]any)
	if len(set) != 2 {
		t.Fatalf("canonical active_set = %#v", set)
	}
	primary, _ := set[0].(map[string]any)
	secondary, _ := set[1].(map[string]any)
	primaryProfile, _ := primary["profile"].(map[string]any)
	secondaryProfile, _ := secondary["profile"].(map[string]any)
	caps, _ := got["capabilities"].([]any)
	if primary["profile_name"] != "alpha/compact" || primary["enforce_models"] != false || secondary["enforce_models"] != true || secondary["provider"] != "beta" || primaryProfile["model"] != "client-model" ||
		secondaryProfile["model"] != "beta-model" || len(caps) != 1 || caps[0] != "variant-cap" {
		t.Fatalf("canonical active_set/provider capabilities = %#v, caps=%v", set, caps)
	}
	if context.SelectedProvider != "spoof" || context.Profile["model"] != "spoof-model" || context.ActiveSet[0].Provider != "spoof" {
		t.Fatal("derive projection mutated caller context")
	}
}

func TestProjectProviderSemanticsRejectsSecondaryViaEvenWhenServiceMatches(t *testing.T) {
	pack := providerDeriveFixture(t, `yolo.derive("probe", "snapshot", function(ctx) return {} end)`)
	input := ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{
		{Name: "p", Via: "wire-bridge"}, {Name: "q", Via: "wire-bridge"},
	}, Selection: ActiveProviderSelection{Default: []string{"p", "q"}}, Receivers: ProviderReceivers([]*packload.Pack{pack})}
	_, _, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "probe", Pack: pack, Surface: "snapshot", ViaRoutes: map[string]ProviderViaRoute{"wire-bridge": {URL: "https://bridge.example", APIKeyEnvName: "TOKEN"}}}})
	if err == nil || !strings.Contains(err.Error(), "q") || !strings.Contains(err.Error(), "wire-bridge") || !strings.Contains(err.Error(), "remove or reselect") {
		t.Fatalf("secondary via must refuse even when service matches, with remedy: %v", err)
	}
}

func TestProjectProviderSemanticsPreservesAbsentButClearsExplicitEmptyModelRows(t *testing.T) {
	pack := providerDeriveFixture(t, `yolo.derive("probe", "snapshot", function(ctx) return { models = ctx.providers.p.models } end)`)
	input := ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{{Name: "p", ModelRows: map[string]packdecl.ProviderModel{}}},
		Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{pack})}
	context := luahook.DeriveCtx{Tables: map[string]map[string]any{"providers": {"p": map[string]any{"models": map[string]any{"legacy": "legacy-id"}}}}}
	_, outputs, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "probe", Pack: pack, Surface: "snapshot", Context: context}})
	if err != nil {
		t.Fatal(err)
	}
	models, ok := outputs[0].Value["models"].(map[string]any)
	if !ok || len(models) != 0 {
		t.Fatalf("explicit empty canonical ModelRows must clear old list, got %#v", outputs[0].Value)
	}
}

func TestProjectProviderSemanticsSkipsNativeAndEmptySelectionAndRequiresARegisteredProducer(t *testing.T) {
	bad := providerDeriveFixture(t, `yolo.derive("probe", "snapshot", function(ctx) error("native must not invoke Lua") end)`)
	receivers := ProviderReceivers([]*packload.Pack{bad})
	for name, selection := range map[string]ActiveProviderSelection{
		"native": {Named: map[string][]string{"probe": {"native"}}},
		"empty":  {},
	} {
		t.Run(name, func(t *testing.T) {
			_, outputs, err := ProjectProviderSemantics(ProviderSemanticInput{Selection: selection, Receivers: receivers},
				[]ProviderDeriveRequest{{Receiver: "probe", Pack: bad, Surface: "snapshot"}})
			if err != nil || len(outputs) != 0 {
				t.Fatalf("native/empty selection must skip derive untouched: outputs=%#v err=%v", outputs, err)
			}
		})
	}
	missing := providerDeriveFixture(t, `local only = true`)
	_, _, err := ProjectProviderSemantics(ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{{Name: "p"}},
		Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{missing})},
		[]ProviderDeriveRequest{{Receiver: "probe", Pack: missing, Surface: "snapshot"}})
	if err == nil || !strings.Contains(err.Error(), "packs/probe") || !strings.Contains(err.Error(), "probe/snapshot") || !strings.Contains(err.Error(), "registers no yolo.derive") || !strings.Contains(err.Error(), "choose a supported") {
		t.Fatalf("declared surface without producer must fail actionably: %v", err)
	}
	empty := providerDeriveFixture(t, `yolo.derive("probe", "snapshot", function(ctx) return {} end)`)
	_, outputs, err := ProjectProviderSemantics(ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{{Name: "p"}},
		Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{empty})},
		[]ProviderDeriveRequest{{Receiver: "probe", Pack: empty, Surface: "snapshot"}})
	if err != nil || len(outputs) != 1 || outputs[0].Value == nil || len(outputs[0].Value) != 0 {
		t.Fatalf("registered empty object derive must remain a valid result: %#v, %v", outputs, err)
	}
}

func providerDeriveFixture(t *testing.T, script string) *packload.Pack {
	t.Helper()
	root := t.TempDir()
	manifest := `{"name":"probe","contributes":[
		{"kind":"program","bin":"probe","via":"npm","package":"probe","protocols":["openai"],"provider_sets":true},
		{"kind":"config","config":[{"agent":"probe","name":"snapshot","codec":"json","path":"~/.probe/snapshot.json","mode":"computed"}]}
	]}`
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "derive.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	pack, problems := packload.LoadDir(root, "probe")
	if pack == nil || len(problems) != 0 {
		t.Fatalf("load derive fixture: pack=%v problems=%v", pack, problems)
	}
	return pack
}
