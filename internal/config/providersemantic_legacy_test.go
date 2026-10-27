package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/luahook"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestNormalizeLegacyProviderModelsKeepsExactSnapshotAndBuildsCanonicalRows(t *testing.T) {
	price := func(s string) *string { return &s }
	input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{"m": "m"}, ModelOptions: packdecl.ModelFacts{
		"m": {"vendor": price("acme"), "name": price("Model"), "context_window": price("100000"), "reasoning": price("false"), "input": price("text,image"),
			"cost_input": price("1.0"), "cost_output": price("2"), "cost_cache_read": price("0.1"), "cost_cache_write": price("0")},
	}}
	got := NormalizeLegacyProviderModels(input, nil)
	if got.Canonical == nil || len(got.Differences) != 0 {
		t.Fatalf("representable legacy models refused: canonical=%#v differences=%#v", got.Canonical, got.Differences)
	}
	row := got.Canonical.ModelRows["m"]
	if got.Canonical.ModelOrder == nil || !reflect.DeepEqual(got.Canonical.ModelOrder, []string{"m"}) || row.Vendor != "acme" || row.Context != 100000 || row.Reasoning == nil || *row.Reasoning || !reflect.DeepEqual(row.Accepts, []string{"text", "image"}) || row.Price == nil || row.Price.Input == nil || *row.Price.Input != 1 {
		t.Fatalf("canonical provider row = %#v, order=%v", row, got.Canonical.ModelOrder)
	}
	// Normalized numeric equality does not replace source provenance: restore the original lexeme.
	restored := got.RestoreLegacyProviderModels(input)
	if !reflect.DeepEqual(restored.Models, input.Models) || !reflect.DeepEqual(restored.ModelOptions, input.ModelOptions) || *restored.ModelOptions["m"]["cost_input"] != "1.0" {
		t.Fatalf("legacy declaration did not restore value/order/lexeme: %#v", restored)
	}
	*got.Snapshot.ModelOptions["m"]["name"] = "mutated snapshot"
	if *input.ModelOptions["m"]["name"] != "Model" {
		t.Fatal("snapshot aliases the caller's nested fact pointer")
	}
}

func TestNormalizeLegacyProviderModelsRefusesOpaqueTombstoneAliasAndConflictingWireFacts(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name     string
		models   map[string]string
		options  packdecl.ModelFacts
		wantPath string
	}{
		{"opaque", map[string]string{"m": "m"}, packdecl.ModelFacts{"m": {"vendor": str("acme"), "future_fact": str("x")}}, "model_options.m.future_fact"},
		{"tombstone", map[string]string{"m": "m"}, packdecl.ModelFacts{"m": {"vendor": str("acme"), "name": nil}}, "model_options.m.name"},
		{"alias is not canonical id", map[string]string{"short": "wire"}, packdecl.ModelFacts{"short": {"vendor": str("acme")}}, "models.short"},
		{"same wire facts conflict", map[string]string{"a": "wire", "b": "wire"}, packdecl.ModelFacts{"a": {"vendor": str("acme")}, "b": {"vendor": str("other")}}, "models.b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := packdecl.ProviderContribution{Name: "p", Models: tc.models, ModelOptions: tc.options}
			got := NormalizeLegacyProviderModels(input, nil)
			if got.Canonical != nil {
				t.Fatalf("lossy declaration produced canonical rows: %#v", got.Canonical)
			}
			found, foundConflict := false, false
			for _, diff := range got.Differences {
				if diff.Path == tc.wantPath {
					found = true
				}
				if tc.name == "same wire facts conflict" && diff.Path == "models.b" && strings.Contains(diff.Reason, "complete model facts differ") {
					foundConflict = true
				}
			}
			if !found {
				t.Fatalf("differences %#v do not name %s", got.Differences, tc.wantPath)
			}
			if tc.name == "same wire facts conflict" && !foundConflict {
				t.Errorf("same-wire facts conflict was not explicit: %#v", got.Differences)
			}
			// Refusal never drops legacy maps or nil fact pointers.
			restored := got.RestoreLegacyProviderModels(input)
			if !reflect.DeepEqual(restored.Models, input.Models) || !reflect.DeepEqual(restored.ModelOptions, input.ModelOptions) {
				t.Fatalf("legacy state lost: %#v", restored)
			}
		})
	}
}

