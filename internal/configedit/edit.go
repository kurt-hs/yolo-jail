// Package configedit plans byte-preserving edits to human-authored config documents.
package configedit

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// Action selects the byte splice to plan.
type Action uint8

// Edit actions supported by the splice planner.
const (
	Set Action = iota + 1
	Add
	Append
	Remove
)

// Mutation identifies one key or array path and the value it sets, adds, or appends.
type Mutation struct {
	Action Action
	Path   []configdoc.Step
	Value  any
}

// Hunk records one exact byte-range replacement in the original document.
type Hunk struct {
	Start, End    int
	Before, After []byte
}

// Plan is an immutable splice candidate; Original and Updated are owned copies.
type Plan struct {
	Format            configdoc.Format
	Original, Updated []byte
	Hunks             []Hunk
	Noop              bool
	Expected          any
}

// PlanEdit locates a value through configdoc's positioned index and changes only its bytes.
func PlanEdit(data []byte, format configdoc.Format, m Mutation) (*Plan, error) {
	doc, err := configdoc.Parse(data, format)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(m.Path) == 0 {
		return nil, fmt.Errorf("config edit path is empty")
	}
	root := cloneMap(doc.Values)
	path := m.Path
	parentPath := path[:len(path)-1]
	last := path[len(path)-1]
	parent, parentOK := lookup(root, parentPath)
	start, end := 0, 0
	var replacement []byte
	var hunks []Hunk
	noop := false
	switch m.Action {
	case Set:
		span, ok, e := doc.Index.Value(path...)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, fmt.Errorf("cannot set missing config value")
		}
		old, _ := lookup(root, path)
		normalized, err := semanticValue(m.Value, format)
		if err != nil {
			return nil, err
		}
		if reflect.DeepEqual(old, normalized) {
			noop = true
			start, end = span.Start, span.End
			break
		}
		start, end = span.Start, span.End
		if hasCommentWithin(doc.Index.Comments(), span) {
			hunks, err = planCommentedSet(doc, path, old, normalized)
			if err != nil {
				return nil, err
			}
			normalized = orderedLike(old, normalized)
			if len(hunks) == 0 {
				noop = true
			}
		} else {
			replacement, err = configdoc.EncodeValue(m.Value, format)
			if err != nil {
				return nil, err
			}
		}
		if !setValue(root, path, normalized) {
			return nil, fmt.Errorf("cannot set config value")
		}
	case Add:
		if last.IsIndex {
			return nil, fmt.Errorf("add path must end in a key")
		}
		if !parentOK && format == configdoc.TOML {
			start, end, replacement, err = addTOMLTable(doc, root, path, m.Value)
			if err != nil {
				return nil, err
			}
			break
		}
		if !parentOK {
			return nil, fmt.Errorf("add parent does not exist")
		}
		obj, ok := parent.(*jsonx.OrderedMap)
		if !ok {
			return nil, fmt.Errorf("add parent is not an object")
		}
		if _, exists := obj.Get(last.Key); exists {
			noop = true
			break
		}
		start, end, replacement, err = insertMember(doc, parentPath, last.Key, m.Value)
		if err != nil {
			return nil, err
		}
		normalized, err := semanticValue(m.Value, format)
		if err != nil {
			return nil, err
		}
		obj.Set(last.Key, normalized)
	case Append:
		span, ok, e := doc.Index.Value(path...)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, fmt.Errorf("cannot append to missing config value")
		}
		v, ok := lookup(root, path)
		if !ok {
			return nil, fmt.Errorf("cannot append to missing config value")
		}
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("append target is %T, not an array", v)
		}
		normalized, err := semanticValue(m.Value, format)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if reflect.DeepEqual(item, normalized) {
				noop = true
				break
			}
		}
		if noop {
			break
		}
		start, end, replacement, err = appendArray(doc, path, span, items, m.Value)
		if err != nil {
			return nil, err
		}
		setValue(root, path, append(append([]any(nil), items...), normalized))
	case Remove:
		if last.IsIndex {
			item, ok, e := doc.Index.Item(path...)
			if e != nil {
				return nil, e
			}
			if !ok {
				noop = true
				break
			}
			items, ok := parent.([]any)
			if !ok || last.Index < 0 || last.Index >= len(items) {
				return nil, fmt.Errorf("remove target is not an array item")
			}
			start, end, replacement, err = removeArrayItem(doc, parentPath, last.Index)
			if err != nil {
				return nil, err
			}
			if hasCommentWithin(doc.Index.Comments(), item) {
				if last.Index > 0 && last.Index < len(items)-1 && end == item.End {
					nextPath := append(append([]configdoc.Step(nil), parentPath...), configdoc.Step{Index: last.Index + 1, IsIndex: true})
					next, found, e := doc.Index.Item(nextPath...)
					if e != nil || !found {
						return nil, fmt.Errorf("cannot locate following array item")
					}
					comma := findComma(data, item.End, next.Start, doc.Index.Comments())
					if comma < 0 {
						return nil, fmt.Errorf("cannot locate following array separator")
					}
					end = comma + 1
				}
				replacement = preserveComments(data, start, end, doc.Index.Comments())
				if last.Index > 0 && last.Index < len(items)-1 {
					replacement = append(replacement, ',')
				}
			}
			setValue(root, parentPath, append(append([]any(nil), items[:last.Index]...), items[last.Index+1:]...))
		} else {
			member, ok, e := doc.Index.Member(path...)
			if e != nil {
				return nil, e
			}
			if !ok {
				noop = true
				break
			}
			if !parentOK {
				return nil, fmt.Errorf("remove parent does not exist")
			}
			if format == configdoc.TOML {
				start, end, replacement, err = removeTOMLMember(data, member)
			} else {
				start, end, replacement, err = removeJSONMember(doc, member, parentPath)
			}
			if err != nil {
				return nil, err
			}
			if hasCommentWithin(doc.Index.Comments(), member) {
				replacement = preserveComments(data, start, end, doc.Index.Comments())
			}
			obj, ok := parent.(*jsonx.OrderedMap)
			if !ok {
				return nil, fmt.Errorf("remove parent is not an object")
			}
			obj.Delete(last.Key)
		}
	default:
		return nil, fmt.Errorf("unknown config edit action %d", m.Action)
	}
	updated := append([]byte(nil), data...)
	if !noop {
		updated = applyHunks(data, hunks)
		if len(hunks) == 0 {
			updated = splice(data, start, end, replacement)
		}
		planned, err := configdoc.Parse(updated, format)
		if err != nil {
			return nil, fmt.Errorf("planned splice does not parse: %w", err)
		}
		if !reflect.DeepEqual(planned.Values, root) {
			return nil, fmt.Errorf("planned splice changes effective document values: expected %#v, got %#v", root, planned.Values)
		}
	}
	planHunks := hunks
	if len(planHunks) == 0 {
		planHunks = []Hunk{{Start: start, End: end, Before: append([]byte(nil), data[start:end]...), After: append([]byte(nil), replacement...)}}
	}
	return &Plan{
		Format: format, Original: append([]byte(nil), data...), Updated: updated,
		Noop: noop, Expected: root, Hunks: planHunks,
	}, nil
}

