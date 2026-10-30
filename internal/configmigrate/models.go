package configmigrate

// models.go converts a provider's legacy model rows into the readable rows and folds the facts
// every row repeats into the provider's `every_model` (docs/design/config-migration.md §4 item 4,
// docs/design/readable-config.md OQ-RC18, RC-D27, RC-D29). The equality check's config layers
// compare both sides with the folds undone (unfoldEveryModel), so what proves a conversion or a
// fold is the extra layer this file adds: the provider table a launch resolves from each side
// (providerTable), composed by the one composition every notch calls.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// FoldedFact is one fact every model row of a provider wrote alike, written once in its
// `every_model` instead.
type FoldedFact struct {
	// Path is the new key, dotted; Rows is how many rows it was written on.
	Path string
	Rows int
}

// legacyRowFacts are the legacy object-row keys a readable row has a spelling for, each with
// that spelling. `extends` and `wire_id` are not here: one points at shared facts and the other
// sends another row's id, and neither has a readable form this conversion can prove.
var legacyRowFacts = map[string]string{
	"id": "", "vendor": "vendor",
	"name": "name", "description": "description", "reasoning": "reasoning", "max_tokens": "max_tokens",
	"base": "base", "openrouter_routing": "openrouter_routing",
	"context_window": "context", "input": "accepts", "cost": "price",
}

// foldableFacts are the readable facts the rename table says `every_model` may carry
// (config.RenameDescriptors, the RenameFoldWhenEqual rows), in the order they are written.
var foldableFacts = []string{"context", "max_tokens", "reasoning", "accepts", "price"}

// convertModelRows returns providers with each legacy row keyed by its own id rewritten as a
// readable row: the keys respelled, `id` left out (it is the key), and `vendor` left out when the
// id names the same maker. A row this cannot convert (an alias for another id, a null fact, a
// shared-facts pointer) is left as it is, for the rename table to report. renamed maps each new
// path to the old one, for comment placement.
func convertModelRows(value any, path []string) (out any, renamed map[string][]string) {
	providers, ok := value.(*jsonx.OrderedMap)
	if !ok {
		return value, nil
	}
	renamed = map[string][]string{}
	result := jsonx.NewOrderedMap()
	for _, name := range providers.Keys() {
		raw, _ := providers.Get(name)
		entry, isMap := raw.(*jsonx.OrderedMap)
		models, hasModels := mapAt(entry, "models")
		if !isMap || entry == nil || !hasModels {
			result.Set(name, raw)
			continue
		}
		rows := jsonx.NewOrderedMap()
		ids := append([]string(nil), models.Keys()...)
		converted := map[string]*jsonx.OrderedMap{}
		for _, id := range ids {
			v, _ := models.Get(id)
			if row, ok := readableFromLegacy(id, v); ok {
				converted[id] = row
			}
		}
		// A readable row's place in its provider is an order every derive sorts by, where a
		// legacy alias sorts by its name: the converted rows go in that order so the sort is
		// unchanged. Rows left legacy keep their place after them.
		if len(converted) > 0 {
			sort.SliceStable(ids, func(i, j int) bool {
				_, ci := converted[ids[i]]
				_, cj := converted[ids[j]]
				if ci != cj {
					return ci
				}
				return ci && ids[i] < ids[j]
			})
		}
		for _, id := range ids {
			if row, ok := converted[id]; ok {
				rows.Set(id, row)
				old, _ := models.Get(id)
				for _, key := range old.(*jsonx.OrderedMap).Keys() {
					if spelled := legacyRowFacts[key]; spelled != "" && spelled != key {
						renamed[strings.Join(append(append([]string(nil), path...), name, "models", id, spelled), "\x00")] =
							append(append([]string(nil), path...), name, "models", id, key)
					}
				}
				continue
			}
			v, _ := models.Get(id)
			rows.Set(id, v)
		}
		copied := jsonx.NewOrderedMap()
		for _, key := range entry.Keys() {
			v, _ := entry.Get(key)
			if key == "models" {
				v = rows
			}
			copied.Set(key, v)
		}
		result.Set(name, copied)
	}
	return result, renamed
}