func TestNormalizeLegacyProviderModelsCoalescesOnlyIdenticalFactsForSharedWireIDs(t *testing.T) {
	str := func(s string) *string { return &s }
	input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{"a": "wire", "b": "wire"}, ModelOptions: packdecl.ModelFacts{
		"a": {"vendor": str("acme"), "name": str("Model")}, "b": {"vendor": str("acme"), "name": str("Model")},
	}}
	got := NormalizeLegacyProviderModels(input, nil)
	for _, diff := range got.Differences {
		if strings.Contains(diff.Reason, "complete model facts differ") {
			t.Fatalf("identical shared-wire facts were treated as conflict: %#v", got.Differences)
		}
	}
	if len(got.Snapshot.Models) != 2 || len(got.Snapshot.ModelOptions) != 2 {
		t.Fatalf("aliases were not preserved independently: %#v", got.Snapshot)
	}
}

func TestNormalizeLegacyProviderModelsPreservesNilEmptyRowsAndTombstonesOnRestore(t *testing.T) {
	str := "1.00"
	input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{}, ModelOptions: packdecl.ModelFacts{"nil-row": nil, "empty-row": {}, "tombstone": {"cost_input": nil, "future": &str}}}
	got := NormalizeLegacyProviderModels(input, nil)
	restored := got.RestoreLegacyProviderModels(input)
	if got.Snapshot.Models == nil || restored.Models == nil || restored.ModelOptions["nil-row"] != nil || restored.ModelOptions["empty-row"] == nil || restored.ModelOptions["tombstone"]["cost_input"] != nil || *restored.ModelOptions["tombstone"]["future"] != "1.00" {
		t.Fatalf("nil/empty/tombstone state changed: %#v / %#v", got.Snapshot, restored.ModelOptions)
	}
	if restored.ModelOptions["tombstone"]["future"] == input.ModelOptions["tombstone"]["future"] {
		t.Fatal("restored fact pointer is not independently owned")
	}
}

func TestNormalizeLegacyProviderModelsMatchesOldCompositionAndRegisteredOwnerRender(t *testing.T) {
	root := t.TempDir()
	manifest := `{"contributes":[
	 {"kind":"provider","name":"p","models":{"m":"m"},"model_options":{"m":{"vendor":"acme","name":"Model","context_window":"100000","reasoning":"false","input":"text,image","cost_input":"1.0","cost_output":"2","cost_cache_read":"0.1","cost_cache_write":"0"}}},
	 {"kind":"program","bin":"probe","via":"npm","package":"probe","protocols":["openai"],"provider_sets":true},
	 {"kind":"config","config":[{"agent":"probe","name":"snapshot","codec":"json","path":"~/.probe/snapshot.json","mode":"computed"}]}
	]}`
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	script := `yolo.derive("probe","snapshot",function(ctx)
	 local p=ctx.providers.p; local f=p.model_options.m
	 return {id=p.models.m,name=f.name,context=tonumber(f.context_window),reasoning=f.reasoning,input=f.input,price=tonumber(f.cost_input)}
	end)`
	if err := os.WriteFile(filepath.Join(root, "derive.lua"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	pack, problems := packload.LoadDir(root, "probe")
	if pack == nil || len(problems) > 0 {
		t.Fatalf("load actual legacy fixture: %#v", problems)
	}
	declarations := pack.Decl.Providers()
	if len(declarations) != 1 {
		t.Fatalf("legacy provider declarations=%#v", declarations)
	}
	normalized := NormalizeLegacyProviderModels(declarations[0], nil)
	if normalized.Canonical == nil || len(normalized.Differences) != 0 {
		t.Fatalf("fixture not representable: %#v", normalized.Differences)
	}

	// Baseline executes the same registered owner source against the actual old host composer output.
	legacy, err := packload.ComposeProviders(nil, []*packload.Pack{pack})
	if err != nil {
		t.Fatal(err)
	}
	legacyTables := map[string]any{"providers": jsonx.Plain(legacy)}
	baseline, err := (luahook.GopherLuaVM{}).Derive(packload.DeriveScript(pack), &luahook.DeriveCtx{Agent: "probe", Surface: "snapshot", Tables: toDeriveTables(t, legacyTables)})
	if err != nil {
		t.Fatalf("old composer owner render: %v", err)
	}

	// The candidate side invokes the provider semantic path and the owner's registered yolo.derive.
	input := ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{*normalized.Canonical}, Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{pack})}
	_, outputs, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "probe", Pack: pack, Surface: "snapshot"}})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("canonical registered owner render: %#v, %v", outputs, err)
	}
	if !reflect.DeepEqual(baseline, outputs[0].Value) {
		t.Fatalf("legacy compose+owner render differs: old=%#v canonical=%#v", baseline, outputs[0].Value)
	}
	if baseline["id"] != "m" || baseline["name"] != "Model" || baseline["input"] != "text,image" || baseline["context"] == nil || baseline["price"] == nil || baseline["reasoning"] != "false" {
		t.Fatalf("unexpected observable legacy output: %#v", baseline)
	}
	if strings.Contains(packload.DeriveScript(pack), "error(") {
		t.Fatal("fixture owner unexpectedly contains an error path")
	}
}

