package config

import "github.com/mschulkind-oss/yolo-jail/internal/jsonx"

func mergeReadableConfig(base, override *jsonx.OrderedMap, baseSource, overrideSource *srcNode, path []string) (*jsonx.OrderedMap, *srcNode) {
	track := baseSource != nil || overrideSource != nil
	result := jsonx.NewOrderedMap()
	var node *srcNode
	if track {
		node = &srcNode{writers: concatWriters(baseSource.writerList(), overrideSource.writerList()),
			keys: make(map[string]*srcNode, base.Len()+override.Len())}
	}
	for _, key := range base.Keys() {
		value, _ := base.Get(key)
		result.Set(key, value)
		if track {
			node.keys[key] = baseSource.key(key)
		}
	}
	for _, key := range override.Keys() {
		value, _ := override.Get(key)
		existing, present := result.Get(key)
		if present {
			if baseMap, ok := asMap(existing); ok {
				if overrideMap, ok := asMap(value); ok {
					merged, mergedSource := mergeReadableConfig(baseMap, overrideMap, node.key(key), overrideSource.key(key), appendReadablePath(path, key))
					result.Set(key, merged)
					if track {
						node.keys[key] = mergedSource
					}
					continue
				}
			}
			if baseList, ok := asList(existing); ok {
				if overrideList, ok := asList(value); ok {
					if len(overrideList) == 0 && readableMergeRemovalPolicy(appendReadablePath(path, key)) == RemovalEmptyList {
						result.Set(key, []any{})
						if track {
							node.keys[key] = replacedBy(node.key(key), overrideSource.key(key))
						}
						continue
					}
					merged, mergedSource := mergeLists(baseList, overrideList, node.key(key), overrideSource.key(key))
					result.Set(key, merged)
					if track {
						node.keys[key] = mergedSource
					}
					continue
				}
			}
		}
		result.Set(key, value)
		if track {
			if present {
				node.keys[key] = replacedBy(node.key(key), overrideSource.key(key))
			} else {
				node.keys[key] = overrideSource.key(key)
			}
		}
	}
	return result, node
}

func readableMergeRemovalPolicy(path []string) RemovalPolicy {
	if rule, ok := readableSchemaRule(path); ok {
		return rule.Removal
	}
	return RemovalUnspecified
}