// readableFromLegacy is one legacy row as a readable row, false when it has none.
func readableFromLegacy(key string, value any) (*jsonx.OrderedMap, bool) {
	row, ok := value.(*jsonx.OrderedMap)
	if !ok {
		return nil, false
	}
	id, hasID := row.Get("id")
	if s, isString := id.(string); !hasID || !isString || s != key {
		return nil, false
	}
	out := jsonx.NewOrderedMap()
	for _, k := range row.Keys() {
		v, _ := row.Get(k)
		spelled, known := legacyRowFacts[k]
		if !known || v == nil {
			return nil, false
		}
		switch {
		case spelled == "":
			continue
		case k == "vendor":
			if maker, _ := v.(string); maker == packload.InferModelVendor(key) {
				continue
			}
		}
		out.Set(spelled, jsonx.DeepCopy(v))
	}
	return out, true
}

// isReadableRow says whether a `models` value is a readable row: a table with no `id`.
func isReadableRow(v any) (*jsonx.OrderedMap, bool) {
	row, ok := v.(*jsonx.OrderedMap)
	if !ok {
		return nil, false
	}
	if _, legacy := row.Get("id"); legacy {
		return nil, false
	}
	return row, true
}

// foldEveryModel moves each fact that every readable row of a provider writes alike into the
// provider's `every_model`, for a provider with at least two rows, only readable rows and no
// `every_model` of its own. Whether a fold keeps the launch's provider table is the plan's proof
// (Plan), which drops every fold when it does not.
func foldEveryModel(values *jsonx.OrderedMap) []FoldedFact {
	providers, ok := mapAt(values, "providers")
	if !ok {
		return nil
	}
	var out []FoldedFact
	for _, name := range providers.Keys() {
		entry, ok := mapAt(providers, name)
		if !ok {
			continue
		}
		if _, has := entry.Get(everyModelKey); has {
			continue
		}
		models, ok := mapAt(entry, "models")
		if !ok || models.Len() < 2 {
			continue
		}
		var rows []*jsonx.OrderedMap
		for _, id := range models.Keys() {
			v, _ := models.Get(id)
			row, readable := isReadableRow(v)
			if !readable {
				rows = nil
				break
			}
			rows = append(rows, row)
		}
		if len(rows) < 2 {
			continue
		}
		every := jsonx.NewOrderedMap()
		for _, fact := range foldableFacts {
			first, has := rows[0].Get(fact)
			if !has {
				continue
			}
			want, err := jsonx.DumpsSnapshot(first)
			if err != nil {
				continue
			}
			same := true
			for _, row := range rows[1:] {
				v, has := row.Get(fact)
				got, _ := jsonx.DumpsSnapshot(v)
				if !has || got != want {
					same = false
					break
				}
			}
			if !same {
				continue
			}
			every.Set(fact, jsonx.DeepCopy(first))
			for _, row := range rows {
				row.Delete(fact)
			}
			out = append(out, FoldedFact{Path: dotted([]string{"providers", name, everyModelKey, fact}), Rows: len(rows)})
		}
		if every.Len() > 0 {
			setBefore(entry, everyModelKey, every, "models")
		}
	}
	return out
}