func TestNormalizeLegacyProviderModelsValidatesEveryFlatRowShape(t *testing.T) {
	validOffline := packdecl.ProviderModel{Vendor: "acme", Name: "Offline", Price: completePrice(1, 2, 3, 4)}
	for _, tc := range []struct {
		name  string
		facts packdecl.ModelFacts
	}{
		{name: "nil row", facts: packdecl.ModelFacts{"m": nil}},
		{name: "absent row", facts: nil},
		{name: "empty row", facts: packdecl.ModelFacts{"m": {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{"m": "m"}, ModelOptions: tc.facts}
			got := NormalizeLegacyProviderModels(input, nil)
			if got.Canonical != nil || !hasProviderDifference(got.Differences, "model_options.m.vendor") || !hasProviderDifference(got.Differences, "model_options.m.price") {
				t.Errorf("incomplete %s row must refuse maker/price rather than infer or invent: canonical=%#v differences=%#v", tc.name, got.Canonical, got.Differences)
			}
			restored := got.RestoreLegacyProviderModels(input)
			if !reflect.DeepEqual(restored.Models, input.Models) || !reflect.DeepEqual(restored.ModelOptions, input.ModelOptions) {
				t.Errorf("%s snapshot was not restored exactly: %#v", tc.name, restored)
			}
			valid := NormalizeLegacyProviderModels(input, map[string]packdecl.ProviderModel{"m": validOffline})
			if valid.Canonical == nil || len(valid.Differences) != 0 {
				t.Errorf("complete offline maker+price facts should support %s row: %#v", tc.name, valid.Differences)
			}
			invalid := NormalizeLegacyProviderModels(input, map[string]packdecl.ProviderModel{"m": {Vendor: "acme", Price: &packdecl.ProviderPrice{Input: float64p(1)}}})
			if invalid.Canonical != nil || !hasProviderDifference(invalid.Differences, "model_options.m.price") {
				t.Errorf("invalid offline facts bypassed final validation for %s: canonical=%#v differences=%#v", tc.name, invalid.Canonical, invalid.Differences)
			}
		})
	}
}

