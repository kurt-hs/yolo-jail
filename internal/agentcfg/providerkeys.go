package agentcfg

// providerkeys.go keeps the PROVIDER KEYS out of the capture overlay: the settings an agent's
// provider selection decides (packdecl.ProviderKey, declared by the agent's own pack), which no
// captured in-jail edit may set.
//
// WHY THE OVERLAY AND NOT THE FILE. A captured edit outranks every layer but computed and
// managed, and a derive asserts a provider switch only while its provider is selected — it
// never tombstones one, because a tombstone at that rank would also delete the user's own HOST
// copy of the key, which PP-D1 rules yolo obeys. So once an overlay holds a switch (an adoption
// that took yolo's own previous Bedrock render for the user's residue, an agent rewriting the
// file from a copy it read before a reset, a hand edit), every later render with another
// provider selected still writes it, and the agent runs on a platform yolo delivered no
// credentials for. Dropping it from the decided overlay, on both branches and before the
// narrowing, is what makes the computed layer the only voice at the key: its value lands when
// it asserts one, and the key is absent when it does not.
//
// IT RUNS ON THE ACCUMULATED OVERLAY, like narrowOverlay, so a sidecar an older yolo wrote is
// cleaned on the next boot, and the drop is persisted: the next render has nothing to report.
// A TOMBSTONE is kept: a captured deletion can switch nothing on.

import (
	"regexp"
	"sort"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
)

// OwnedLeaf is one provider key as the caller resolved it for this render: the pointer, and the
// values dropped there (Matches nil = every value). The caller decides a value-conditional key
// against the agent's selection before handing it here — a key whose platform IS selected is
// simply not passed — so this package knows no platform.
type OwnedLeaf struct {
	Pointer string
	Matches *regexp.Regexp
}

// OwnedDrop is one captured value this render dropped at a provider key: the pointer and the
// value the overlay held there. A REPORT, printed by the caller.
type OwnedDrop struct {
	Pointer string
	Value   any
}

// dropProviderKeys returns overlay without the captured values at owned, and what it dropped,
// in pointer order. A non-object overlay (a keyless surface) has no keys and is returned as is.
func dropProviderKeys(overlay any, owned []OwnedLeaf) (any, []OwnedDrop) {
	m, ok := overlay.(map[string]any)
	if !ok || len(owned) == 0 || len(m) == 0 {
		return overlay, nil
	}
	var drops []OwnedDrop
	for _, leaf := range owned {
		steps, err := jsonptr.Parse(leaf.Pointer)
		if err != nil || len(steps) == 0 {
			continue
		}
		var v any
		var dropped bool
		m, v, dropped = dropAt(m, steps, leaf.Matches)
		if dropped {
			drops = append(drops, OwnedDrop{Pointer: leaf.Pointer, Value: v})
		}
	}
	sort.SliceStable(drops, func(i, j int) bool { return drops[i].Pointer < drops[j].Pointer })
	return m, drops
}

// dropAt removes the value at steps from m when it is a non-null value matches accepts, pruning
// every object the removal empties. m is copied along the path, never mutated.
func dropAt(m map[string]any, steps []string, matches *regexp.Regexp) (map[string]any, any, bool) {
	v, present := m[steps[0]]
	if !present {
		return m, nil, false
	}
	if len(steps) > 1 {
		sub, isObj := v.(map[string]any)
		if !isObj {
			return m, nil, false
		}
		inner, got, dropped := dropAt(sub, steps[1:], matches)
		if !dropped {
			return m, nil, false
		}
		out := withoutKey(m, steps[0])
		if len(inner) > 0 {
			out[steps[0]] = inner
		}
		return out, got, true
	}
	if v == nil {
		return m, nil, false // a captured deletion switches nothing on
	}
	if matches != nil {
		s, isString := v.(string)
		if !isString || !matches.MatchString(s) {
			return m, nil, false
		}
	}
	return withoutKey(m, steps[0]), v, true
}
