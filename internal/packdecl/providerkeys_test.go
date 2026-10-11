package packdecl

import (
	"strings"
	"testing"
)

const providerKeysProgram = `{"kind":"program","bin":"acme","via":"npm","package":"acme",
  "platform_switches":[{"platform":"aws-bedrock","surface":"acme/settings","pointer":"/env/USE_BR"}]}`

// A pack's provider keys decode, keyed by its program's bin, and that program's platform
// switches are provider keys too, first.
func TestAPackDeclaresItsProgramsProviderKeys(t *testing.T) {
	m, problems := Decode([]byte(`{"provider_keys":{"acme":[
	    {"surface":"acme/settings","pointer":"/env/AWS_REGION"},
	    {"surface":"acme/settings","pointer":"/model","platform":"aws-bedrock","matches":"^br\\."}]},
	  "contributes":[` + providerKeysProgram + `]}`))
	if len(problems) != 0 {
		t.Fatalf("legal provider keys refused: %v", problems)
	}
	got := m.ProviderKeysFor("acme/settings")
	if len(got) != 3 || got[0].Pointer != "/env/USE_BR" || got[1].Pointer != "/env/AWS_REGION" ||
		got[2].Matches != `^br\.` || got[2].Platform != "aws-bedrock" {
		t.Errorf("provider keys = %+v", got)
	}
	if len(m.ProviderKeysFor("acme/other")) != 0 {
		t.Error("a key is the surface's it names")
	}
}

func TestMalformedProviderKeysAreRefused(t *testing.T) {
	for body, want := range map[string]string{
		`{"acme":[{"surface":"other/settings","pointer":"/env/X"}]}`:                     "is agent \"other\"'s",
		`{"acme":[{"surface":"acme","pointer":"/env/X"}]}`:                               "must be \"<agent>/<name>\"",
		`{"acme":[{"surface":"acme/settings","pointer":""}]}`:                            "pointer to a key",
		`{"acme":[{"surface":"acme/settings","pointer":"/m","matches":"("}]}`:            "not a regular expression",
		`{"acme":[{"surface":"acme/settings","pointer":"/m","matches":"x"}]}`:            "needs the \"platform\"",
		`{"acme":[{"surface":"acme/settings","pointer":"/m","platform":"aws-bedrock"}]}`: "there is no \"matches\"",
		`{"nobody":[{"surface":"nobody/settings","pointer":"/env/X"}]}`:                  "must name a non-fork program declared by this pack",
	} {
		_, problems := Decode([]byte(`{"provider_keys":` + body + `,"contributes":[` +
			providerKeysProgram + `]}`))
		if got := strings.Join(problems, "\n"); !strings.Contains(got, want) {
			t.Errorf("%s: want a problem containing %q, got %v", body, want, problems)
		}
	}
}