func TestNormalizeLegacyProviderModelsPreservesIdentityWirePresenceThroughShippedPi(t *testing.T) {
	piPack := shippedPiPack(t)
	oldPack, provider := loadLegacyPiProvider(t, "openai", `"models":{"global.openai.gpt-6-luna":"global.openai.gpt-6-luna"},"model_options":{"global.openai.gpt-6-luna":{"vendor":"openai","name":"GPT-6 Luna","cost_input":"1","cost_output":"2","cost_cache_read":"3","cost_cache_write":"4"}}`)
	oldTable, err := packload.ComposeProviders(nil, []*packload.Pack{oldPack})
	if err != nil {
		t.Fatal(err)
	}
	ctx := luahook.DeriveCtx{BuiltInProviders: packload.BuiltInProvidersFor([]*packload.Pack{piPack}, "pi")}
	ctx.Tables = toDeriveTables(t, map[string]any{"providers": jsonx.Plain(oldTable)})
	oldOutput := runShippedPi(t, piPack, "model-lists", oldTable, ctx)
	if registered, ok := oldOutput["providers"]; ok && registered != nil {
		t.Errorf("old identity-only openai declaration unexpectedly registers model-lists: %#v", oldOutput)
	}

	normalized := NormalizeLegacyProviderModels(provider, nil)
	if normalized.Canonical == nil || len(normalized.Differences) != 0 {
		t.Fatalf("identity declaration should be representable: %#v", normalized.Differences)
	}
	input := ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{*normalized.Canonical}, Selection: ActiveProviderSelection{Default: []string{"openai"}}, Receivers: ProviderReceivers([]*packload.Pack{piPack})}
	_, outputs, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "model-lists", Context: ctx}})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("project through shipped pi derive: outputs=%#v err=%v", outputs, err)
	}
	if !reflect.DeepEqual(oldOutput, outputs[0].Value) {
		t.Errorf("legacy ComposeProviders→shipped pi model-lists differs from typed projection: old=%#v projected=%#v", oldOutput, outputs[0].Value)
	}
	oldModels := runShippedPi(t, piPack, "models", oldTable, ctx)
	_, modelOutputs, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "models", Context: ctx}})
	if err != nil || len(modelOutputs) != 1 {
		t.Errorf("project complete shipped pi models surface: outputs=%#v err=%v", modelOutputs, err)
	} else if !reflect.DeepEqual(oldModels, modelOutputs[0].Value) {
		t.Errorf("legacy ComposeProviders→shipped pi models differs from typed projection: old=%#v projected=%#v", oldModels, modelOutputs[0].Value)
	}

	// Presence is distinct from value: an authored wire_id equal to id must still reach pi.
	authoredPack, authored := loadLegacyPiProvider(t, "openai", `"models":{"global.openai.gpt-6-luna":"global.openai.gpt-6-luna"},"model_options":{"global.openai.gpt-6-luna":{"vendor":"openai","wire_id":"global.openai.gpt-6-luna","cost_input":"1","cost_output":"2","cost_cache_read":"3","cost_cache_write":"4"}}`)
	authoredNormalized := NormalizeLegacyProviderModels(authored, nil)
	authoredOld, err := packload.ComposeProviders(nil, []*packload.Pack{authoredPack})
	if err != nil {
		t.Fatal(err)
	}
	oldAuthoredOutput := runShippedPi(t, piPack, "model-lists", authoredOld, ctx)
	_, authoredOutputs, err := ProjectProviderSemantics(ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{*authoredNormalized.Canonical}, Selection: ActiveProviderSelection{Default: []string{"openai"}}, Receivers: ProviderReceivers([]*packload.Pack{piPack})}, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "model-lists", Context: ctx}})
	if err != nil || len(authoredOutputs) != 1 {
		t.Fatalf("authored wire derive: %#v %v", authoredOutputs, err)
	}
	if !reflect.DeepEqual(oldAuthoredOutput, authoredOutputs[0].Value) {
		t.Errorf("authored identity wire_id presence differs in shipped pi output: old=%#v projected=%#v", oldAuthoredOutput, authoredOutputs[0].Value)
	}
	authoredModels := runShippedPi(t, piPack, "models", authoredOld, ctx)
	_, authoredModelOutputs, err := ProjectProviderSemantics(ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{*authoredNormalized.Canonical}, Selection: ActiveProviderSelection{Default: []string{"openai"}}, Receivers: ProviderReceivers([]*packload.Pack{piPack})}, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "models", Context: ctx}})
	if err != nil || len(authoredModelOutputs) != 1 {
		t.Errorf("project authored shipped pi models surface: outputs=%#v err=%v", authoredModelOutputs, err)
	} else if !reflect.DeepEqual(authoredModels, authoredModelOutputs[0].Value) {
		t.Errorf("authored wire_id differs in shipped pi models surface: old=%#v projected=%#v", authoredModels, authoredModelOutputs[0].Value)
	}
}

