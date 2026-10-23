package packdecl

// programneed_test.go pins OQ-PS11 (docs/design/provisioner-sets.md, ruled 2026-10-23): one
// `program` kind, a recipe-less entry being a NEED, and the old `requires` kind refused with
// the next step rather than migrated.

import (
	"strings"
	"testing"
)

func TestTheRequiresKindIsRefusedNamingTheNextStep(t *testing.T) {
	_, problems := Decode([]byte(`{"name":"p","contributes":[{"kind":"requires","bin":"rg"}]}`))
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, `kind "requires" has been REMOVED`) ||
		!strings.Contains(joined, `write it as a "program" with no recipe`) {
		t.Fatalf("the old kind must be refused with the rewrite named; got %v", problems)
	}
	if KnownKind("requires") {
		t.Error(`"requires" must not be a known kind: there is no transition`)
	}
}

func TestARecipeLessProgramIsANeed(t *testing.T) {
	m, problems := Decode([]byte(`{"name":"p","contributes":[` +
		`{"kind":"program","bin":"rg","install_hints":{"brew":"ripgrep"}},` +
		`{"kind":"program","bin":"tool","via":"npm","package":"tool"}]}`))
	if len(problems) != 0 {
		t.Fatalf("a need and a recipe must both decode: %v", problems)
	}
	cs := m.Contributions()
	if !cs[0].IsNeed() || cs[0].Provides() || !cs[1].Provides() || cs[1].IsNeed() {
		t.Fatalf("shapes wrong: need=%+v recipe=%+v", cs[0], cs[1])
	}
	if ins := m.InstallContributions(); len(ins) != 1 || ins[0].Bin != "tool" {
		t.Errorf("a need installs nothing; InstallContributions = %+v", ins)
	}
	if req := m.RequiredBins(); len(req) != 1 || req[0].Bin != "rg" || !req[0].Need {
		t.Errorf("RequiredBins = %+v, want the one need", req)
	}
	deps := m.DepRequirements()
	if len(deps) != 2 || !deps[0].Need || deps[1].Need || deps[0].SelfInstall != "" {
		t.Errorf("DepRequirements = %+v, want the need (no self-install) then the recipe", deps)
	}
}

func TestANeedRefusesRecipeAndAgentFieldsByName(t *testing.T) {
	_, problems := Decode([]byte(`{"name":"p","contributes":[` +
		`{"kind":"program","bin":"rg","package":"ripgrep","update":["up"]}]}`))
	joined := strings.Join(problems, "\n")
	for _, want := range []string{`a program with no "via" does not take "package"`,
		`a program with no "via" does not take "update"`, `it is a NEED`, `Add a "via"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, problems)
		}
	}
}

func TestOnePackDeclaresAProgramOnce(t *testing.T) {
	_, problems := Decode([]byte(`{"name":"p","contributes":[` +
		`{"kind":"program","bin":"tool"},{"kind":"program","bin":"tool","via":"npm","package":"tool"}]}`))
	if !strings.Contains(strings.Join(problems, "\n"), `program "tool" is declared again`) {
		t.Fatalf("a need beside its own pack's recipe must be refused: %v", problems)
	}
}
