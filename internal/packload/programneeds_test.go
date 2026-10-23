package packload

// programneeds_test.go pins OQ-PS5 and PS-D18 (docs/design/provisioner-sets.md): a `program`
// with no recipe is a need, satisfied by another SELECTED pack's recipe for the same bin; two
// selected recipes for one bin are refused naming both; a need nobody provides keeps what the
// retired `requires` kind did.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func needOf(bin string) packdecl.Contribution {
	return packdecl.Contribution{Kind: packdecl.KindProgram, Bin: bin}
}

func recipeOf(bin string) packdecl.Contribution {
	return packdecl.Contribution{Kind: packdecl.KindProgram, Bin: bin, Via: "npm", Package: bin + "-pkg"}
}

// A needs X, B provides X: satisfied by B, and the line names both packs.
func TestANeedIsSatisfiedByAnotherSelectedPacksRecipe(t *testing.T) {
	a := claimPack(t, "alpha", needOf("xtool"))
	b := claimPack(t, "bravo", recipeOf("xtool"))
	needs := ResolveProgramNeeds([]*Pack{a, b})
	if len(needs) != 1 {
		t.Fatalf("want one need, got %+v", needs)
	}
	n := needs[0]
	if n.Pack != "alpha" || n.Bin != "xtool" || n.Provider() != "bravo" {
		t.Fatalf("need = %+v, want alpha's xtool provided by bravo", n)
	}
	if got, want := n.ProvidedLine(), "program xtool, needed by pack alpha, is provided by pack bravo"; got != want {
		t.Errorf("ProvidedLine = %q, want %q", got, want)
	}
	if got := ProvidedNeeds([]*Pack{a, b}, "linux", "amd64"); got["alpha\x00xtool"] != "bravo" {
		t.Errorf("ProvidedNeeds = %v, want alpha/xtool → bravo", got)
	}
	if cols := ProgramProviderCollisions([]*Pack{a, b}); len(cols) != 0 {
		t.Errorf("one provider is not a collision: %+v", cols)
	}
	// A need beside its provider is neither a footprint collision nor an agent-name one.
	for _, c := range Collisions([]*Pack{a, b}) {
		if c.Target == "xtool" {
			t.Errorf("a need beside its provider collided: %+v", c)
		}
	}
	if cols := AgentNameCollisions([]*Pack{a, b}); len(cols) != 0 {
		t.Errorf("a need claims no agent name: %+v", cols)
	}
}

// Any recipe kind satisfies a need: the user picks the provider by picking the pack.
func TestANeedIsSatisfiedWhateverRecipeTheProviderCarries(t *testing.T) {
	a := claimPack(t, "alpha", needOf("xtool"))
	inst := claimPack(t, "bravo", packdecl.Contribution{
		Kind: packdecl.KindProgram, Bin: "xtool", Via: "installer", URL: "https://example.com/i.sh"})
	if got := ResolveProgramNeeds([]*Pack{a, inst})[0].Provider(); got != "bravo" {
		t.Errorf("an installer recipe must satisfy the need; provider = %q", got)
	}
}

// B and C both provide X: refused, naming both, with the one remedy.
func TestTwoSelectedProvidersOfOneProgramAreRefusedNamingBoth(t *testing.T) {
	a := claimPack(t, "alpha", needOf("xtool"))
	b := claimPack(t, "bravo", recipeOf("xtool"))
	c := claimPack(t, "charlie", recipeOf("xtool"))
	packs := []*Pack{a, b, c}
	cols := ProgramProviderCollisions(packs)
	if len(cols) != 1 {
		t.Fatalf("want one provider collision, got %+v", cols)
	}
	col := cols[0]
	if col.Target != "xtool" || strings.Join(col.Packs, ",") != "bravo,charlie" {
		t.Errorf("collision = %+v, want xtool by bravo,charlie", col)
	}
	for _, want := range []string{"pack bravo", "pack charlie", "needed by pack alpha", "from `packs`"} {
		if !strings.Contains(col.Reason, want) {
			t.Errorf("reason missing %q:\n%s", want, col.Reason)
		}
	}
	if got := ResolveProgramNeeds(packs)[0].Provider(); got != "" {
		t.Errorf("an ambiguous need has no single provider; got %q", got)
	}
	// Reported once, under the provider rule, not again as an agent-name clash.
	if cols := AgentNameCollisions(packs); len(cols) != 0 {
		t.Errorf("a program-only clash must not double-report as an agent name: %+v", cols)
	}
	// And `pack lint`'s generic loop names the same remedy.
	found := false
	for _, gc := range Collisions(packs) {
		if gc.Kind == packdecl.KindProgram && gc.Target == "xtool" {
			found = strings.Contains(gc.Reason, "from `packs`")
		}
	}
	if !found {
		t.Errorf("Collisions must report the provider clash with the PS-D18 remedy: %+v", Collisions(packs))
	}
}

// A needs X and nothing selected provides it: unprovided, so the jail asserts it on PATH.
func TestAnUnprovidedNeedHasNoProvider(t *testing.T) {
	a := claimPack(t, "alpha", needOf("xtool"))
	other := claimPack(t, "bravo", recipeOf("ytool"))
	n := ResolveProgramNeeds([]*Pack{a, other})
	if len(n) != 1 || len(n[0].Providers) != 0 || n[0].ProvidedLine() != "" {
		t.Fatalf("need = %+v, want unprovided", n)
	}
	if len(ProvidedNeeds([]*Pack{a, other}, "linux", "amd64")) != 0 {
		t.Error("an unprovided need must not be skipped by the boot assertion")
	}
}

// Only SELECTED packs provide (OQ-BH14): the resolver reads what it is handed and nothing wider.
func TestAnUnselectedPackProvidesNothing(t *testing.T) {
	a := claimPack(t, "alpha", needOf("claude"))
	if n := ResolveProgramNeeds([]*Pack{a}); len(n[0].Providers) != 0 {
		t.Fatalf("a shipped-but-unselected pack must not provide a need: %+v", n)
	}
}
