// Package configdoc parses human-authored configuration documents while retaining
// the source positions needed for byte-preserving edits.
package configdoc

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mschulkind-oss/yolo-jail/internal/json5"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/tomlx"
	pelletier "github.com/pelletier/go-toml/v2/unstable"
)

// Format identifies the syntax of one document.
type Format uint8

const (
	JSONC Format = 1
	TOML  Format = 2
)

// Step selects an object member or array element.
type Step struct {
	Key     string
	Index   int
	IsIndex bool
}

// Span is a half-open byte range in the original document.
type Span struct{ Start, End int }

// Document is one parsed document and its source index.
type Document struct {
	Format Format
	Bytes  []byte
	Values *jsonx.OrderedMap
	Index  *Index
}

// Index stores value and structural spans in original byte coordinates.
type Index struct {
	data     []byte
	values   map[string]Span
	members  map[string]Span
	tables   map[string]Span
	items    map[string]Span
	comments []Span
	jsonc    json5Indexer
}

// Parse decodes a JSONC or TOML document with an object at its root.
func Parse(data []byte, format Format) (*Document, error) {
	var values *jsonx.OrderedMap
	ix := &Index{data: append([]byte(nil), data...), values: map[string]Span{}, members: map[string]Span{}, tables: map[string]Span{}, items: map[string]Span{}}
	switch format {
	case JSONC:
		decoded, err := json5.Decode(data)
		if err != nil {
			return nil, err
		}
		var ok bool
		values, ok = decoded.(*jsonx.OrderedMap)
		if !ok {
			return nil, fmt.Errorf("configdoc: document root is %T, want object", decoded)
		}
		jix, err := json5.NewIndex(data)
		if err != nil {
			return nil, err
		}
		// Values are located on demand because JSON5's index owns duplicate detection.
		ix.jsonc = jix
		ix.comments = jsonComments(data)
	case TOML:
		var err error
		values, err = tomlx.DecodeOrdered(data)
		if err != nil {
			return nil, err
		}
		if err := validateTOMLConfigValues(values, nil); err != nil {
			return nil, err
		}
		if err := ix.indexTOML(data); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("configdoc: unsupported format %d", format)
	}
	return &Document{Format: format, Bytes: append([]byte(nil), data...), Values: values, Index: ix}, nil
}

// json5Index is populated only for JSONC documents.
type json5Indexer interface {
	Locate(...json5.Step) (json5.Span, bool, error)
}

// Value returns the exact source range of a value at path.
func (ix *Index) Value(path ...Step) (Span, bool, error) {
	if ix.jsonc != nil {
		steps := make([]json5.Step, len(path))
		for i, step := range path {
			if step.IsIndex {
				steps[i] = json5.Elem(step.Index)
			} else {
				steps[i] = json5.Key(step.Key)
			}
		}
		span, ok, err := ix.jsonc.Locate(steps...)
		return Span{Start: span.Start, End: span.End}, ok, err
	}
	span, ok := ix.lookup(ix.values, path)
	return span, ok, nil
}

func (ix *Index) lookup(index map[string]Span, path []Step) (Span, bool) {
	span, ok := index[pathKey(path)]
	return span, ok
}

// Member returns the key/value assignment containing the named TOML key (or
// the JSONC key and its value), excluding trailing comments.
func (ix *Index) Member(path ...Step) (Span, bool, error) {
	if ix.jsonc != nil {
		value, ok, err := ix.Value(path...)
		if err != nil || !ok {
			return Span{}, ok, err
		}
		start, err := jsonMemberStart(ix.data, value.Start)
		if err != nil {
			return Span{}, false, err
		}
		return Span{Start: start, End: value.End}, true, nil
	}
	span, ok := ix.lookup(ix.members, path)
	return span, ok, nil
}

// Table returns the source extent of a table/object at path. TOML's extent
// ends immediately before the next table header; JSONC returns the object value.
func (ix *Index) Table(path ...Step) (Span, bool, error) {
	if ix.jsonc != nil {
		span, ok, err := ix.Value(path...)
		if err != nil || !ok {
			return Span{}, ok, err
		}
		start := span.Start
		for start < span.End && (ix.data[start] == ' ' || ix.data[start] == '\t' || ix.data[start] == '\r' || ix.data[start] == '\n') {
			start++
		}
		if start >= span.End || ix.data[start] != '{' {
			return Span{}, false, nil
		}
		return span, true, nil
	}
	span, ok := ix.lookup(ix.tables, path)
	return span, ok, nil
}

// Item returns the exact range of an array element at path.
func (ix *Index) Item(path ...Step) (Span, bool, error) {
	if len(path) == 0 || !path[len(path)-1].IsIndex {
		return Span{}, false, nil
	}
	span, ok := ix.lookup(ix.items, path)
	if ix.jsonc != nil {
		return ix.Value(path...)
	}
	return span, ok, nil
}

