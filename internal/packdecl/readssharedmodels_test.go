package packdecl

// readssharedmodels_test.go pins two model-file declarations of
// docs/design/model-lists-and-pickers.md: `reads_shared_models` on a program (ML-D11), read by bin
// and refused on any other kind, and a null `model_options` fact decoding as a tombstone (ML-D10)
// rather than as an empty string. Through Decode and the real validator.

import (
	"strings"
	"testing"
)

func TestReadsSharedModelsIsAProgramsDeclaration(t *testing.T) {
	m, problems := Decode([]byte(`{"contributes":[{"kind":"program","bin":"tool","via":"npm",
	  "package":"tool","reads_shared_models":true}]}`))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if !m.ReadsSharedModelsFor("tool") || m.ReadsSharedModelsFor("other") {
		t.Errorf("ReadsSharedModelsFor(tool/other) = %v/%v, want true/false",
			m.ReadsSharedModelsFor("tool"), m.ReadsSharedModelsFor("other"))
	}
	_, problems = Decode([]byte(`{"contributes":[{"kind":"provider","name":"p",
	  "models":{"a":"a"},"reads_shared_models":true}]}`))
	if !strings.Contains(strings.Join(problems, "\n"), `does not take "reads_shared_models"`) {
		t.Errorf("a provider declaring reads_shared_models: problems %v", problems)
	}
}

func TestANullModelFactDecodesAsATombstone(t *testing.T) {
	m, problems := Decode([]byte(`{"contributes":[{"kind":"provider","name":"p",
	  "models":{"a":"a"},"model_options":{"a":{"reasoning":null,"input":"text"}}}]}`))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	facts := m.Providers()[0].ModelOptions["a"]
	if v, ok := facts["reasoning"]; !ok || v != nil {
		t.Errorf("reasoning = %v (present %v), want a nil tombstone", v, ok)
	}
	if v := facts["input"]; v == nil || *v != "text" {
		t.Errorf("input = %v, want text", v)
	}
	if got := m.Providers()[0].ModelOptions.Strings()["a"]; len(got) != 1 || got["input"] != "text" {
		t.Errorf("Strings() = %v, want the stated fact alone", got)
	}
}
