// Package arraymerge shares the value semantics used by grouped-manifest array folds.
// It deliberately imports neither the config composer nor the pack loader.
package arraymerge

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// MergeAppending merges an object layer recursively and appends only values not already
// present. Nil deletes, matching merge-patch behavior. Inputs and nested values are copied.
func MergeAppending(base, patch map[string]any) map[string]any {
	out := cloneMap(base)
	for key, value := range patch {
		if value == nil {
			delete(out, key)
			continue
		}
		if patchMap, ok := value.(map[string]any); ok {
			if baseMap, ok := out[key].(map[string]any); ok {
				out[key] = MergeAppending(baseMap, patchMap)
			} else {
				out[key] = MergeAppending(map[string]any{}, patchMap)
			}
			continue
		}
		if patchArray, ok := value.([]any); ok {
			baseArray, _ := out[key].([]any)
			appended := append([]any{}, baseArray...)
			for _, entry := range patchArray {
				if !containsValue(appended, entry) {
					appended = append(appended, cloneValue(entry))
				}
			}
			out[key] = appended
			continue
		}
		out[key] = cloneValue(value)
	}
	return out
}

// EntryKey returns canonical identity across the config decoders' equivalent value types.
func EntryKey(value any) string {
	b, err := json.Marshal(normalize(value))
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(b)
}

func containsValue(values []any, candidate any) bool {
	key := EntryKey(candidate)
	for _, value := range values {
		if EntryKey(value) == key {
			return true
		}
	}
	return false
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		out := make([]any, len(value))
		for i, entry := range value {
			out[i] = cloneValue(entry)
		}
		return out
	default:
		return value
	}
}

func normalize(value any) any {
	switch typed := value.(type) {
	case *jsonx.OrderedMap:
		return normalize(jsonx.PlainMap(typed))
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, entry := range typed {
			out[key] = normalize(entry)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, entry := range typed {
			out[i] = normalize(entry)
		}
		return out
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32:
		return rv.Float()
	}
	return jsonx.Plain(value)
}
