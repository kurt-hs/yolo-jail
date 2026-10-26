// Package tomlx decodes TOML where yolo reads it: mise.toml / mise.jail.toml
// venv discovery (feeds the venv-shadow mount set and a jail-made-venv rmtree),
// and the TOML agent-config surfaces the composition codec reads and writes,
// such as codex's ~/.codex/config.toml.
//
// Backed by github.com/BurntSushi/toml (the de-facto standard, vendored) rather
// than hand-written — TOML is genuinely complex and a vetted parser is the
// happy path. What this package must get right is the SHAPES its callers read
// (str vs dict entries, nested env._.python.venv, later-file-wins), pinned by a
// fixture corpus in the test.
//
// The corpus's expected values were first taken from Python tomllib's observed
// behavior over the real config shapes.
package tomlx

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// Decode parses TOML bytes into a generic map, matching tomllib.load's shape:
// tables -> map[string]any, arrays -> []any, and scalars to Go's bool/int64/
// float64/string/time. Returns an error on malformed TOML (tomllib raises).
func Decode(data []byte) (map[string]any, error) {
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// DecodeFile reads and parses a TOML file.
func DecodeFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// DecodeOrdered parses TOML into a *jsonx.OrderedMap that preserves the
// document's key order (top-level keys in file order; nested tables likewise),
// matching what Python's tomllib.load + dict insertion order yields. Scalars
// map to Go bool/int64/float64/string; tables to nested *jsonx.OrderedMap;
// arrays to []any. Used by codex's config.toml round-trip where top-level user
// key order must be preserved on re-emit.
//
// Note tomllib preserves the ORDER a key first appears in the source; BurntSushi
// MetaData.Keys() reports keys in that same document order, which we replay.
func DecodeOrdered(data []byte) (*jsonx.OrderedMap, error) {
	var raw map[string]any
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, err
	}
	keys := md.Keys()

	// A path is a table prefix only when its semantic value is a map. Arrays
	// of inline tables also produce nested metadata paths, but their parent
	// must remain an array value rather than an empty table.
	isPrefix := make(map[string]bool)
	for _, key := range keys {
		if len(key) == 0 {
			continue
		}
		if _, ok := rawMapAt(raw, key); ok {
			isPrefix[joinKey(key)] = true
		}
	}

	root := jsonx.NewOrderedMap()
	processedValues := make(map[string]bool)
	for keyIndex, key := range keys {
		parts := []string(key)
		cur := root
		var srcCur any = raw
		for i, p := range parts {
			sm, ok := srcCur.(map[string]any)
			if !ok {
				break
			}
			val, present := sm[p]
			if !present {
				break
			}
			if i == len(parts)-1 {
				if isPrefix[joinKey(key)] {
					// Table node: ensure it exists (children fill it in order).
					ensureChild(cur, p)
				} else {
					id := joinKey(key)
					if processedValues[id] {
						break
					}
					cur.Set(p, convertTOMLValueOrdered(val, keys[keyIndex:], key))
					processedValues[id] = true
				}
			} else {
				if _, isTable := val.(map[string]any); !isTable {
					break
				}
				cur = ensureChild(cur, p)
				srcCur = val
			}
		}
	}
	return root, nil
}

func rawMapAt(root map[string]any, path toml.Key) (map[string]any, bool) {
	var current any = root
	for _, part := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	m, ok := current.(map[string]any)
	return m, ok
}

func joinKey(k toml.Key) string {
	var encoded strings.Builder
	for _, part := range k {
		encoded.WriteString(strconv.Itoa(len(part)))
		encoded.WriteByte(':')
		encoded.WriteString(part)
	}
	return encoded.String()
}

// ensureChild returns the *jsonx.OrderedMap at key, creating it if absent.
func ensureChild(m *jsonx.OrderedMap, key string) *jsonx.OrderedMap {
	if v, ok := m.Get(key); ok {
		if om, isMap := v.(*jsonx.OrderedMap); isMap {
			return om
		}
	}
	child := jsonx.NewOrderedMap()
	m.Set(key, child)
	return child
}

// convertTOMLValue converts BurntSushi-decoded leaves into the ordered model.
// Maps in ordinary tables are assembled from MetaData.Keys; map order inside
// arrays is supplied by the corresponding source-order metadata segment.
func convertTOMLValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return orderedMapLexical(t)
	case []map[string]any:
		arr := make([]any, len(t))
		for i, e := range t {
			arr[i] = orderedMapLexical(e)
		}
		return arr
	case []any:
		arr := make([]any, len(t))
		for i, e := range t {
			arr[i] = convertTOMLValue(e)
		}
		return arr
	default:
		return v
	}
}