func insertMember(doc *configdoc.Document, parent []configdoc.Step, key string, value any) (int, int, []byte, error) {
	encoded, err := configdoc.EncodeValue(value, doc.Format)
	if err != nil {
		return 0, 0, nil, err
	}
	if doc.Format == configdoc.JSONC {
		span, ok, err := doc.Index.Table(parent...)
		if err != nil {
			return 0, 0, nil, err
		}
		if !ok && len(parent) == 0 {
			span = configdoc.Span{0, len(doc.Bytes)}
			ok = true
		}
		if !ok {
			return 0, 0, nil, fmt.Errorf("JSONC parent is not an object")
		}
		return insertJSONMember(doc, span, parent, key, encoded)
	}
	if _, ok := lookup(doc.Values, parent); !ok {
		return 0, 0, nil, fmt.Errorf("TOML parent table does not exist")
	}
	span, ok, err := doc.Index.Table(parent...)
	if err != nil {
		return 0, 0, nil, err
	}
	if ok && span.Start < len(doc.Bytes) && doc.Bytes[span.Start] == '{' {
		tableValue, _ := lookup(doc.Values, parent)
		table, valid := tableValue.(*jsonx.OrderedMap)
		if !valid {
			return 0, 0, nil, fmt.Errorf("inline TOML table parent is invalid")
		}
		name := tomlKey(key)
		close := span.End - 1
		if table.Len() == 0 {
			return close, close, []byte(name + " = " + string(encoded)), nil
		}
		keys := table.Keys()
		path := append(append([]configdoc.Step(nil), parent...), configdoc.Step{Key: keys[len(keys)-1]})
		last, found, e := doc.Index.Value(path...)
		if e != nil || !found {
			return 0, 0, nil, fmt.Errorf("cannot locate last inline TOML key")
		}
		return last.End, last.End, []byte(", " + name + " = " + string(encoded)), nil
	}
	end := len(doc.Bytes)
	if ok {
		end = span.End
	}
	name := tomlKey(key)
	text := []byte(name + " = " + string(encoded) + newlineOf(doc.Bytes))
	if end > 0 && doc.Bytes[end-1] != '\n' {
		text = append([]byte(newlineOf(doc.Bytes)), text...)
	}
	return end, end, text, nil
}
func insertJSONMember(doc *configdoc.Document, table configdoc.Span, parent []configdoc.Step, key string, value []byte) (int, int, []byte, error) {
	data := doc.Bytes
	open, close := table.Start, table.End
	for close > open && (data[close-1] == ' ' || data[close-1] == '\t' || data[close-1] == '\r' || data[close-1] == '\n') {
		close--
	}
	close--
	for open < close && data[open] != '{' {
		open++
	}
	if open >= close || data[close] != '}' {
		return 0, 0, nil, fmt.Errorf("JSONC object span is malformed")
	}
	v, ok := lookup(doc.Values, parent)
	obj, valid := v.(*jsonx.OrderedMap)
	if !ok || !valid {
		return 0, 0, nil, fmt.Errorf("JSONC parent is not an object")
	}
	name := strconv.Quote(key)
	multiline := bytes.Contains(data[open:close], []byte("\n")) || bytes.Contains(data[open:close], []byte("\r"))
	if obj.Len() == 0 {
		if multiline {
			return open + 1, open + 1, []byte(newlineOf(data) + inferIndent(data, open) + "  " + name + ": " + string(value)), nil
		}
		return open + 1, open + 1, []byte("\n  " + name + ": " + string(value) + "\n"), nil
	}
	keys := obj.Keys()
	lastPath := append(append([]configdoc.Step(nil), parent...), configdoc.Step{Key: keys[len(keys)-1]})
	last, ok, err := doc.Index.Value(lastPath...)
	if err != nil || !ok {
		return 0, 0, nil, fmt.Errorf("cannot locate last JSONC member: %v", err)
	}
	var trailingComment configdoc.Span
	foundComment := false
	for _, comment := range doc.Index.Comments() {
		if comment.Start >= last.End && comment.Start < close {
			trailingComment = comment
			foundComment = true
		}
	}
	if foundComment {
		indent := inferIndent(data, last.Start)
		if !multiline {
			if trailingComment.Start+1 >= trailingComment.End || data[trailingComment.Start+1] != '*' {
				return 0, 0, nil, fmt.Errorf("cannot insert after a single-line JSONC line comment")
			}
			commaBefore := findComma(data, last.End, trailingComment.Start, doc.Index.Comments())
			commaAfter := findComma(data, trailingComment.End, close, doc.Index.Comments())
			var out []byte
			if commaBefore < 0 && commaAfter < 0 {
				out = append(out, ',')
			}
			out = append(out, data[last.End:trailingComment.End]...)
			end := trailingComment.End
			if commaBefore < 0 && commaAfter >= 0 {
				out = append(out, data[end:commaAfter+1]...)
				end = commaAfter + 1
			}
			spaceStart := end
			for end < close && (data[end] == ' ' || data[end] == '\t') {
				out = append(out, data[end])
				end++
			}
			if end == spaceStart {
				out = append(out, ' ')
			}
			out = append(out, []byte(name+": "+string(value))...)
			return last.End, end, out, nil
		}
		end := lineEndAt(data, trailingComment.End)
		if end < len(data) && data[end] == '\r' {
			end++
		}
		if end < len(data) && data[end] == '\n' {
			end++
		}
		comma := findComma(data, last.End, end, doc.Index.Comments())
		var out []byte
		if comma < 0 {
			out = append(out, ',')
		}
		out = append(out, data[last.End:end]...)
		out = append(out, []byte(indent+name+": "+string(value)+newlineOf(data))...)
		return last.End, end, out, nil
	}
	if multiline {
		return last.End, last.End, []byte("," + newlineOf(data) + inferIndent(data, last.Start) + name + ": " + string(value)), nil
	}
	return last.End, last.End, []byte(", " + name + ": " + string(value)), nil
}
func addTOMLTable(doc *configdoc.Document, root *jsonx.OrderedMap, path []configdoc.Step, value any) (int, int, []byte, error) {
	if len(path) < 2 {
		return 0, 0, nil, fmt.Errorf("new TOML parent table is incomplete")
	}
	keys := make([]string, len(path))
	for i, s := range path {
		if s.IsIndex {
			return 0, 0, nil, fmt.Errorf("new TOML table path cannot contain array indices")
		}
		keys[i] = s.Key
	}
	normalized, err := semanticValue(value, configdoc.TOML)
	if err != nil {
		return 0, 0, nil, err
	}
	current := any(root)
	for _, key := range keys[:len(keys)-1] {
		m, ok := current.(*jsonx.OrderedMap)
		if !ok {
			return 0, 0, nil, fmt.Errorf("TOML parent %q is not a table", key)
		}
		next, exists := m.Get(key)
		if !exists {
			break
		}
		current = next
	}
	encoded, err := configdoc.EncodeValue(value, doc.Format)
	if err != nil {
		return 0, 0, nil, err
	}
	out := make([]byte, 0)
	if len(doc.Bytes) > 0 {
		if doc.Bytes[len(doc.Bytes)-1] != '\n' {
			out = append(out, []byte(newlineOf(doc.Bytes))...)
		}
		out = append(out, []byte(newlineOf(doc.Bytes))...)
	}
	out = append(out, '[')
	for i, key := range keys[:len(keys)-1] {
		if i > 0 {
			out = append(out, '.')
		}
		out = append(out, []byte(tomlKey(key))...)
	}
	out = append(out, ']')
	out = append(out, []byte(newlineOf(doc.Bytes))...)
	out = append(out, []byte(tomlKey(keys[len(keys)-1])+" = ")...)
	out = append(out, encoded...)
	out = append(out, []byte(newlineOf(doc.Bytes))...)
	current = any(root)
	for _, key := range keys[:len(keys)-1] {
		m := current.(*jsonx.OrderedMap)
		next, exists := m.Get(key)
		if !exists {
			next = jsonx.NewOrderedMap()
			m.Set(key, next)
		}
		child, ok := next.(*jsonx.OrderedMap)
		if !ok {
			return 0, 0, nil, fmt.Errorf("TOML parent %q is not a table", key)
		}
		current = child
	}
	current.(*jsonx.OrderedMap).Set(keys[len(keys)-1], normalized)
	return len(doc.Bytes), len(doc.Bytes), out, nil
}