func TestNormalizeLegacyProviderModelsDeeplyOwnsBothReturnedDeclarations(t *testing.T) {
	price := completePrice(1, 2, 3, 4)
	flag := true
	enforce := false
	opaque := 7
	channel := make(chan int)
	input := packdecl.ProviderContribution{
		Name: "p", Alias: "alias", Model: "m", Variants: map[string]packdecl.ProviderVariant{"v": {
			Model: "m", Options: map[string]packdecl.OptionDefault{"temperature": {Defaulted: true, Value: "0.2"}},
			Subagents:     map[string]any{"roles": map[string]any{"worker": "m"}, "opaque": &opaque, "channel": channel, "nil": nil},
			EnforceModels: &enforce, Capabilities: []string{}, Protocols: []string{"openai"},
		}}, VariantOrder: []string{}, EveryModel: &packdecl.ProviderModel{Vendor: "acme", Reasoning: &flag, Price: price, Accepts: []string{}},
		ModelRows: map[string]packdecl.ProviderModel{"m": {Vendor: "acme", Price: price, Accepts: []string{}}}, ModelOrder: []string{},
		Endpoints:     map[string]packdecl.ProviderEndpoint{"openai": {BaseURL: "https://example.test/v1", WireAPI: "openai-responses"}},
		APIKeyEnvName: packdecl.EnvNames{}, RegionEnvName: []string{}, RegionFile: &packdecl.RegionFile{Path: ".aws/config", Key: "region"},
		Models: nil, ModelOptions: nil, Options: map[string]packdecl.OptionDefault{"context_window": {Defaulted: true, Value: "500"}}, Capabilities: []string{},
	}
	got := NormalizeLegacyProviderModels(input, nil)
	if got.Canonical == nil {
		t.Fatalf("canonical clone fixture was refused: %#v", got.Differences)
	}
	first := got.RestoreLegacyProviderModels(input)
	second := got.RestoreLegacyProviderModels(input)
	if got.Canonical.Capabilities == nil || got.Canonical.VariantOrder == nil || got.Canonical.ModelOrder == nil || got.Canonical.RegionEnvName == nil || got.Canonical.APIKeyEnvName == nil || got.Canonical.Variants["v"].Capabilities == nil {
		t.Errorf("explicit empty slices collapsed to nil: %#v", got.Canonical)
	}

	variant := got.Canonical.Variants["v"]
	variant.Options["temperature"] = packdecl.OptionDefault{Defaulted: true, Value: "9"}
	variant.Subagents["roles"].(map[string]any)["worker"] = "changed"
	*variant.Subagents["opaque"].(*int) = 9
	variant.EnforceModels = boolPointer(true)
	variant.Capabilities = []string{"changed"}
	variant.Protocols[0] = "changed"
	got.Canonical.Variants["v"] = variant
	got.Canonical.VariantOrder = append(got.Canonical.VariantOrder, "changed")
	got.Canonical.RegionFile.Path = "changed"
	got.Canonical.Options["context_window"] = packdecl.OptionDefault{Defaulted: true, Value: "9"}
	got.Canonical.Endpoints["openai"] = packdecl.ProviderEndpoint{BaseURL: "changed"}
	got.Canonical.ModelOrder = append(got.Canonical.ModelOrder, "changed")
	modelRow := got.Canonical.ModelRows["m"]
	modelRow.Accepts = []string{"image"}
	*modelRow.Price.Input = 9
	got.Canonical.ModelRows["m"] = modelRow
	*got.Canonical.EveryModel.Reasoning = false
	*got.Canonical.EveryModel.Price.Output = 9
	got.Canonical.Capabilities = []string{"changed"}
	got.Canonical.APIKeyEnvName = packdecl.EnvNames{"changed"}
	got.Canonical.RegionEnvName = []string{"changed"}
	if got.Canonical.Variants["v"].Subagents["channel"] != channel || got.Canonical.Variants["v"].Subagents["nil"] != nil {
		t.Error("non-JSON or nil subagent values were not preserved")
	}
	if input.Variants["v"].Options["temperature"].Value != "0.2" || input.Variants["v"].Subagents["roles"].(map[string]any)["worker"] != "m" || opaque != 7 || *input.Variants["v"].EnforceModels || input.Variants["v"].Capabilities == nil || input.Variants["v"].Protocols[0] != "openai" || len(input.VariantOrder) != 0 || input.RegionFile.Path != ".aws/config" || input.Options["context_window"].Value != "500" || input.Endpoints["openai"].BaseURL != "https://example.test/v1" || input.ModelRows["m"].Accepts == nil || *input.ModelRows["m"].Price.Input != 1 || *input.EveryModel.Reasoning != true || *input.EveryModel.Price.Output != 2 || input.Capabilities == nil || len(input.APIKeyEnvName) != 0 || input.RegionEnvName == nil {
		t.Errorf("canonical return aliases original declaration: %#v", input)
	}
	if first.Variants["v"].Options["temperature"].Value != "0.2" || second.Variants["v"].Options["temperature"].Value != "0.2" || *first.ModelRows["m"].Price.Input != 1 || *second.ModelRows["m"].Price.Input != 1 {
		t.Errorf("canonical return aliases one or both restore returns: first=%#v second=%#v", first, second)
	}
	first.Variants["v"].Subagents["roles"].(map[string]any)["worker"] = "restore-mutated"
	first.RegionFile.Path = "restore-mutated"
	first.Capabilities = []string{"restore-mutated"}
	if input.Variants["v"].Subagents["roles"].(map[string]any)["worker"] != "m" || second.Variants["v"].Subagents["roles"].(map[string]any)["worker"] != "m" || got.Canonical.Variants["v"].Subagents["roles"].(map[string]any)["worker"] != "changed" || input.RegionFile.Path != ".aws/config" || second.RegionFile.Path != ".aws/config" || got.Canonical.RegionFile.Path != "changed" || input.Capabilities == nil || len(input.Capabilities) != 0 || second.Capabilities == nil || len(second.Capabilities) != 0 || got.Canonical.Capabilities[0] != "changed" {
		t.Errorf("restore return is not independent from input/canonical/sibling returns")
	}
}

