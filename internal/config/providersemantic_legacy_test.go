package config

import (
	"encoding/json"
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
