package agentcfg

// mergeAppendingArrays is the grouped-manifest overlay fold. Objects still merge
// recursively; an array from a TOML overlay appends only entries not already present.
// Null retains merge-patch deletion semantics, and callers leave legacy/replace layers
// on deepMerge so their historical whole-array replacement is unchanged.
func mergeAppendingArrays(base, patch map[string]any) map[string]any {
	out := cloneOverlayMap(base)
	for key, value := range patch {
		if value == nil {
			delete(out, key)
			continue
		}
		if patchMap, ok := value.(map[string]any); ok {
			if baseMap, ok := out[key].(map[string]any); ok {
				out[key] = mergeAppendingArrays(baseMap, patchMap)
			} else {
				out[key] = mergeAppendingArrays(map[string]any{}, patchMap)
			}
			continue
		}
		if patchArray, ok := value.([]any); ok {
			baseArray, _ := out[key].([]any)
			appended := append([]any{}, baseArray...)
			for _, entry := range patchArray {
				if !containsOverlayValue(appended, entry) {
					appended = append(appended, cloneOverlayValue(entry))
				}
			}
			out[key] = appended
			continue
		}
		out[key] = cloneOverlayValue(value)
	}
	return out
}

func cloneOverlayMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneOverlayValue(value)
	}
	return out
}

func cloneOverlayValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneOverlayMap(value)
	case []any:
		out := make([]any, len(value))
		for i, entry := range value {
			out[i] = cloneOverlayValue(entry)
		}
		return out
	default:
		return value
	}
}

func containsOverlayValue(values []any, candidate any) bool {
	key := entryKey(candidate)
	for _, value := range values {
		if entryKey(value) == key {
			return true
		}
	}
	return false
}
