package packload

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func TestComposeProviderDeclarationsPreservesEmptySlicesAndFalsePointers(t *testing.T) {
	manifest, problems := packdecl.DecodeTOML([]byte(`name = "fixture"
[providers.p]
capabilities = []
[providers.p.models."global.openai.m"]
vendor = "openai"
accepts = []
thinking = []
reasoning = false
price = { input = 0.1, output = 0.2, cache_read = 0.0, cache_write = 0.0 }
[providers.p.variants.preview]
model = "global.openai.m"
enforce_models = false
capabilities = []
protocols = []
`))
	if len(problems) != 0 {
		t.Fatalf("fixture decode: %v", problems)
	}
	declarations, err := ComposeProviderDeclarations(nil, []*Pack{{Name: "fixture", Decl: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations) != 1 {
		t.Fatalf("declarations = %#v", declarations)
	}
	provider := declarations[0]
	model := provider.ModelRows["global.openai.m"]
	if model.Accepts == nil || len(model.Accepts) != 0 || model.Thinking == nil || len(model.Thinking) != 0 ||
		model.Reasoning == nil || *model.Reasoning {
		t.Fatalf("empty arrays or false model pointer were lost: %#v", model)
	}
	variant := provider.Variants["preview"]
	if variant.EnforceModels == nil || *variant.EnforceModels || variant.Capabilities == nil || len(variant.Capabilities) != 0 ||
		variant.Protocols == nil || len(variant.Protocols) != 0 || provider.Capabilities == nil || len(provider.Capabilities) != 0 {
		t.Fatalf("empty arrays or false variant pointer were lost: %#v / %#v", provider, variant)
	}
}

func TestComposeProviderDeclarationsKeepsModelContributionsBeforeUserFold(t *testing.T) {
	manifest, problems := packdecl.Decode([]byte(`{"name":"fixture","contributes":[
		{"kind":"provider","name":"options","models":{"base":"base-id"},"model_options":{"base":{"order":"1"}}},
		{"kind":"models","provider":"options","add":[{"id":"added","vendor":"openai","name":"Added"}]},
		{"kind":"models","provider":"options","only":["base-id","added"]}
	]}`))
	if len(problems) != 0 {
		t.Fatalf("fixture decode: %v", problems)
	}
	packs := []*Pack{{Name: "fixture", Decl: manifest}}

	declarations, err := ComposeProviderDeclarations(nil, packs)
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations) != 1 || declarations[0].Name != "options" {
		t.Fatalf("provider named options was lost or confused with entry options: %#v", declarations)
	}
	provider := declarations[0]
	if provider.Models["base"] != "base-id" || provider.Models["added"] != "added" {
		t.Fatalf("model contribution add/only result = %#v", provider.Models)
	}
	if got := *provider.ModelOptions["base"]["order"]; got != "1" {
		t.Fatalf("base menu order = %q, want 1", got)
	}
	if got := *provider.ModelOptions["added"]["order"]; got != "2" {
		t.Fatalf("added menu order = %q, want 2", got)
	}

	// The old public path still performs the same add-before-only pass and legacy lowering.
	legacy, err := ComposeProviders(nil, packs)
	if err != nil {
		t.Fatal(err)
	}
	entryRaw, ok := legacy.Get("options")
	if !ok {
		t.Fatal("legacy composer dropped provider named options")
	}
	entry := entryRaw.(*jsonx.OrderedMap)
	modelsRaw, _ := entry.Get("models")
	models, ok := modelsRaw.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("legacy composer lost model list: %#v", entry)
	}
	if base, _ := models.Get("base"); base != "base-id" {
		t.Fatalf("legacy base model = %#v", base)
	}
	if added, _ := models.Get("added"); added != "added" {
		t.Fatalf("legacy added model = %#v", added)
	}
	optionsRaw, _ := entry.Get("model_options")
	options, ok := optionsRaw.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("legacy composer lost model options: %#v", entry)
	}
	addedOptionsRaw, _ := options.Get("added")
	addedOptions, ok := addedOptionsRaw.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("legacy composer lost added model options: %#v", options)
	}
	if order, _ := addedOptions.Get("order"); order != "2" {
		t.Fatalf("legacy added model order = %#v", order)
	}
}
