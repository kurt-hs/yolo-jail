package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestReadableJSONCProviderEmptyCapabilitiesRemainAdditiveAtSelectedConsumer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigLegacyJSONCName), `{"include_if_found":["extra.jsonc"],"providers":{"p":{"model":"base-model","capabilities":["provider-capability"],"variants":{"v":{"model":"variant-model","capabilities":["variant-capability"]}}}},"active_provider":{"default":"p/v"}}`)
	write(t, filepath.Join(userDir, "extra.jsonc"), `{"providers":{"p":{"capabilities":[],"variants":{"v":{"capabilities":[]}}}}}`)

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
	if inputs.ProviderConsumer.Readable == nil {
		t.Fatalf("legacy JSONC provider consumer was not selected: %#v", inputs.ProviderConsumer)
	}
	selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
	if len(selected) != 1 || !reflect.DeepEqual(selected[0].Capabilities, []string{"variant-capability"}) || selected[0].Capabilities == nil {
		t.Fatalf("legacy JSONC empty variant capabilities stopped being additive: %#v", selected)
	}
	compat := selected[0].LegacyCompatibility()
	if compat.Capabilities == nil || !reflect.DeepEqual(compat.Capabilities, []string{"variant-capability"}) {
		t.Fatalf("legacy JSONC compatibility capabilities = %#v", compat.Capabilities)
	}
}

func TestReadableTOMLVariantCapabilitiesResetAndUnionReachSelectedConsumer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fragment   string
		want       []string
		wantParsed []any
		wantMode   string
	}{
		{name: "missing fragment inherits", fragment: "[providers.\"p.with.dot\".variants.\"v.with.dot\"]\noptions = { mode = 'fast' }\n", want: []string{"variant-capability"}, wantParsed: []any{"variant-capability"}, wantMode: "fast"},
		{name: "explicit empty resets", fragment: "[providers.\"p.with.dot\".variants.\"v.with.dot\"]\ncapabilities = []\n", want: []string{}, wantParsed: []any{}, wantMode: "base"},
		{name: "nonempty unions in layer order", fragment: "[providers.\"p.with.dot\".variants.\"v.with.dot\"]\ncapabilities = ['later-capability']\n", want: []string{"variant-capability", "later-capability"}, wantParsed: []any{"variant-capability", "later-capability"}, wantMode: "base"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			userDir := filepath.Join(home, ".config", "yolo-jail")
			write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "[providers.\"p.with.dot\"]\nmodel = 'base-model'\ncapabilities = ['provider-capability']\noptions = { mode = 'default' }\n"+
				"[providers.\"p.with.dot\".variants.\"v.with.dot\"]\nmodel = 'variant-model'\nvia = 'wire-bridge'\ncapabilities = ['variant-capability']\noptions = { mode = 'base' }\n"+
				"[active_provider]\ndefault = 'p.with.dot/v.with.dot'\n")
			write(t, filepath.Join(userDir, "config.d", "01-later.toml"), tc.fragment)

			config, err := LoadReadableUserScopeConfig(true, nil)
			if err != nil {
				t.Fatalf("public layered TOML user-scope read: %v", err)
			}
			providers, _ := config.Get("providers")
			provider, present := providers.(*jsonx.OrderedMap).Get("p.with.dot")
			if !present {
				t.Fatalf("literal dotted provider key was split or lost: %#v", providers)
			}
			variants, _ := provider.(*jsonx.OrderedMap).Get("variants")
			variantValue, present := variants.(*jsonx.OrderedMap).Get("v.with.dot")
			if !present {
				t.Fatalf("literal dotted variant key was split or lost: %#v", variants)
			}
			rawCapabilities, present := variantValue.(*jsonx.OrderedMap).Get("capabilities")
			if !present || !reflect.DeepEqual(rawCapabilities, tc.wantParsed) {
				t.Fatalf("parsed layered variant capabilities = %#v (present=%t), want %#v", rawCapabilities, present, tc.wantParsed)
			}
			inputs, err := BuildReadableSemanticInputs(ReadableSemanticInputSource{
				Config: config, Packs: selectedEmbeddedPacks(t, "pi", "wire-bridge"),
				Catalog: map[string]map[string]packdecl.ProviderModel{},
			})
			if err != nil {
				t.Fatalf("provider declaration and configured consumer composition: %v", err)
			}
			declaration := declarationNamed(t, inputs.ProviderInput.Declarations, "p.with.dot")
			if inputs.ProviderConsumer.Readable == nil {
				t.Fatalf("configured provider consumer was not selected: %#v", inputs.ProviderConsumer)
			}
			variant := declaration.Variants["v.with.dot"]
			if !reflect.DeepEqual(variant.Capabilities, tc.want) || variant.Capabilities == nil {
				t.Fatalf("typed variant capabilities = %#v, want non-nil %#v", variant.Capabilities, tc.want)
			}
			selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
			if len(selected) != 1 || selected[0].Target != (ProviderTarget{Provider: "p.with.dot", Variant: "v.with.dot"}) {
				t.Fatalf("selected provider/variant = %#v", selected)
			}
			if selected[0].Capabilities == nil || !reflect.DeepEqual(selected[0].Capabilities, tc.want) {
				t.Fatalf("selected capabilities = %#v, want non-nil %#v", selected[0].Capabilities, tc.want)
			}
			compat := selected[0].LegacyCompatibility()
			if compat.Capabilities == nil || !reflect.DeepEqual(compat.Capabilities, tc.want) || compat.Profile.Via != "wire-bridge" || compat.Profile.Options["mode"] != tc.wantMode {
				t.Fatalf("compatibility projection = %#v, want non-nil capabilities %#v, mode %q and preserved via", compat, tc.want, tc.wantMode)
			}
		})
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

