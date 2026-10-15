package agentcfg

// listremove_test.go pins a config-list's `remove` in the fold (Compose →
// applyListContributions) and in the rmw contributed set (ContributedEntries): what it
// deletes, that an absent entry or path is a silent no-op, and the ORDER — remove before add
// within one contribution, contributions in pack-then-declaration order
// (docs/reference/pack-system.md#config-list-order).

import (
	"encoding/json"
	"reflect"
	"testing"
)

func mustEdit(t *testing.T, pack, path, add, remove string) ListContribution {
	t.Helper()
	var a, r json.RawMessage
	if add != "" {
		a = json.RawMessage(add)
	}
	if remove != "" {
		r = json.RawMessage(remove)
	}
	l, err := NewListEdit(pack, path, a, r)
	if err != nil {
		t.Fatalf("NewListEdit(%s, %s, %s, %s): %v", pack, path, add, remove, err)
	}
	return l
}

// abcSurface is listSurface with the owner list [A, B, C].
func abcSurface() Inputs {
	s := listSurface()
	s.Defaults = map[string]any{"packages": []any{"A", "B", "C"}}
	return Inputs{Surface: s}
}

func wantList(t *testing.T, got map[string]any, want ...any) {
	t.Helper()
	if !reflect.DeepEqual(got["packages"], want) {
		t.Fatalf("packages = %#v, want %#v", got["packages"], want)
	}
}

// A remove deletes the matching entry from what the layers below produced, keeping the
// survivors' order, and every occurrence of it — compared as whole JSON values, the identity
// `add` de-duplicates by, so an object entry and a TOML int match too.
func TestComposeListRemoveDeletesFromTheLowerLayers(t *testing.T) {
	in := abcSurface()
	in.Lists = []ListContribution{mustEdit(t, "trim", "/packages", "", `["B"]`)}
	res, got := composeMap(t, in)
	wantList(t, got, "A", "C")
	// The per-entry account no longer lists the removed entry.
	var shown []any
	for _, e := range res.Lists[0].Entries {
		shown = append(shown, e.Value)
	}
	if !reflect.DeepEqual(shown, []any{"A", "C"}) {
		t.Errorf("provenance entries = %#v, want the survivors only", shown)
	}

	in = abcSurface()
	in.Surface.Defaults = map[string]any{"packages": []any{
		"A", map[string]any{"src": "B", "n": int64(1)}, "C", map[string]any{"n": 1.0, "src": "B"}}}
	in.Lists = []ListContribution{mustEdit(t, "trim", "/packages", "", `[{"src":"B","n":1}]`)}
	_, got = composeMap(t, in)
	wantList(t, got, "A", "C")
}

// Removing an entry the array does not hold changes nothing and is not an error; removing
// from an array that does not exist creates nothing.
func TestComposeListRemoveOfAbsentEntryIsANoOp(t *testing.T) {
	in := abcSurface()
	in.Lists = []ListContribution{mustEdit(t, "trim", "/packages", "", `["Z"]`)}
	_, got := composeMap(t, in)
	wantList(t, got, "A", "B", "C")

	in = abcSurface()
	in.Lists = []ListContribution{mustEdit(t, "trim", "/nothing/here", "", `["Z"]`)}
	_, got = composeMap(t, in)
	if _, has := got["nothing"]; has {
		t.Fatalf("a remove at a missing path created it: %#v", got)
	}
	wantList(t, got, "A", "B", "C")
}

// WITHIN ONE CONTRIBUTION, remove applies before add: an entry named in both ends up
// present (re-appended after the survivors), and the adds still append.
func TestComposeListRemoveThenAddWithinOneContribution(t *testing.T) {
	in := abcSurface()
	in.Lists = []ListContribution{mustEdit(t, "both", "/packages", `["B","D"]`, `["B","C"]`)}
	_, got := composeMap(t, in)
	wantList(t, got, "A", "B", "D")
}

// ACROSS CONTRIBUTIONS, pack order then declaration order: a later remove deletes what an
// earlier pack added, and a later add re-adds what an earlier pack removed.
func TestComposeListRemoveAcrossContributionsFollowsPackOrder(t *testing.T) {
	in := abcSurface()
	in.Lists = []ListContribution{
		mustEdit(t, "first", "/packages", `["X"]`, `["B"]`),
		mustEdit(t, "second", "/packages", `["B"]`, `["X"]`),
	}
	_, got := composeMap(t, in)
	wantList(t, got, "A", "C", "B")

	// …and the contributed set rmw reconciles against is the same fold over [].
	if got := ContributedEntries(in.Lists, "/packages"); !reflect.DeepEqual(got, []any{"B"}) {
		t.Errorf("ContributedEntries = %#v, want [B] (X added by first, removed by second)", got)
	}
}

// A remove at a non-array value is the type conflict an add is: refused, never skipped.
func TestComposeListRemoveTypeConflictRefuses(t *testing.T) {
	in := abcSurface()
	in.Surface.Defaults = map[string]any{"packages": "A"}
	in.Lists = []ListContribution{mustEdit(t, "trim", "/packages", "", `["A"]`)}
	if _, err := Compose(in); err == nil {
		t.Fatal("a remove at a non-array value composed")
	}
}

func TestNewListEditRefusesNullRemove(t *testing.T) {
	if _, err := NewListEdit("p", "/a", nil, json.RawMessage(`["x",null]`)); err == nil {
		t.Fatal("a null remove entry was accepted")
	}
}