// unfoldEveryModel undoes every fold in values for the config layers' comparison: each
// `every_model` fact is written on each readable row that does not write its own, and
// `every_model` leaves the entry. What a fold means for a launch is the provider table's to prove.
func unfoldEveryModel(values *jsonx.OrderedMap) {
	providers, ok := mapAt(values, "providers")
	if !ok {
		return
	}
	for _, name := range providers.Keys() {
		entry, ok := mapAt(providers, name)
		if !ok {
			continue
		}
		every, ok := mapAt(entry, everyModelKey)
		if !ok {
			continue
		}
		models, _ := mapAt(entry, "models")
		if models == nil {
			continue
		}
		for _, id := range models.Keys() {
			v, _ := models.Get(id)
			row, readable := isReadableRow(v)
			if !readable {
				continue
			}
			for _, fact := range every.Keys() {
				if _, own := row.Get(fact); !own {
					value, _ := every.Get(fact)
					row.Set(fact, jsonx.DeepCopy(value))
				}
			}
		}
		entry.Delete(everyModelKey)
	}
}

const everyModelKey = "every_model"

// modelOrderKey is the proof's own key in each provider of the table: the order a derive lists
// the provider's models in (by each one's `order` fact, then its name), which the `order` facts
// themselves stand for.
const modelOrderKey = "(model order)"

// providerTable is the provider table a launch resolves from one side's merged read, composed
// with every shipped pack and the offline catalog, canonical: each provider's `order` facts are
// replaced by the order they make (modelOrderKey). A composition that refuses is its refusal.
func providerTable(read *jsonx.OrderedMap, packs []*packload.Pack) *jsonx.OrderedMap {
	out := jsonx.NewOrderedMap()
	providers, _ := mapAt(read, "providers")
	facts, _ := mapAt(read, "model_facts")
	table, err := packload.ComposeProviders(providers, packs, packload.WithModelFacts(facts),
		config.WithOfflineModelCatalog(read, packs))
	if err != nil {
		out.Set("(refused)", err.Error())
		return out
	}
	if table == nil {
		return out
	}
	for _, name := range table.Keys() {
		entry, ok := mapAt(table, name)
		if !ok {
			v, _ := table.Get(name)
			out.Set(name, v)
			continue
		}
		entry = jsonx.DeepCopyMap(entry)
		options, _ := mapAt(entry, "model_options")
		if models, ok := mapAt(entry, "models"); ok {
			aliases := append([]string(nil), models.Keys()...)
			order := func(alias string) (int, bool) {
				row, ok := mapAt(options, alias)
				if !ok {
					return 0, false
				}
				v, _ := row.Get("order")
				s, _ := v.(string)
				n, err := strconv.Atoi(s)
				return n, err == nil
			}
			if options == nil {
				order = func(string) (int, bool) { return 0, false }
			}
			sort.SliceStable(aliases, func(i, j int) bool {
				oi, hi := order(aliases[i])
				oj, hj := order(aliases[j])
				switch {
				case hi && hj && oi != oj:
					return oi < oj
				case hi != hj:
					return hi
				}
				return aliases[i] < aliases[j]
			})
			list := make([]any, len(aliases))
			for i, alias := range aliases {
				list[i] = alias
			}
			entry.Set(modelOrderKey, list)
		}
		if options != nil {
			for _, alias := range options.Keys() {
				if row, ok := mapAt(options, alias); ok {
					row.Delete("order")
				}
			}
		}
		out.Set(name, entry)
	}
	return out
}

func mapAt(m *jsonx.OrderedMap, key string) (*jsonx.OrderedMap, bool) {
	if m == nil {
		return nil, false
	}
	v, _ := m.Get(key)
	child, ok := v.(*jsonx.OrderedMap)
	return child, ok && child != nil
}

// setBefore sets key in m just before the existing key before, so a provider's `every_model` is
// written above the rows it is shared by.
func setBefore(m *jsonx.OrderedMap, key string, value any, before string) {
	keys := append([]string(nil), m.Keys()...)
	values := make([]any, len(keys))
	for i, k := range keys {
		values[i], _ = m.Get(k)
	}
	for _, k := range keys {
		m.Delete(k)
	}
	placed := false
	for i, k := range keys {
		if k == before && !placed {
			m.Set(key, value)
			placed = true
		}
		m.Set(k, values[i])
	}
	if !placed {
		m.Set(key, value)
	}
}