func TestNormalizeLegacyProviderModelsPreservesCapabilityReplacementPresenceForReceiver(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		providerCapabilities []string
		variantCapabilities  []string
		selector             string
		want                 []string
	}{
		{name: "inherit absent provider and variant capabilities", providerCapabilities: nil, variantCapabilities: nil, selector: "p/v", want: nil},
		{name: "preserve explicitly empty provider capabilities", providerCapabilities: []string{}, variantCapabilities: nil, selector: "p", want: []string{}},
		{name: "inherit provider capabilities", providerCapabilities: []string{"provider-capability"}, variantCapabilities: nil, selector: "p/v", want: []string{"provider-capability"}},
		{name: "replace with explicitly empty variant capabilities", providerCapabilities: []string{"provider-capability"}, variantCapabilities: []string{}, selector: "p/v", want: []string{}},
		{name: "replace with present variant capabilities", providerCapabilities: []string{"provider-capability"}, variantCapabilities: []string{"variant-capability"}, selector: "p/v", want: []string{"variant-capability"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declaration := packdecl.ProviderContribution{Name: "p", Capabilities: tc.providerCapabilities, Variants: map[string]packdecl.ProviderVariant{"v": {Capabilities: tc.variantCapabilities}}}
			normalized := NormalizeLegacyProviderModels(declaration, nil)
			if normalized.Canonical == nil {
				t.Fatalf("canonical clone failed: %#v", normalized.Differences)
			}
			resolved, err := ResolveActiveProviders([]packdecl.ProviderContribution{*normalized.Canonical}, ActiveProviderSelection{Default: []string{tc.selector}}, []ProviderReceiver{{Bin: "pi"}})
			if err != nil {
				t.Fatal(err)
			}
			got := resolved.ByReceiver["pi"][0].LegacyCompatibility().Capabilities
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("receiver capabilities = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestNormalizeLegacyProviderModelsRefusesUnresolvedHybridPrecedence(t *testing.T) {
	valid := packdecl.ProviderModel{Vendor: "acme", Price: completePrice(1, 2, 3, 4)}
	t.Run("empty legacy map and canonical rows coexist", func(t *testing.T) {
		input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{}, ModelRows: map[string]packdecl.ProviderModel{"existing": valid}, ModelOrder: []string{"existing"}}
		got := NormalizeLegacyProviderModels(input, nil)
		if got.Canonical != nil || !hasProviderDifference(got.Differences, "model_rows") {
			t.Errorf("explicitly empty legacy Models silently replaced canonical rows: %#v", got)
		}
		restored := got.RestoreLegacyProviderModels(input)
		if !reflect.DeepEqual(restored.Models, input.Models) || !reflect.DeepEqual(restored.ModelRows, input.ModelRows) || !reflect.DeepEqual(restored.ModelOrder, input.ModelOrder) {
			t.Errorf("hybrid refusal changed raw rows/order: %#v", restored)
		}
	})
	t.Run("every model facts are not old composer facts", func(t *testing.T) {
		falseValue := false
		input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{"m": "m"}, EveryModel: &packdecl.ProviderModel{Reasoning: &falseValue}, ModelOptions: packdecl.ModelFacts{"m": {"vendor": stringPointer("acme"), "cost_input": stringPointer("1"), "cost_output": stringPointer("2"), "cost_cache_read": stringPointer("3"), "cost_cache_write": stringPointer("4")}}}
		got := NormalizeLegacyProviderModels(input, nil)
		if got.Canonical != nil || !hasProviderDifference(got.Differences, "every_model") {
			t.Errorf("retained every_model silently activates facts old composer did not emit: %#v", got)
		}
		if !reflect.DeepEqual(got.RestoreLegacyProviderModels(input).ModelOptions, input.ModelOptions) || got.Snapshot.ModelOptions["m"]["reasoning"] != nil {
			t.Errorf("hybrid difference changed original declaration/snapshot: %#v", got)
		}
	})
	t.Run("ordered supported nonhybrid control", func(t *testing.T) {
		input := packdecl.ProviderContribution{Name: "p", Models: map[string]string{"z": "z", "a": "a"}, ModelOptions: packdecl.ModelFacts{
			"z": {"vendor": stringPointer("acme"), "order": stringPointer("0"), "cost_input": stringPointer("1"), "cost_output": stringPointer("2"), "cost_cache_read": stringPointer("3"), "cost_cache_write": stringPointer("4")},
			"a": {"vendor": stringPointer("acme"), "order": stringPointer("1"), "cost_input": stringPointer("1"), "cost_output": stringPointer("2"), "cost_cache_read": stringPointer("3"), "cost_cache_write": stringPointer("4")},
		}}
		got := NormalizeLegacyProviderModels(input, nil)
		if got.Canonical == nil || !reflect.DeepEqual(got.Canonical.ModelOrder, []string{"z", "a"}) {
			t.Errorf("supported [z,a] control failed: %#v %#v", got.Canonical, got.Differences)
		}
	})
}

func TestNormalizeLegacyProviderModelsRefusesZeroContextThatChangesPiFallback(t *testing.T) {
	piPack := shippedPiPack(t)
	oldPack, provider := loadLegacyPiProvider(t, "p", `"options":{"context_window":"500"},"endpoints":{"openai":{"base_url":"https://example.test/v1","wire_api":"openai-responses"}},"models":{"m":"m"},"model_options":{"m":{"vendor":"acme","context_window":"0","cost_input":"1","cost_output":"2","cost_cache_read":"3","cost_cache_write":"4"}}`)
	got := NormalizeLegacyProviderModels(provider, nil)
	if got.Canonical != nil || !hasProviderDifference(got.Differences, "model_options.m.context_window") {
		t.Errorf("authored zero context must be an explicit nonrepresentable difference: %#v", got)
	}
	if got.RestoreLegacyProviderModels(provider).ModelOptions["m"]["context_window"] == nil || *got.RestoreLegacyProviderModels(provider).ModelOptions["m"]["context_window"] != "0" {
		t.Errorf("refusal did not preserve original zero spelling")
	}
	legacyTable, err := packload.ComposeProviders(nil, []*packload.Pack{oldPack})
	if err != nil {
		t.Fatal(err)
	}
	ctx := luahook.DeriveCtx{BuiltInProviders: packload.BuiltInProvidersFor([]*packload.Pack{piPack}, "pi")}
	ctx.Tables = toDeriveTables(t, map[string]any{"providers": jsonx.Plain(legacyTable)})
	oldOutput := runShippedPi(t, piPack, "models", legacyTable, ctx)
	attempted := provider
	attempted.Models, attempted.ModelOptions = nil, nil
	attempted.ModelRows = map[string]packdecl.ProviderModel{"m": {Vendor: "acme", Context: 0, Price: completePrice(1, 2, 3, 4)}}
	attempted.ModelOrder = []string{"m"}
	_, outputs, err := ProjectProviderSemantics(ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{attempted}, Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{piPack})}, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "models", Context: ctx}})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("attempted typed pi render: %#v %v", outputs, err)
	}
	if reflect.DeepEqual(oldOutput, outputs[0].Value) {
		t.Errorf("actual pi zero→fallback behavior unexpectedly equal: old=%#v typed=%#v", oldOutput, outputs[0].Value)
	}
	if piRenderedContext(oldOutput) != "0" || piRenderedContext(outputs[0].Value) != "500" {
		t.Errorf("expected actual shipped pi context 0→500 fallback, got old=%#v typed=%#v", oldOutput, outputs[0].Value)
	}
}