func appendArray(doc *configdoc.Document, path []configdoc.Step, span configdoc.Span, items []any, value any) (int, int, []byte, error) {
	encoded, err := configdoc.EncodeValue(value, doc.Format)
	if err != nil {
		return 0, 0, nil, err
	}
	data := doc.Bytes
	open, close := span.Start, span.End-1
	if open < 0 || close < open || data[open] != '[' || data[close] != ']' {
		return 0, 0, nil, fmt.Errorf("array span is malformed")
	}
	if len(items) == 0 {
		if bytes.Contains(data[open:close], []byte("\n")) {
			return close, close, []byte("  " + string(encoded) + newlineOf(data) + inferIndent(data, close)), nil
		}
		return close, close, encoded, nil
	}
	lastPath := append(append([]configdoc.Step(nil), path...), configdoc.Step{Index: len(items) - 1, IsIndex: true})
	last, ok, err := doc.Index.Item(lastPath...)
	if err != nil || !ok {
		return 0, 0, nil, fmt.Errorf("cannot locate last array item")
	}
	for _, comment := range doc.Index.Comments() {
		if comment.Start < last.End || comment.Start >= close {
			continue
		}
		lineEnd := lineEndAt(data, comment.End)
		end := lineEnd
		if end < len(data) && data[end] == '\r' {
			end++
		}
		if end < len(data) && data[end] == '\n' {
			end++
		}
		closeLine := lineStartAt(data, close)
		if end > closeLine {
			continue
		}
		comma := findComma(data, last.End, comment.Start, doc.Index.Comments())
		var out []byte
		if comma < 0 {
			out = append(out, ',')
		}
		out = append(out, data[last.End:closeLine]...)
		out = append(out, []byte(inferIndent(data, last.Start)+string(encoded)+newlineOf(data))...)
		return last.End, closeLine, out, nil
	}
	sep := []byte(", ")
	if bytes.Contains(data[open:close], []byte("\n")) {
		sep = []byte("," + newlineOf(data) + inferIndent(data, last.Start))
	}
	return last.End, last.End, append(sep, encoded...), nil
}
func semanticValue(value any, format configdoc.Format) (any, error) {
	encoded, err := configdoc.EncodeValue(value, format)
	if err != nil {
		return nil, err
	}
	var source []byte
	if format == configdoc.JSONC {
		source = append([]byte(`{"value":`), encoded...)
		source = append(source, '}')
	} else {
		source = append([]byte("value = "), encoded...)
	}
	doc, err := configdoc.Parse(source, format)
	if err != nil {
		return nil, fmt.Errorf("parse encoded value: %w", err)
	}
	value, ok := doc.Values.Get("value")
	if !ok {
		return nil, fmt.Errorf("encoded value was not retained")
	}
	return value, nil
}

