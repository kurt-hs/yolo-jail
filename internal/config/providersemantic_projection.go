package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// LegacyDeriveProjection is the compatibility shape current pack derives consume through
// ctx.providers: provider model ids plus flat per-model facts. Unsupported names are explicit;
// callers must not silently claim a receiver consumed those canonical facts.
type LegacyDeriveProjection struct {
	ProviderEntry *jsonx.OrderedMap
	HasModelRows  bool
	Unsupported   map[string][]string
}

// RequireSupportedFor turns the structured census into an actionable refusal for callers that
// cannot accept a partial projection.
func (projection LegacyDeriveProjection) RequireSupportedFor(receiver string) error {
	ids := make([]string, 0, len(projection.Unsupported))
	for id := range projection.Unsupported {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	var details []string
	for _, id := range ids {
		details = append(details, fmt.Sprintf("%s: %s", id, strings.Join(projection.Unsupported[id], ", ")))
	}
	return fmt.Errorf("selected receiver %s cannot consume canonical model facts in the legacy derive projection (%s); omit those facts or add a receiver-specific mapping before selection", receiver, strings.Join(details, "; "))
}

// ProjectProviderModelsForLegacyDerives lowers only the canonical facts understood by the
// existing shared derive vocabulary. Facts not represented there are returned by model id so a
// receiver can refuse them actionably; this does not activate or invoke a derive.
func ProjectProviderModelsForLegacyDerives(models ReadableModels) (LegacyDeriveProjection, error) {
	entry := jsonx.NewOrderedMap()
	if !models.Declared {
		return LegacyDeriveProjection{ProviderEntry: entry, Unsupported: map[string][]string{}}, nil
	}
	ids, options, routing := jsonx.NewOrderedMap(), jsonx.NewOrderedMap(), jsonx.NewOrderedMap()
	unsupported := map[string][]string{}
	order := append([]string(nil), models.Order...)
	if len(order) == 0 {
		for id := range models.ByID {
			order = append(order, id)
		}
		sort.Strings(order)
	}
	for _, id := range order {
		model, ok := models.ByID[id]
		if !ok {
			return LegacyDeriveProjection{}, fmt.Errorf("model order names missing row %q", id)
		}
		if model.ID != "" && model.ID != id {
			return LegacyDeriveProjection{}, fmt.Errorf("model key %q disagrees with row id %q", id, model.ID)
		}
		ids.Set(id, id)
		facts := model.Facts
		flat := jsonx.NewOrderedMap()
		setText := func(key, value string) {
			if value != "" {
				flat.Set(key, value)
			}
		}
		setText("name", facts.Name)
		setText("description", facts.Description)
		setText("vendor", facts.Vendor)
		if facts.Context != 0 {
			flat.Set("context_window", strconv.FormatInt(facts.Context, 10))
		}
		if facts.MaxTokens != 0 {
			flat.Set("max_tokens", strconv.FormatInt(facts.MaxTokens, 10))
		}
		if facts.Reasoning != nil {
			flat.Set("reasoning", strconv.FormatBool(*facts.Reasoning))
		}
		if facts.Accepts != nil && len(facts.Accepts) > 0 {
			flat.Set("input", strings.Join(facts.Accepts, ","))
		}
		if facts.Price != nil {
			for _, rate := range []struct {
				name  string
				value *float64
			}{{"input", facts.Price.Input}, {"output", facts.Price.Output}, {"cache_read", facts.Price.CacheRead}, {"cache_write", facts.Price.CacheWrite}} {
				if rate.value != nil {
					flat.Set("cost_"+rate.name, strconv.FormatFloat(*rate.value, 'f', -1, 64))
				}
			}
		}
		setText("base", facts.Base)
		wireID := model.WireID
		if facts.WireID != "" {
			wireID = facts.WireID
		}
		setText("wire_id", wireID)
		setText("extends", facts.Extends)
		if flat.Len() > 0 {
			options.Set(id, flat)
		}
		if facts.OpenRouterRouting != nil {
			routing.Set(id, orderedJSONValue(facts.OpenRouterRouting))
		}
		fields := []string{}
		if facts.Accepts != nil && len(facts.Accepts) == 0 {
			fields = append(fields, "accepts")
		}
		if facts.API != "" {
			fields = append(fields, "api")
		}
		if facts.Thinking != nil {
			fields = append(fields, "thinking")
		}
		if facts.Images != nil {
			fields = append(fields, "images")
		}
		if model.CompactAt != 0 {
			fields = append(fields, "compact_at")
		}
		if len(fields) > 0 {
			sort.Strings(fields)
			unsupported[id] = fields
		}
	}
	entry.Set("models", ids)
	if options.Len() > 0 {
		entry.Set("model_options", options)
	}
	if routing.Len() > 0 {
		entry.Set("model_routing", routing)
	}
	return LegacyDeriveProjection{ProviderEntry: entry, HasModelRows: true, Unsupported: unsupported}, nil
}

func orderedJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := jsonx.NewOrderedMap()
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out.Set(key, orderedJSONValue(typed[key]))
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = orderedJSONValue(child)
		}
		return out
	default:
		return value
	}
}