func TestNormalizeLegacyProviderModelsUsesPiExactReasoningGrammar(t *testing.T) {
	piPack := shippedPiPack(t)
	for _, spelling := range []string{"1", "TRUE", "t", "true", "false"} {
		t.Run(spelling, func(t *testing.T) {
			extra := `"endpoints":{"openai":{"base_url":"https://example.test/v1","wire_api":"openai-responses"}},"models":{"m":"m"},"model_options":{"m":{"vendor":"acme","reasoning":"` + spelling + `","cost_input":"1","cost_output":"2","cost_cache_read":"3","cost_cache_write":"4"}}`
			oldPack, provider := loadLegacyPiProvider(t, "p", extra)
			got := NormalizeLegacyProviderModels(provider, nil)
			oldTable, err := packload.ComposeProviders(nil, []*packload.Pack{oldPack})
			if err != nil {
				t.Fatal(err)
			}
			ctx := luahook.DeriveCtx{BuiltInProviders: packload.BuiltInProvidersFor([]*packload.Pack{piPack}, "pi")}
			ctx.Tables = toDeriveTables(t, map[string]any{"providers": jsonx.Plain(oldTable)})
			oldOutput := runShippedPi(t, piPack, "models", oldTable, ctx)
			if spelling == "1" || spelling == "TRUE" || spelling == "t" {
				if got.Canonical != nil || !hasProviderDifference(got.Differences, "model_options.m.reasoning") {
					t.Errorf("unsupported Pi boolean spelling %q lacks explicit difference: %#v", spelling, got)
				}
				if got.RestoreLegacyProviderModels(provider).ModelOptions["m"]["reasoning"] == nil || *got.RestoreLegacyProviderModels(provider).ModelOptions["m"]["reasoning"] != spelling {
					t.Errorf("raw reasoning spelling was not restored")
				}
				attempted := provider
				attempted.Models, attempted.ModelOptions = nil, nil
				b := true
				attempted.ModelRows = map[string]packdecl.ProviderModel{"m": {Vendor: "acme", Reasoning: &b, Price: completePrice(1, 2, 3, 4)}}
				attempted.ModelOrder = []string{"m"}
				_, outputs, err := ProjectProviderSemantics(ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{attempted}, Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{piPack})}, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "models", Context: ctx}})
				if err != nil || len(outputs) != 1 {
					t.Fatalf("attempted bool render: %#v %v", outputs, err)
				}
				if reflect.DeepEqual(oldOutput, outputs[0].Value) || piRenderedReasoning(oldOutput) != nil || piRenderedReasoning(outputs[0].Value) != true {
					t.Errorf("actual pi boolean grammar difference missing: old=%#v projected=%#v", oldOutput, outputs[0].Value)
				}
			} else {
				if got.Canonical == nil || len(got.Differences) != 0 {
					t.Errorf("exact Pi boolean %q should remain supported: %#v", spelling, got.Differences)
				}
				input := ProviderSemanticInput{Declarations: []packdecl.ProviderContribution{*got.Canonical}, Selection: ActiveProviderSelection{Default: []string{"p"}}, Receivers: ProviderReceivers([]*packload.Pack{piPack})}
				_, outputs, err := ProjectProviderSemantics(input, []ProviderDeriveRequest{{Receiver: "pi", Pack: piPack, Surface: "models", Context: ctx}})
				if err != nil || len(outputs) != 1 {
					t.Fatalf("supported boolean render: %#v %v", outputs, err)
				}
				if !reflect.DeepEqual(oldOutput, outputs[0].Value) {
					t.Errorf("exact Pi boolean %q changed owner output: old=%#v typed=%#v", spelling, oldOutput, outputs[0].Value)
				}
			}
		})
	}
}