func TestReadableTOMLVariantCapabilitiesRemainUserScopeStringLists(t *testing.T) {
	t.Run("workspace refuses the user-only field", func(t *testing.T) {
		workspace := t.TempDir()
		write(t, filepath.Join(workspace, paths.WorkspaceConfigTOMLName), "[providers.p.variants.v]\nmodel = 'variant-model'\ncapabilities = ['capability']\n")
		if _, err := LoadReadableWorkspaceConfig(workspace, true, nil); err == nil || !strings.Contains(err.Error(), "user-scope only") {
			t.Fatalf("workspace variant capabilities result = %v, want a user-scope refusal", err)
		}
	})
	t.Run("elements must be strings", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		write(t, filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), "[providers.p.variants.v]\nmodel = 'variant-model'\ncapabilities = [7]\n")
		if _, err := LoadReadableUserScopeConfig(true, nil); err == nil || !strings.Contains(err.Error(), "expected a string") {
			t.Fatalf("user variant capabilities result = %v, want a string-list element refusal", err)
		}
	})
}

func TestReadableTOMLVariantCapabilitiesReplaceProviderCapabilitiesAtSelectedConsumer(t *testing.T) {
	for _, tc := range []struct {
		name           string
		variantField   string
		want           []string
		wantVariant    []string
		wantVariantNil bool
	}{
		{name: "missing inherits", want: []string{"provider-capability"}, wantVariantNil: true},
		{name: "explicit empty replaces", variantField: "capabilities = []\n", want: []string{}, wantVariant: []string{}},
		{name: "nonempty replaces", variantField: "capabilities = ['variant-capability']\n", want: []string{"variant-capability"}, wantVariant: []string{"variant-capability"}},
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
			variant := declarationNamed(t, inputs.ProviderInput.Declarations, "p").Variants["v"]
			if (variant.Capabilities == nil) != tc.wantVariantNil || !reflect.DeepEqual(variant.Capabilities, tc.wantVariant) {
				t.Fatalf("TOML provider declaration variant capabilities = %#v, want %#v (nil=%t)", variant.Capabilities, tc.wantVariant, tc.wantVariantNil)
			}
			if inputs.ProviderConsumer.Readable == nil {
				t.Fatalf("configured provider consumer was not selected: %#v", inputs.ProviderConsumer)
			}
			selected := inputs.ProviderConsumer.Readable.Selection.ByReceiver["pi"]
			if len(selected) != 1 {
				t.Fatalf("selected providers = %#v, want p/v", selected)
			}
			got := selected[0].Capabilities
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("selected variant capabilities = %#v, want %#v", got, tc.want)
			}
			if selected[0].Via != "wire-bridge" || selected[0].Model != "variant-model" || selected[0].Options["mode"].Value != "fast" {
				t.Fatalf("neighboring variant facts were lost at configured consumer: %#v", selected[0])
			}
			compat := selected[0].LegacyCompatibility()
			if compat.Capabilities == nil || !reflect.DeepEqual(compat.Capabilities, tc.want) || compat.Profile.Via != "wire-bridge" || compat.Profile.Options["mode"] != "fast" {
				t.Fatalf("compatibility projection = %#v, want caps %#v and preserved via/options", compat, tc.want)
			}
		})
	}
}

// declarationNamed is the one provider declaration named name: the user's TOML provider, beside
// whatever the selected packs ship (packs/copilot-auth ships `github-copilot`).
func declarationNamed(t *testing.T, declarations []packdecl.ProviderContribution, name string) packdecl.ProviderContribution {
	t.Helper()
	var found []packdecl.ProviderContribution
	for _, d := range declarations {
		if d.Name == name {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("provider declarations = %#v, want exactly one named %q", declarations, name)
	}
	return found[0]
}