func planCommentedSet(doc *configdoc.Document, path []configdoc.Step, old, next any) ([]Hunk, error) {
	var hunks []Hunk
	var walk func([]configdoc.Step, any, any) error
	walk = func(at []configdoc.Step, before, after any) error {
		if reflect.DeepEqual(before, after) {
			return nil
		}
		span, ok, err := doc.Index.Value(at...)
		if err != nil || !ok {
			return fmt.Errorf("cannot locate value while preserving comments")
		}
		comments := doc.Index.Comments()
		if hasCommentWithin(comments, span) {
			oldMap, oldIsMap := before.(*jsonx.OrderedMap)
			newMap, newIsMap := after.(*jsonx.OrderedMap)
			if oldIsMap && newIsMap && sameKeys(oldMap, newMap) {
				for _, key := range oldMap.Keys() {
					oldValue, _ := oldMap.Get(key)
					newValue, _ := newMap.Get(key)
					if err := walk(appendStep(at, configdoc.Step{Key: key}), oldValue, newValue); err != nil {
						return err
					}
				}
				return nil
			}
			oldItems, oldArray := before.([]any)
			newItems, newArray := after.([]any)
			if oldArray && newArray && len(oldItems) == len(newItems) {
				for i := range oldItems {
					if err := walk(appendStep(at, configdoc.Step{Index: i, IsIndex: true}), oldItems[i], newItems[i]); err != nil {
						return err
					}
				}
				return nil
			}
			return fmt.Errorf("cannot replace a commented config value with a different structure without relocating comments")
		}
		replacement, err := configdoc.EncodeValue(after, doc.Format)
		if err != nil {
			return err
		}
		hunks = append(hunks, Hunk{Start: span.Start, End: span.End, Before: append([]byte(nil), doc.Bytes[span.Start:span.End]...), After: replacement})
		return nil
	}
	if err := walk(path, old, next); err != nil {
		return nil, err
	}
	return hunks, nil
}