func hasProviderDifference(differences []ProviderModelDifference, path string) bool {
	for _, difference := range differences {
		if difference.Path == path {
			return true
		}
	}
	return false
}

func shippedPiPack(t *testing.T) *packload.Pack {
	t.Helper()
	for _, pack := range packload.Embedded() {
		if pack.Name == "pi" {
			t.Cleanup(packload.ReleaseEmbedded)
			if packload.DeriveScript(pack) == "" {
				t.Fatal("shipped pi derive is missing")
			}
			return pack
		}
	}
	t.Fatal("shipped pi pack missing")
	return nil
}

func loadLegacyPiProvider(t *testing.T, name, providerFields string) (*packload.Pack, packdecl.ProviderContribution) {
	t.Helper()
	root := t.TempDir()
	quotedName, err := json.Marshal(name)
	if err != nil {
		t.Fatal(err)
	}
	manifest := `{"contributes":[{"kind":"provider","name":` + string(quotedName) + `,` + providerFields + `}]}`
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	pack, problems := packload.LoadDir(root, "legacy-fixture")
	if pack == nil || len(problems) > 0 {
		t.Fatalf("load old provider fixture: pack=%#v problems=%#v manifest=%s", pack, problems, manifest)
	}
	providers := pack.Decl.Providers()
	if len(providers) != 1 {
		t.Fatalf("legacy provider declarations=%#v", providers)
	}
	return pack, providers[0]
}

func runShippedPi(t *testing.T, pack *packload.Pack, surface string, providers *jsonx.OrderedMap, context luahook.DeriveCtx) map[string]any {
	t.Helper()
	context.Agent, context.Surface = "pi", surface
	context.Tables = toDeriveTables(t, map[string]any{"providers": jsonx.Plain(providers)})
	output, err := (luahook.GopherLuaVM{}).Derive(packload.DeriveScript(pack), &context)
	if err != nil {
		t.Fatalf("shipped pi %s derive: %v", surface, err)
	}
	return output
}

func piRenderedContext(output map[string]any) string {
	providers, _ := output["providers"].(map[string]any)
	for _, provider := range providers {
		entry, _ := provider.(map[string]any)
		models, _ := entry["models"].([]any)
		for _, model := range models {
			row, _ := model.(map[string]any)
			if row["id"] == "m" {
				return fmt.Sprint(row["contextWindow"])
			}
		}
	}
	return ""
}

func piRenderedReasoning(output map[string]any) any {
	providers, _ := output["providers"].(map[string]any)
	for _, provider := range providers {
		entry, _ := provider.(map[string]any)
		models, _ := entry["models"].([]any)
		for _, model := range models {
			row, _ := model.(map[string]any)
			if row["id"] == "m" {
				return row["reasoning"]
			}
		}
	}
	return nil
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func float64p(value float64) *float64    { return &value }

func toDeriveTables(t *testing.T, values map[string]any) map[string]map[string]any {
	t.Helper()
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for k, v := range decoded {
		if row, ok := v.(map[string]any); ok {
			out[k] = row
		}
	}
	return out
}
