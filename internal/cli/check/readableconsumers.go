package check

import (
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func readableCheckMachineValue(merged *jsonx.OrderedMap, key, legacyKey string) (any, bool) {
	if merged == nil {
		return nil, false
	}
	if machine := mapOrNil(merged, "machine"); machine != nil {
		if value, present := machine.Get(key); present {
			return value, true
		}
	}
	return merged.Get(legacyKey)
}

func readableCheckResources(merged *jsonx.OrderedMap) *jsonx.OrderedMap {
	value, present := readableCheckMachineValue(merged, "resources", "resources")
	if !present {
		return nil
	}
	resources, _ := value.(*jsonx.OrderedMap)
	return resources
}

func readableCheckMiseTools(merged *jsonx.OrderedMap) *jsonx.OrderedMap {
	value, present := configValue(merged, "install", "mise")
	if !present {
		if merged == nil {
			return config.MergeMiseTools(jsonx.NewOrderedMap())
		}
		value, present = merged.Get("mise_tools")
	}
	tools, _ := value.(*jsonx.OrderedMap)
	result := config.MergeMiseTools(jsonx.NewOrderedMap())
	if present && tools != nil {
		for _, key := range tools.Keys() {
			value, _ := tools.Get(key)
			result.Set(key, value)
		}
	}
	return result
}

func configValue(configMap *jsonx.OrderedMap, sectionName, key string) (any, bool) {
	if configMap == nil {
		return nil, false
	}
	sectionValue, _ := configMap.Get(sectionName)
	if sectionValue == nil {
		return nil, false
	}
	section, ok := sectionValue.(*jsonx.OrderedMap)
	if !ok {
		return nil, false
	}
	return section.Get(key)
}