func orderedLike(before, after any) any {
	oldMap, oldIsMap := before.(*jsonx.OrderedMap)
	newMap, newIsMap := after.(*jsonx.OrderedMap)
	if oldIsMap && newIsMap && sameKeys(oldMap, newMap) {
		out := jsonx.NewOrderedMap()
		for _, key := range oldMap.Keys() {
			oldValue, _ := oldMap.Get(key)
			newValue, _ := newMap.Get(key)
			out.Set(key, orderedLike(oldValue, newValue))
		}
		return out
	}
	oldItems, oldArray := before.([]any)
	newItems, newArray := after.([]any)
	if oldArray && newArray && len(oldItems) == len(newItems) {
		out := make([]any, len(newItems))
		for i := range newItems {
			out[i] = orderedLike(oldItems[i], newItems[i])
		}
		return out
	}
	return after
}

func sameKeys(a, b *jsonx.OrderedMap) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, key := range a.Keys() {
		if _, ok := b.Get(key); !ok {
			return false
		}
	}
	return true
}

func appendStep(path []configdoc.Step, step configdoc.Step) []configdoc.Step {
	return append(append([]configdoc.Step(nil), path...), step)
}

func applyHunks(data []byte, hunks []Hunk) []byte {
	updated := append([]byte(nil), data...)
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		updated = splice(updated, h.Start, h.End, h.After)
	}
	return updated
}