// Position converts a byte offset to a 1-based line and UTF-8 character column.
func (ix *Index) Position(offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(ix.data) {
		offset = len(ix.data)
	}
	line = 1
	lineStart := 0
	for i := 0; i < offset; i++ {
		if ix.data[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, 1 + utf8.RuneCount(ix.data[lineStart:offset])
}

// Comments returns every comment's byte span in document order. Comments are
// not attached to keys or values.
func (ix *Index) Comments() []Span { return append([]Span(nil), ix.comments...) }

func pathKey(path []Step) string {
	var b strings.Builder
	for _, step := range path {
		if step.IsIndex {
			fmt.Fprintf(&b, "#%d;", step.Index)
		} else {
			fmt.Fprintf(&b, "%d:%s", len(step.Key), step.Key)
		}
	}
	return b.String()
}

func jsonComments(data []byte) []Span {
	var out []Span
	quote := byte(0)
	escaped := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			start := i
			i += 2
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
			out = append(out, Span{start, i})
			i--
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			start := i
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			if i+1 < len(data) {
				i += 2
			}
			out = append(out, Span{start, i})
			i--
		}
	}
	return out
}

func jsonMemberStart(data []byte, valueStart int) (int, error) {
	// Scan from the document start; the last colon outside strings/comments before the
	// value is the member separator.
	quote := byte(0)
	escaped := false
	colon := -1
	for i := 0; i < valueStart; i++ {
		c := data[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		if c == ':' {
			colon = i
		}
	}
	if colon < 0 {
		return 0, fmt.Errorf("configdoc: cannot find JSONC member boundary at byte %d", valueStart)
	}
	start := colon - 1
	for start >= 0 && (data[start] == ' ' || data[start] == '\t' || data[start] == '\r' || data[start] == '\n') {
		start--
	}
	if start < 0 {
		return 0, fmt.Errorf("configdoc: malformed JSONC key before byte %d", valueStart)
	}
	if data[start] == '\'' || data[start] == '"' {
		q := data[start]
		start--
		for start >= 0 {
			if data[start] == q {
				n := 0
				for k := start - 1; k >= 0 && data[k] == '\\'; k-- {
					n++
				}
				if n%2 == 0 {
					break
				}
			}
			start--
		}
		if start < 0 {
			return 0, fmt.Errorf("configdoc: malformed quoted key")
		}
		return start, nil
	}
	for start >= 0 && data[start] != '{' && data[start] != '}' && data[start] != ',' && data[start] != '\n' && data[start] != '\r' {
		start--
	}
	start++
	for start < len(data) && (data[start] == ' ' || data[start] == '\t' || data[start] == '\r' || data[start] == '\n') {
		start++
	}
	return start, nil
}

func validateTOMLConfigValues(value any, path []string) error {
	switch v := value.(type) {
	case time.Time:
		return fmt.Errorf("configdoc: TOML date/time at %s is not a supported config scalar", strings.Join(path, "."))
	case *jsonx.OrderedMap:
		for _, key := range v.Keys() {
			child, _ := v.Get(key)
			if err := validateTOMLConfigValues(child, append(append([]string(nil), path...), key)); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			name := fmt.Sprintf("[%d]", i)
			if err := validateTOMLConfigValues(child, append(append([]string(nil), path...), name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ix *Index) indexTOML(data []byte) error {
	p := pelletier.Parser{KeepComments: true}
	p.Reset(data)
	var table []Step
	arrayContexts := map[string]struct {
		keys []string
		path []Step
	}{}
	arrayTableCounts := map[string]int{}
	type tableRange struct {
		key   string
		start int
		path  []Step
	}
	var tableRanges []tableRange
	ix.comments = tomlComments(data)
	for p.NextExpression() {
		n := p.Expression()
		if n == nil {
			continue
		}
		switch n.Kind {
		case pelletier.Table, pelletier.ArrayTable:
			keys := nodeKeys(n.Key())
			var parentKeys []string
			var parentPath []Step
			for _, context := range arrayContexts {
				if len(context.keys) < len(keys) && len(context.keys) > len(parentKeys) && stringPrefix(keys, context.keys) {
					parentKeys, parentPath = context.keys, context.path
				}
			}
			next := append(append([]Step(nil), parentPath...), stringSteps(keys[len(parentKeys):])...)
			if n.Kind == pelletier.ArrayTable {
				k := pathKey(next)
				idx := arrayTableCounts[k]
				arrayTableCounts[k]++
				next = append(next, Step{Index: idx, IsIndex: true})
				for name, context := range arrayContexts {
					if len(context.keys) > len(keys) && stringPrefix(context.keys, keys) {
						delete(arrayContexts, name)
					}
				}
				arrayContexts[pathKey(stringSteps(keys))] = struct {
					keys []string
					path []Step
				}{append([]string(nil), keys...), append([]Step(nil), next...)}
			}
			table = next
			it := n.Key()
			first := it.Node()
			start := lineStart(data, int(first.Raw.Offset))
			key := pathKey(table)
			ix.tables[key] = Span{start, lineEnd(data, start)}
			tableRanges = append(tableRanges, tableRange{key, start, append([]Step(nil), table...)})
		case pelletier.KeyValue:
			keys := nodeKeys(n.Key())
			full := append(append([]Step(nil), table...), stringSteps(keys)...)
			value := n.Value()
			if value == nil {
				continue
			}
			keyIter := n.Key()
			var firstKey, lastKey *pelletier.Node
			for keyIter.Next() {
				if firstKey == nil {
					firstKey = keyIter.Node()
				}
				lastKey = keyIter.Node()
			}
			valueStart := skipTOMLSpace(data, int(lastKey.Raw.Offset)+int(lastKey.Raw.Length))
			for valueStart < len(data) && data[valueStart] != '=' {
				valueStart++
			}
			if valueStart < len(data) {
				valueStart = skipTOMLSpace(data, valueStart+1)
			}
			span := scanTOMLValue(data, valueStart)
			ix.walkTOMLValue(value, full, span)
			memberStart := int(firstKey.Raw.Offset)
			key := pathKey(full)
			ix.members[key] = Span{memberStart, span.End}
			if value.Kind == pelletier.InlineTable {
				ix.tables[key] = span
			}
		}
	}
	if err := p.Error(); err != nil {
		return err
	}
	// Complete table ranges through their descendants, stopping at the next
	// header outside this table's ancestry.
	for i, tr := range tableRanges {
		end := len(data)
		for j := i + 1; j < len(tableRanges); j++ {
			if !stepPrefix(tableRanges[j].path, tr.path) {
				end = tableRanges[j].start
				break
			}
		}
		ix.tables[tr.key] = Span{tr.start, end}
		if len(tr.path) > 0 && tr.path[len(tr.path)-1].IsIndex {
			ix.items[tr.key] = Span{tr.start, end}
		}
	}
	return nil
}

func stepPrefix(path, prefix []Step) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

func stringPrefix(value, prefix []string) bool {
	if len(prefix) > len(value) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

func nodeKeys(it pelletier.Iterator) []string {
	var out []string
	for it.Next() {
		n := it.Node()
		if n != nil {
			out = append(out, string(n.Data))
		}
	}
	return out
}
func stringSteps(keys []string) []Step {
	out := make([]Step, len(keys))
	for i, k := range keys {
		out[i] = Step{Key: k}
	}
	return out
}

func skipTOMLArrayTrivia(data []byte, offset int) int {
	for {
		offset = skipTOMLSpace(data, offset)
		if offset < len(data) && data[offset] == ',' {
			offset++
			continue
		}
		if offset < len(data) && data[offset] == '#' {
			for offset < len(data) && data[offset] != '\n' {
				offset++
			}
			continue
		}
		return offset
	}
}

func (ix *Index) walkTOMLValue(n *pelletier.Node, path []Step, span Span) {
	if n == nil {
		return
	}
	if span.End < span.Start {
		span = Span{}
	}
	key := pathKey(path)
	ix.values[key] = span
	switch n.Kind {
	case pelletier.Array:
		idx := 0
		cursor := span.Start + 1
		it := n.Children()
		for it.Next() {
			child := it.Node()
			if child.Kind == pelletier.Comment {
				continue
			}
			cursor = skipTOMLArrayTrivia(ix.data, cursor)
			childSpan := scanTOMLValue(ix.data, cursor)
			if childSpan.End <= childSpan.Start {
				childSpan = Span{cursor, cursor}
			}
			childPath := append(append([]Step(nil), path...), Step{Index: idx, IsIndex: true})
			ix.walkTOMLValue(child, childPath, childSpan)
			ix.items[pathKey(childPath)] = childSpan
			idx++
			cursor = childSpan.End
		}
	case pelletier.InlineTable:
		ix.tables[key] = span
		it := n.Children()
		for it.Next() {
			kv := it.Node()
			if kv.Kind != pelletier.KeyValue {
				continue
			}
			keys := nodeKeys(kv.Key())
			childPath := append(append([]Step(nil), path...), stringSteps(keys)...)
			v := kv.Value()
			keyIter := kv.Key()
			var lastKey *pelletier.Node
			for keyIter.Next() {
				lastKey = keyIter.Node()
			}
			valueStart := skipTOMLSpace(ix.data, int(lastKey.Raw.Offset)+int(lastKey.Raw.Length))
			for valueStart < len(ix.data) && ix.data[valueStart] != '=' {
				valueStart++
			}
			if valueStart < len(ix.data) {
				valueStart = skipTOMLSpace(ix.data, valueStart+1)
			}
			childSpan := scanTOMLValue(ix.data, valueStart)
			rr := kv.Raw
			ix.walkTOMLValue(v, childPath, childSpan)
			ix.members[pathKey(childPath)] = Span{int(rr.Offset), childSpan.End}
		}
	}
}