func convertTOMLValueOrdered(value any, order []toml.Key, prefix toml.Key) any {
	switch values := value.(type) {
	case []any:
		out := make([]any, len(values))
		if segments := arrayTableMetadataSegments(order, prefix); len(segments) > 1 {
			for i, item := range values {
				if i >= len(segments) {
					out[i] = convertTOMLValue(item)
					continue
				}
				if row, ok := item.(map[string]any); ok {
					out[i] = orderedMapFromMetadata(row, prefix, segments[i])
				} else {
					out[i] = convertTOMLValue(item)
				}
			}
			return out
		}
		remaining := order
		for i, item := range values {
			if m, ok := item.(map[string]any); ok {
				remaining = trimExactPath(remaining, prefix)
				n := metadataLeafCount(m)
				if n > len(remaining) {
					n = len(remaining)
				}
				out[i] = orderedMapFromMetadata(m, prefix, remaining[:n])
				remaining = remaining[n:]
			} else if nested, ok := item.([]any); ok {
				n := metadataNestedMapCount(nested)
				if n > len(remaining) {
					n = len(remaining)
				}
				out[i] = convertTOMLValueOrdered(nested, remaining[:n], prefix)
				remaining = remaining[n:]
			} else {
				out[i] = convertTOMLValue(item)
			}
		}
		return out
	case []map[string]any:
		items := make([]any, len(values))
		for i, m := range values {
			items[i] = m
		}
		return convertTOMLValueOrdered(items, order, prefix)
	case map[string]any:
		return orderedMapFromMetadata(values, prefix, order)
	default:
		return value
	}
}

func orderedMapFromMetadata(raw map[string]any, prefix toml.Key, order []toml.Key) *jsonx.OrderedMap {
	ordered := jsonx.NewOrderedMap()
	for _, path := range order {
		if !tomlKeyPrefix(path, prefix) || len(path) <= len(prefix) {
			continue
		}
		key := path[len(prefix)]
		if _, exists := raw[key]; exists {
			if _, already := ordered.Get(key); !already {
				ordered.Set(key, nil)
			}
		}
	}
	if ordered.Len() == 0 {
		return orderedMapLexical(raw)
	}
	for _, key := range append([]string(nil), ordered.Keys()...) {
		childPrefix := append(append(toml.Key(nil), prefix...), key)
		childOrder := make([]toml.Key, 0)
		for _, path := range order {
			if tomlKeyPrefix(path, childPrefix) {
				childOrder = append(childOrder, path)
			}
		}
		ordered.Set(key, convertTOMLValueOrdered(raw[key], childOrder, childPrefix))
	}
	// Preserve any semantic field the parser reported without a metadata row.
	for key, value := range raw {
		if _, ok := ordered.Get(key); !ok {
			ordered.Set(key, convertTOMLValue(value))
		}
	}
	return ordered
}

func metadataLeafCount(value any) int {
	switch v := value.(type) {
	case map[string]any:
		count := 0
		for _, child := range v {
			switch child.(type) {
			case map[string]any:
				count += 1 + metadataLeafCount(child)
			case []any, []map[string]any:
				count += 1 + metadataNestedMapCount(child)
			default:
				count++
			}
		}
		if count == 0 {
			return 1
		}
		return count
	case []any, []map[string]any:
		return 1 + metadataNestedMapCount(v)
	default:
		return 1
	}
}

func metadataNestedMapCount(value any) int {
	count := 0
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			switch child.(type) {
			case map[string]any:
				count += metadataLeafCount(child)
			case []any, []map[string]any:
				count += metadataNestedMapCount(child)
			}
		}
	case []map[string]any:
		for _, child := range v {
			count += metadataLeafCount(child)
		}
	}
	return count
}

func arrayTableMetadataSegments(order []toml.Key, prefix toml.Key) [][]toml.Key {
	var starts []int
	for i, path := range order {
		if equalTOMLKey(path, prefix) {
			starts = append(starts, i)
		}
	}
	if len(starts) < 2 {
		return nil
	}
	segments := make([][]toml.Key, len(starts))
	for i, start := range starts {
		end := len(order)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		segments[i] = order[start:end]
	}
	return segments
}

func equalTOMLKey(left, right toml.Key) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func trimExactPath(order []toml.Key, prefix toml.Key) []toml.Key {
	if len(order) > 0 && len(order[0]) == len(prefix) && tomlKeyPrefix(order[0], prefix) {
		return order[1:]
	}
	return order
}
func tomlKeyPrefix(path, prefix toml.Key) bool {
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
func orderedMapLexical(raw map[string]any) *jsonx.OrderedMap {
	out := jsonx.NewOrderedMap()
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.Set(key, convertTOMLValue(raw[key]))
	}
	return out
}

// VenvValue extracts env._.python.venv from a parsed mise config, walking the
// nested tables the way the entrypoint's venv-discovery does
// (`env -> _ -> python -> venv`). Returns (value, true) if the whole chain is
// present, else (nil, false).
// entrypoint/shell.py.
func VenvValue(cfg map[string]any) (any, bool) {
	var cur any = cfg
	for _, key := range []string{"env", "_", "python", "venv"} {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// MiseVenvPath resolves the venv path from a set of mise config files in
// priority order (highest-priority first, first hit wins), mirroring the
// entrypoint's shell.py discovery:
// - for each file, read env._.python.venv;
// - the FIRST file that has a value wins;
// - a string value is the path directly;
// - a table value must have create=true (else "no venv"), then path (default
// ".venv"); a non-string/absent -> "no venv".
//
// Returns (path, true) when a venv should be created, or ("", false) when none.
func MiseVenvPath(files []string) (string, bool) {
	var v any
	found := false
	for _, f := range files {
		cfg, err := DecodeFile(f)
		if err != nil {
			continue
		}
		if val, ok := VenvValue(cfg); ok {
			v = val
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case map[string]any:
		create, _ := t["create"].(bool)
		if !create {
			return "", false
		}
		path, ok := t["path"].(string)
		if !ok {
			path = ".venv"
		}
		return path, true
	default:
		return "", false
	}
}
