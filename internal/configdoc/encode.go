package configdoc

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// Encode serializes an ordered object. OrderedMap preserves authored order,
// including maps nested in arrays; native Go maps use lexical key order as a
// deterministic fallback because those values carry no key ordering.
func Encode(values *jsonx.OrderedMap, format Format) ([]byte, error) {
	if values == nil {
		return nil, fmt.Errorf("configdoc: cannot encode a nil object")
	}
	switch format {
	case JSONC:
		s, err := jsonx.DumpsIndent(values, 2)
		if err != nil {
			return nil, err
		}
		return []byte(s + "\n"), nil
	case TOML:
		var out strings.Builder
		if err := writeTOMLTable(&out, values, nil, true); err != nil {
			return nil, err
		}
		return []byte(out.String()), nil
	default:
		return nil, fmt.Errorf("configdoc: unsupported format %d", format)
	}
}

// EncodeValue serializes one scalar or container value without a key.
func EncodeValue(value any, format Format) ([]byte, error) {
	switch format {
	case JSONC:
		s, err := jsonx.DumpsIndent(value, 2)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case TOML:
		s, err := tomlValue(value)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	default:
		return nil, fmt.Errorf("configdoc: unsupported format %d", format)
	}
}

func writeTOMLTable(out *strings.Builder, table *jsonx.OrderedMap, path []string, root bool) error {
	if !root {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		heading, err := tomlKeyPath(path)
		if err != nil {
			return err
		}
		out.WriteByte('[')
		out.WriteString(heading)
		out.WriteString("]\n")
	}
	keys := table.Keys()
	for _, key := range keys {
		value, _ := table.Get(key)
		if asOrderedMap(value) != nil {
			continue
		}
		keyText, err := tomlKey(key)
		if err != nil {
			return err
		}
		encoded, err := tomlValue(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		out.WriteString(keyText)
		out.WriteString(" = ")
		out.WriteString(encoded)
		out.WriteByte('\n')
	}
	for _, key := range keys {
		value, _ := table.Get(key)
		child := asOrderedMap(value)
		if child == nil {
			continue
		}
		if err := writeTOMLTable(out, child, append(append([]string(nil), path...), key), false); err != nil {
			return err
		}
	}
	return nil
}

func tomlValue(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", fmt.Errorf("TOML has no null value")
	case string:
		return tomlString(v)
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return tomlUnsigned(uint64(v))
	case uint8:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return tomlUnsigned(v)
	case float32:
		return tomlFloat(float64(v))
	case float64:
		return tomlFloat(v)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			s, err := tomlValue(sortedNestedMapKeys(item))
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case *jsonx.OrderedMap:
		parts := make([]string, 0, v.Len())
		for _, key := range v.Keys() {
			item, _ := v.Get(key)
			keyText, err := tomlKey(key)
			if err != nil {
				return "", err
			}
			s, err := tomlValue(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, keyText+" = "+s)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			keyText, err := tomlKey(key)
			if err != nil {
				return "", err
			}
			s, err := tomlValue(v[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, keyText+" = "+s)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		rv := reflect.ValueOf(value)
		if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
			parts := make([]string, rv.Len())
			for i := range parts {
				s, err := tomlValue(rv.Index(i).Interface())
				if err != nil {
					return "", err
				}
				parts[i] = s
			}
			return "[" + strings.Join(parts, ", ") + "]", nil
		}
		return "", fmt.Errorf("unsupported TOML value %T", value)
	}
}

func sortedNestedMapKeys(value any) any {
	switch v := value.(type) {
	case *jsonx.OrderedMap:
		out := jsonx.NewOrderedMap()
		for _, key := range v.Keys() {
			item, _ := v.Get(key)
			out.Set(key, sortedNestedMapKeys(item))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = sortedNestedMapKeys(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sortedNestedMapKeys(item)
		}
		return out
	default:
		return value
	}
}

func tomlUnsigned(value uint64) (string, error) {
	if value > math.MaxInt64 {
		return "", fmt.Errorf("TOML integers cannot exceed %d", int64(math.MaxInt64))
	}
	return strconv.FormatUint(value, 10), nil
}
func tomlFloat(value float64) (string, error) {
	if math.IsNaN(value) {
		return "nan", nil
	}
	if math.IsInf(value, 1) {
		return "+inf", nil
	}
	if math.IsInf(value, -1) {
		return "-inf", nil
	}
	s := strconv.FormatFloat(value, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s, nil
}

func tomlString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("TOML strings must be valid UTF-8")
	}
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			out.WriteString("\\\\")
		case '"':
			out.WriteString("\\\"")
		case '\b':
			out.WriteString("\\b")
		case '\t':
			out.WriteString("\\t")
		case '\n':
			out.WriteString("\\n")
		case '\f':
			out.WriteString("\\f")
		case '\r':
			out.WriteString("\\r")
		default:
			if r < 0x20 || r >= 0x7f && r <= 0x9f {
				if r <= 0xffff {
					fmt.Fprintf(&out, "\\u%04X", r)
				} else {
					fmt.Fprintf(&out, "\\U%08X", r)
				}
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return out.String(), nil
}

func tomlKey(key string) (string, error) {
	if key != "" {
		valid := true
		for _, r := range key {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				valid = false
				break
			}
		}
		if valid {
			return key, nil
		}
	}
	return tomlString(key)
}
func tomlKeyPath(path []string) (string, error) {
	parts := make([]string, len(path))
	for i, key := range path {
		encoded, err := tomlKey(key)
		if err != nil {
			return "", err
		}
		parts[i] = encoded
	}
	return strings.Join(parts, "."), nil
}
func asOrderedMap(value any) *jsonx.OrderedMap { m, _ := value.(*jsonx.OrderedMap); return m }
