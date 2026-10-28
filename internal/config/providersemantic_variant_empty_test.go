package config

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestReadableJSONCProviderEmptyCapabilitiesRemainAdditiveAtSelectedConsumer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigLegacyJSONCName), `{"include_if_found":["extra.jsonc"],"providers":{"p":{"model":"base-model","capabilities":["provider-capability"]}},"active_provider":{"default":"p"}}`)
	write(t, filepath.Join(userDir, "extra.jsonc"), `{"providers":{"p":{"capabilities":[]}}}`)

	config, err := LoadReadableUserScopeConfig(true, nil)
	if err != nil {
		t.Fatalf("public legacy JSONC user-scope read: %v", err)
	}
	inputs, err := BuildReadableSemanticInputs(ReadableSemanticInputSource{
		Config: config, Packs: selectedEmbeddedPacks(t, "pi"),
		Catalog: map[string]map[string]packdecl.ProviderModel{},
	})
	if err != nil {
		t.Fatalf("provider declaration and configured consumer composition: %v", err)
	}
	selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
	if len(selected) != 1 || !reflect.DeepEqual(selected[0].Capabilities, []string{"provider-capability"}) {
		t.Fatalf("legacy JSONC empty capabilities stopped being additive: %#v", selected)
	}
}

// This caller-level case records the behavior when [] is authored in a later TOML layer.
// Variant capabilities have no ReadableRemovalPolicy, so the public reader currently treats
// the empty list additively; this is distinct from the single-file typed replacement above.
func TestReadableTOMLVariantCapabilitiesEmptyLaterLayerRemainsAdditive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "[providers.p]\nmodel = 'base-model'\n[providers.p.variants.v]\nmodel = 'variant-model'\ncapabilities = ['variant-capability']\n[active_provider]\ndefault = 'p/v'\n")
	write(t, filepath.Join(userDir, "config.d", "01-empty.toml"), "[providers.p.variants.v]\ncapabilities = []\n")

	config, err := LoadReadableUserScopeConfig(true, nil)
	if err != nil {
		t.Fatalf("public layered TOML user-scope read: %v", err)
	}
	inputs, err := BuildReadableSemanticInputs(ReadableSemanticInputSource{
		Config: config, Packs: selectedEmbeddedPacks(t, "pi"),
		Catalog: map[string]map[string]packdecl.ProviderModel{},
	})
	if err != nil {
		t.Fatalf("provider declaration and configured consumer composition: %v", err)
	}
	selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
	if len(selected) != 1 || !reflect.DeepEqual(selected[0].Capabilities, []string{"variant-capability"}) {
		t.Fatalf("variant list merge = %#v, want current additive behavior without a registered reset policy", selected)
	}
}

func TestReadableTOMLProviderCapabilityResetAndUnionReachInheritedVariant(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fragment   string
		want       []string
		wantParsed []any
	}{
		{name: "missing inherits", fragment: "[install]\nnix = ['just']\n", want: []string{"provider-capability"}, wantParsed: []any{"provider-capability"}},
		{name: "schema-authorized empty resets", fragment: "[providers.p]\ncapabilities = []\n", want: []string{}, wantParsed: []any{}},
		{name: "nonempty unions in source order", fragment: "[providers.p]\ncapabilities = ['later-capability']\n", want: []string{"provider-capability", "later-capability"}, wantParsed: []any{"provider-capability", "later-capability"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			userDir := filepath.Join(home, ".config", "yolo-jail")
			write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "[providers.p]\nmodel = 'base-model'\ncapabilities = ['provider-capability']\n[providers.p.variants.v]\nmodel = 'variant-model'\n[active_provider]\ndefault = 'p/v'\n")
			write(t, filepath.Join(userDir, "config.d", "01-later.toml"), tc.fragment)

			config, err := LoadReadableUserScopeConfig(true, nil)
			if err != nil {
				t.Fatalf("public layered TOML user-scope read: %v", err)
			}
			providers, _ := config.Get("providers")
			provider, _ := providers.(*jsonx.OrderedMap).Get("p")
			raw, present := provider.(*jsonx.OrderedMap).Get("capabilities")
			if !present || !reflect.DeepEqual(raw, tc.wantParsed) {
				t.Fatalf("parsed provider capabilities = %#v (present=%t), want %#v", raw, present, tc.wantParsed)
			}
			inputs, err := BuildReadableSemanticInputs(ReadableSemanticInputSource{
				Config: config, Packs: selectedEmbeddedPacks(t, "pi"),
				Catalog: map[string]map[string]packdecl.ProviderModel{},
			})
			if err != nil {
				t.Fatalf("provider declaration and configured consumer composition: %v", err)
			}
			selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
			if len(selected) != 1 || !reflect.DeepEqual(selected[0].Capabilities, tc.want) {
				t.Fatalf("selected provider/variant capabilities = %#v, want %#v", selected, tc.want)
			}
		})
	}
}

