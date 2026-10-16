package packload

// treefallback.go is an UNMODIFIED EXTENSION's FALLBACK (docs/design/pi-extension-store-builds.md
// §4.3, XB-D7): where a launch hands no tree for an extension that declares one — a notch that
// builds none, a build that failed with nothing serving, a wire that is absent — the author's raw
// entry takes the place of the tree's list entry in the contributing pack's own list
// contributions, before anything folds, so the agent installs the extension itself, exactly as it
// does with no yolo in between.
//
// And a SKIPPED extension (YOLO_SKIP_EXTENSIONS, docs/design/patched-build-recovery.md §6.6): every
// entry of the contributing pack's own lists that loads the tree is left out, so the agent starts
// without it.
//
// THIS IS NOT THE POST-MERGE REWRITE pack-system.md's OQ-LT2 forbids: it chooses between two
// strings one author wrote, or drops one, in that author's own contributions, before the fold, and
// touches no other pack's entry and no entry the user wrote. Equality is exact, so core reads none
// of the agent's grammar.

import (
	"bytes"
	"encoding/json"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// ApplyTreeFallbacks returns packs with every taken fallback substituted, and the trees whose
// fallback was taken, in PatchedTrees' order. A tree's fallback is taken when it declares one and
// handed reports that this launch hands it no tree. Every pack it changes is a copy (copyPackDecl,
// its posture lists copied too); the input is never modified, since an embedded pack is one shared
// value per process. With nothing taken, packs is returned as it came.
func ApplyTreeFallbacks(packs []*Pack, handed func(Fork) bool) ([]*Pack, []Fork) {
	out, taken, _ := ApplyTreeLists(packs, handed, nil)
	return out, taken
}

// ApplyTreeLists is ApplyTreeFallbacks with SKIPPED trees besides: a tree skipped reports true for
// has every entry that loads it (its own list entry, or a path inside it) left out of the
// contributing pack's config-lists and posture lists, and takes no fallback. The skipped trees come
// back third, in PatchedTrees' order. A nil skipped skips none.
func ApplyTreeLists(packs []*Pack, handed, skipped func(Fork) bool) ([]*Pack, []Fork, []Fork) {
	var taken, dropped []Fork
	edits := map[string]*listEdits{} // pack → its trees' list edits
	editsOf := func(pack string) *listEdits {
		if edits[pack] == nil {
			edits[pack] = &listEdits{swap: map[string]string{}}
		}
		return edits[pack]
	}
	for _, f := range PatchedTrees(packs) {
		if skipped != nil && skipped(f) {
			dropped = append(dropped, f)
			e := editsOf(f.Pack)
			e.drop = append(e.drop, f.ListEntry())
			continue
		}
		if f.Fallback == "" || handed(f) {
			continue
		}
		taken = append(taken, f)
		editsOf(f.Pack).swap[f.ListEntry()] = f.Fallback
	}
	if len(edits) == 0 {
		return packs, nil, nil
	}
	out := append([]*Pack(nil), packs...)
	for i, p := range out {
		if p == nil || p.Decl == nil || edits[p.Name] == nil {
			continue
		}
		e := edits[p.Name]
		cp := copyPackDecl(p)
		for j, c := range cp.Decl.Contributes {
			switch c.Kind {
			case packdecl.KindConfigList:
				cp.Decl.Contributes[j].Add = e.apply(c.Add)
			case packdecl.KindAutonomy:
				cp.Decl.Contributes[j].Autonomous = postureWithEdits(c.Autonomous, e)
				cp.Decl.Contributes[j].Guarded = postureWithEdits(c.Guarded, e)
			}
		}
		out[i] = cp
	}
	return out, taken, dropped
}

// listEdits is what one pack's lists have done to them: a tree's own entry swapped for its
// fallback, and every entry that loads a skipped tree left out.
type listEdits struct {
	swap map[string]string
	drop []string // the skipped trees' own list entries
}

// dropped reports whether entry loads a skipped tree (loadsTree).
func (e *listEdits) dropped(entry string) bool {
	for _, want := range e.drop {
		if loadsTree(entry, want) {
			return true
		}
	}
	return false
}

// postureWithEdits is a copy of posture with e applied to its lists, or posture itself when it has
// none.
func postureWithEdits(posture *packdecl.AutonomyPosture, e *listEdits) *packdecl.AutonomyPosture {
	if posture == nil || len(posture.Lists) == 0 {
		return posture
	}
	cp := *posture
	cp.Lists = append([]packdecl.PostureList(nil), posture.Lists...)
	for i, l := range cp.Lists {
		cp.Lists[i].Add = e.apply(l.Add)
	}
	return &cp
}

// apply is a list body's `add` array with every string entry swap names replaced by its value, in
// place, so the list keeps its order, and every entry that loads a skipped tree removed. A body that
// does not decode, or that it changes nothing in, is returned as it came: the collector reports a
// malformed one.
func (e *listEdits) apply(add json.RawMessage) json.RawMessage {
	var entries []any
	dec := json.NewDecoder(bytes.NewReader(add))
	dec.UseNumber() // a number entry beside the swapped one is written back as it was written
	if err := dec.Decode(&entries); err != nil {
		return add
	}
	changed := false
	kept := make([]any, 0, len(entries))
	for _, entry := range entries {
		if s, ok := entry.(string); ok {
			if e.dropped(s) {
				changed = true
				continue
			}
			if to, hit := e.swap[s]; hit {
				entry, changed = to, true
			}
		}
		kept = append(kept, entry)
	}
	if !changed {
		return add
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return add
	}
	return b
}