func preserveComments(data []byte, start, end int, comments []configdoc.Span) []byte {
	var out []byte
	for i := start; i < end; i++ {
		keep := data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n'
		if !keep {
			for _, comment := range comments {
				if i >= comment.Start && i < comment.End {
					keep = true
					break
				}
			}
		}
		if keep {
			out = append(out, data[i])
		}
	}
	return out
}

func hasCommentWithin(comments []configdoc.Span, span configdoc.Span) bool {
	for _, comment := range comments {
		if comment.Start < span.End && comment.End > span.Start {
			return true
		}
	}
	return false
}

func removeArrayItem(doc *configdoc.Document, parent []configdoc.Step, index int) (int, int, []byte, error) {
	v, ok := lookup(doc.Values, parent)
	items, valid := v.([]any)
	if !ok || !valid || index < 0 || index >= len(items) {
		return 0, 0, nil, fmt.Errorf("array item does not exist")
	}
	path := append(append([]configdoc.Step(nil), parent...), configdoc.Step{Index: index, IsIndex: true})
	item, found, err := doc.Index.Item(path...)
	if err != nil || !found {
		return 0, 0, nil, fmt.Errorf("cannot locate array item")
	}
	if len(items) <= 1 {
		return item.Start, item.End, nil, nil
	}
	if index == 0 {
		end := item.End
		for end < len(doc.Bytes) && (doc.Bytes[end] == ' ' || doc.Bytes[end] == '\t') {
			end++
		}
		if end < len(doc.Bytes) && doc.Bytes[end] == ',' {
			end++
		}
		return item.Start, end, nil, nil
	}
	priorPath := append(append([]configdoc.Step(nil), parent...), configdoc.Step{Index: index - 1, IsIndex: true})
	prior, found, err := doc.Index.Item(priorPath...)
	if err != nil || !found {
		return 0, 0, nil, fmt.Errorf("cannot locate prior array item")
	}
	comma := findComma(doc.Bytes, prior.End, item.Start, doc.Index.Comments())
	if comma < 0 {
		return 0, 0, nil, fmt.Errorf("cannot locate array separator")
	}
	if index == len(items)-1 {
		end := item.End
		for end < len(doc.Bytes) && (doc.Bytes[end] == ' ' || doc.Bytes[end] == '\t') {
			end++
		}
		return comma, end, nil, nil
	}
	nextPath := append(append([]configdoc.Step(nil), parent...), configdoc.Step{Index: index + 1, IsIndex: true})
	next, found, err := doc.Index.Item(nextPath...)
	if err != nil || !found {
		return 0, 0, nil, fmt.Errorf("cannot locate following array item")
	}
	commaAfter := findComma(doc.Bytes, item.End, next.Start, doc.Index.Comments())
	if commaAfter < 0 {
		return 0, 0, nil, fmt.Errorf("cannot locate following array separator")
	}
	for _, c := range doc.Index.Comments() {
		if c.Start >= comma && c.End <= next.Start {
			return comma, item.End, append([]byte(nil), doc.Bytes[comma+1:item.Start]...), nil
		}
	}
	end := next.Start
	spacing := append([]byte(nil), doc.Bytes[commaAfter+1:end]...)
	return comma, end, append([]byte(","), spacing...), nil
}
func removeJSONMember(doc *configdoc.Document, member configdoc.Span, parentPath []configdoc.Step) (int, int, []byte, error) {
	data := doc.Bytes
	v, ok := lookup(doc.Values, parentPath)
	obj, valid := v.(*jsonx.OrderedMap)
	if !ok || !valid {
		return 0, 0, nil, fmt.Errorf("JSONC parent is not an object")
	}
	line := lineStartAt(data, member.Start)
	end := lineEndAt(data, member.End)
	if end < len(data) && data[end] == '\r' {
		end++
	}
	if end < len(data) && data[end] == '\n' {
		end++
	}
	comment := false
	for _, c := range doc.Index.Comments() {
		if c.Start >= member.End && c.Start < lineEndAt(data, member.End) {
			comment = true
			break
		}
	}
	trailing := []byte(nil)
	if comment {
		trailing = append([]byte(nil), data[member.End:end]...)
	}
	commaAfter := findComma(data, member.End, lineEndAt(data, member.End), doc.Index.Comments())
	if commaAfter >= 0 {
		if comment {
			idx := commaAfter - member.End
			trailing = append(trailing[:idx], trailing[idx+1:]...)
		}
		return line, end, trailing, nil
	}
	key := findMemberKey(doc, parentPath, member)
	keys := obj.Keys()
	for i, k := range keys {
		if k != key || i == 0 {
			continue
		}
		pp := append(append([]configdoc.Step(nil), parentPath...), configdoc.Step{Key: keys[i-1]})
		prior, found, e := doc.Index.Value(pp...)
		if e != nil || !found {
			return 0, 0, nil, fmt.Errorf("cannot locate preceding JSONC member")
		}
		comma := findComma(data, prior.End, member.Start, doc.Index.Comments())
		if comma >= 0 {
			repl := append([]byte(nil), data[comma+1:line]...)
			repl = append(repl, trailing...)
			return comma, end, repl, nil
		}
		break
	}
	return line, end, trailing, nil
}
func removeTOMLMember(data []byte, member configdoc.Span) (int, int, []byte, error) {
	start := lineStartAt(data, member.Start)
	indentEnd := start
	for indentEnd < len(data) && (data[indentEnd] == ' ' || data[indentEnd] == '\t') {
		indentEnd++
	}
	end := member.End
	for end < len(data) && data[end] != '\n' && data[end] != '\r' {
		if data[end] == '#' {
			return start, end, append(append([]byte(nil), data[start:indentEnd]...), data[member.End:end]...), nil
		}
		end++
	}
	if end < len(data) && data[end] == '\r' {
		end++
	}
	if end < len(data) && data[end] == '\n' {
		end++
	}
	return start, end, nil, nil
}
func findMemberKey(doc *configdoc.Document, parent []configdoc.Step, member configdoc.Span) string {
	v, _ := lookup(doc.Values, parent)
	obj, _ := v.(*jsonx.OrderedMap)
	if obj == nil {
		return ""
	}
	for _, k := range obj.Keys() {
		p := append(append([]configdoc.Step(nil), parent...), configdoc.Step{Key: k})
		s, ok, _ := doc.Index.Member(p...)
		if ok && s.Start == member.Start {
			return k
		}
	}
	return ""
}
func findComma(data []byte, start, end int, comments []configdoc.Span) int {
	for i := start; i < end; i++ {
		if data[i] != ',' {
			continue
		}
		inside := false
		for _, c := range comments {
			if i >= c.Start && i < c.End {
				inside = true
				break
			}
		}
		if !inside {
			return i
		}
	}
	return -1
}
func lookup(root any, path []configdoc.Step) (any, bool) {
	cur := root
	for _, s := range path {
		if s.IsIndex {
			a, ok := cur.([]any)
			if !ok || s.Index < 0 || s.Index >= len(a) {
				return nil, false
			}
			cur = a[s.Index]
		} else {
			m, ok := cur.(*jsonx.OrderedMap)
			if !ok {
				return nil, false
			}
			v, ok := m.Get(s.Key)
			if !ok {
				return nil, false
			}
			cur = v
		}
	}
	return cur, true
}
func setValue(root any, path []configdoc.Step, v any) bool {
	if len(path) == 0 {
		return false
	}
	p, ok := lookup(root, path[:len(path)-1])
	if !ok {
		return false
	}
	s := path[len(path)-1]
	if s.IsIndex {
		a, ok := p.([]any)
		if !ok || s.Index < 0 || s.Index >= len(a) {
			return false
		}
		a[s.Index] = v
		return true
	}
	m, ok := p.(*jsonx.OrderedMap)
	if !ok {
		return false
	}
	m.Set(s.Key, v)
	return true
}
func cloneMap(m *jsonx.OrderedMap) *jsonx.OrderedMap {
	n := jsonx.NewOrderedMap()
	if m != nil {
		for _, k := range m.Keys() {
			v, _ := m.Get(k)
			n.Set(k, cloneValue(v))
		}
	}
	return n
}
func cloneValue(v any) any {
	switch x := v.(type) {
	case *jsonx.OrderedMap:
		return cloneMap(x)
	case []any:
		y := make([]any, len(x))
		for i, v := range x {
			y[i] = cloneValue(v)
		}
		return y
	case map[string]any:
		y := make(map[string]any, len(x))
		for k, v := range x {
			y[k] = cloneValue(v)
		}
		return y
	default:
		return v
	}
}
func splice(data []byte, start, end int, repl []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(repl))
	out = append(out, data[:start]...)
	out = append(out, repl...)
	out = append(out, data[end:]...)
	return out
}
func newlineOf(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}
func lineStartAt(b []byte, n int) int {
	if n > len(b) {
		n = len(b)
	}
	for n > 0 && b[n-1] != '\n' {
		n--
	}
	return n
}
func lineEndAt(b []byte, n int) int {
	for n < len(b) && b[n] != '\n' && b[n] != '\r' {
		n++
	}
	return n
}
func inferIndent(b []byte, n int) string {
	start := lineStartAt(b, n)
	i := start
	for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
		i++
	}
	return string(b[start:i])
}
func tomlKey(s string) string {
	if s != "" {
		ok := true
		for _, r := range s {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				ok = false
				break
			}
		}
		if ok {
			return s
		}
	}
	return strconv.Quote(s)
}