func TestReadableTOMLVariantCapabilitiesReplaceProviderCapabilitiesAtSelectedConsumer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		variantField string
		want         []string
		wantNil      bool
	}{
		{name: "missing inherits", want: []string{"provider-capability"}},
		{name: "explicit empty replaces", variantField: "capabilities = []\n", want: []string{}, wantNil: false},
		{name: "nonempty replaces", variantField: "capabilities = ['variant-capability']\n", want: []string{"variant-capability"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			configPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
			write(t, configPath, "[providers.p]\nmodel = 'base-model'\ncapabilities = ['provider-capability']\noptions = { mode = 'default' }\n"+
				"[providers.p.variants.v]\nmodel = 'variant-model'\nvia = 'wire-bridge'\noptions = { mode = 'fast' }\n"+tc.variantField+
				"[active_provider]\ndefault = 'p/v'\n")

			config, err := LoadReadableUserScopeConfig(true, nil)
			if err != nil {
				t.Fatalf("public readable TOML user-scope read: %v", err)
			}
			if config == nil {
				t.Fatal("public TOML read returned nil config")
			}
			providers, _ := config.Get("providers")
			provider, _ := providers.(*jsonx.OrderedMap).Get("p")
			variants, _ := provider.(*jsonx.OrderedMap).Get("variants")
			variantValue, _ := variants.(*jsonx.OrderedMap).Get("v")
			variantMap := variantValue.(*jsonx.OrderedMap)
			rawCapabilities, present := variantMap.Get("capabilities")
			switch tc.name {
			case "missing inherits":
				if present {
					t.Fatalf("parsed missing capabilities became present: %#v", rawCapabilities)
				}
			case "explicit empty replaces":
				list, ok := rawCapabilities.([]any)
				if !present || !ok || list == nil || len(list) != 0 {
					t.Fatalf("parsed explicit empty capabilities = %#v (present=%t), want non-nil empty list", rawCapabilities, present)
				}
			case "nonempty replaces":
				if !present || !reflect.DeepEqual(rawCapabilities, []any{"variant-capability"}) {
					t.Fatalf("parsed nonempty capabilities = %#v (present=%t)", rawCapabilities, present)
				}
			}
			inputs, err := BuildReadableSemanticInputs(ReadableSemanticInputSource{
				Config: config, Packs: selectedEmbeddedPacks(t, "pi", "wire-bridge"),
				Catalog: map[string]map[string]packdecl.ProviderModel{},
			})
			if err != nil {
				t.Fatalf("provider declaration and configured consumer composition: %v", err)
			}
			if len(inputs.ProviderInput.Declarations) != 1 {
				t.Fatalf("provider declarations = %#v, want one decoded TOML provider", inputs.ProviderInput.Declarations)
			}
			variant := inputs.ProviderInput.Declarations[0].Variants["v"]
			if tc.name == "explicit empty replaces" && variant.Capabilities == nil {
				t.Fatalf("TOML provider declaration lost explicit empty variant capabilities: %#v", variant)
			}
			if tc.name == "missing inherits" && variant.Capabilities != nil {
				t.Fatalf("missing variant capabilities became authored: %#v", variant.Capabilities)
			}
			if inputs.ProviderConsumer.Readable == nil {
				t.Fatalf("configured provider consumer was not selected: %#v", inputs.ProviderConsumer)
			}
			selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
			if len(selected) != 1 {
				t.Fatalf("selected providers = %#v, want p/v", selected)
			}
			got := selected[0].Capabilities
			if (got == nil) != tc.wantNil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("selected variant capabilities = %#v, want %#v (nil=%t)", got, tc.want, tc.wantNil)
			}
			if selected[0].Via != "wire-bridge" || selected[0].Model != "variant-model" || selected[0].Options["mode"].Value != "fast" {
				t.Fatalf("neighboring variant facts were lost at configured consumer: %#v", selected[0])
			}
			compat := selected[0].LegacyCompatibility()
			if (compat.Capabilities == nil) != tc.wantNil || !reflect.DeepEqual(compat.Capabilities, tc.want) || compat.Profile.Via != "wire-bridge" || compat.Profile.Options["mode"] != "fast" {
				t.Fatalf("compatibility projection = %#v, want caps %#v and preserved via/options", compat, tc.want)
			}
		})
	}
}
