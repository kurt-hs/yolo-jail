package packdecl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// ManifestFormat records which manifest reader produced a normalized Manifest.
type ManifestFormat uint8

const (
	ManifestFormatJSON ManifestFormat = iota + 1
	ManifestFormatTOML
)

// ManifestOrigin is carried with the normalized manifest so consumers can keep
// legacy and grouped-TOML semantics distinct during the reader transition.
type ManifestOrigin struct {
	Format ManifestFormat
	File   string
}

// ManifestLocation identifies a grouped TOML declaration in the source file.
type ManifestLocation struct {
	File         string
	Key          string
	Line, Column int
}

// ProviderVariant is one named provider configuration in grouped TOML.
type ProviderVariant struct {
	Model         string                   `json:"model,omitempty"`
	Via           string                   `json:"via,omitempty"`
	Options       map[string]OptionDefault `json:"options,omitempty"`
	Subagents     map[string]any           `json:"subagents,omitempty"`
	EnforceModels *bool                    `json:"enforce_models,omitempty"`
	Capabilities  []string                 `json:"capabilities,omitempty"`
	Protocols     []string                 `json:"protocols,omitempty"`
}

// ProviderModel contains one model row's shared, provider-independent facts.
type ProviderModel struct {
	Name              string         `json:"name,omitempty"`
	Description       string         `json:"description,omitempty"`
	Vendor            string         `json:"vendor,omitempty"`
	API               string         `json:"api,omitempty"`
	Thinking          []string       `json:"thinking,omitempty"`
	Images            *ProviderImage `json:"images,omitempty"`
	Price             *ProviderPrice `json:"price,omitempty"`
	Context           int64          `json:"context,omitempty"`
	MaxTokens         int64          `json:"max_tokens,omitempty"`
	Reasoning         *bool          `json:"reasoning,omitempty"`
	Accepts           []string       `json:"accepts,omitempty"`
	Base              string         `json:"base,omitempty"`
	WireID            string         `json:"wire_id,omitempty"`
	Extends           string         `json:"extends,omitempty"`
	OpenRouterRouting map[string]any `json:"openrouter_routing,omitempty"`
	TwinOf            string         `json:"twin_of,omitempty"`
	CompactAt         int64          `json:"compact_at,omitempty"`
}

// ProviderImage bounds image input for a model.
type ProviderImage struct {
	MaxWidth    int64 `json:"max_width,omitempty"`
	MaxHeight   int64 `json:"max_height,omitempty"`
	MaxBytes    int64 `json:"max_bytes,omitempty"`
	JPEGQuality int64 `json:"jpeg_quality,omitempty"`
}

// ProviderPrice contains per-million-token rates for a model.
type ProviderPrice struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type manifestReadMode uint8

const (
	manifestStrict manifestReadMode = iota
	manifestUse
	manifestTolerant
)

type groupedContribution struct {
	kind     Kind
	group    string
	index    int
	start    int
	value    map[string]any
	replace  bool
	location ManifestLocation
	provider *providerShape
}

type providerShape struct {
	alias        string
	model        string
	variants     map[string]ProviderVariant
	variantOrder []string
	everyModel   *ProviderModel
	models       map[string]ProviderModel
	modelOrder   []string
}

// DecodeTOML reads a strict authoring manifest from grouped TOML.
func DecodeTOML(data []byte) (*Manifest, []string) {
	m, problems, _ := decodeTOML(data, manifestStrict)
	return m, problems
}

// DecodeTOMLForUse reads a grouped TOML manifest with the host-use skew policy.
func DecodeTOMLForUse(data []byte) (*Manifest, []string, []string) {
	return decodeTOML(data, manifestUse)
}

// DecodeTOMLTolerant reads a grouped TOML manifest across the jail version boundary.
func DecodeTOMLTolerant(data []byte) (*Manifest, []string, []string) {
	return decodeTOML(data, manifestTolerant)
}

func decodeTOML(data []byte, mode manifestReadMode) (*Manifest, []string, []string) {
	doc, err := configdoc.Parse(data, configdoc.TOML)
	if err != nil {
		return nil, []string{ManifestTOMLName + ": " + err.Error()}, nil
	}
	root, ok := jsonx.Plain(doc.Values).(map[string]any)
	if !ok {
		return nil, []string{ManifestTOMLName + ": expected a TOML table at the root"}, nil
	}
	normalized, groups, problems, preSkipped := normalizeGrouped(root, doc, mode)
	if len(problems) > 0 {
		return nil, problems, preSkipped
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, []string{ManifestTOMLName + ": " + err.Error()}, nil
	}
	var m *Manifest
	var skipped []string
	switch mode {
	case manifestStrict:
		var strict []string
		m, strict = Decode(encoded)
		problems = strict
	case manifestUse:
		m, problems, skipped = DecodeForUse(encoded)
	case manifestTolerant:
		m, problems, skipped = DecodeTolerant(encoded)
	}
	if m == nil {
		return nil, locateTOMLProblems(problems, groups, root, doc), append(preSkipped, locateTOMLProblems(skipped, groups, root, doc)...)
	}
	m.Origin = ManifestOrigin{Format: ManifestFormatTOML, File: ManifestTOMLName}
	usedGroups := make(map[int]bool, len(groups))
	for i := range m.Contributes {
		entry, groupIndex, ok := matchGroupedContribution(m.Contributes[i], groups, usedGroups)
		if !ok {
			continue
		}
		usedGroups[groupIndex] = true
		m.Contributes[i].Origin = m.Origin
		m.Contributes[i].Location = entry.location
		m.Contributes[i].Replace = entry.replace
		if entry.provider != nil {
			m.Contributes[i].ProviderAlias = entry.provider.alias
			m.Contributes[i].ProviderModel = entry.provider.model
			m.Contributes[i].ProviderVariants = entry.provider.variants
			m.Contributes[i].ProviderVariantOrder = append([]string(nil), entry.provider.variantOrder...)
			m.Contributes[i].EveryProviderModel = entry.provider.everyModel
			m.Contributes[i].ProviderModels = entry.provider.models
			m.Contributes[i].ProviderModelOrder = append([]string(nil), entry.provider.modelOrder...)
		}
	}
	return m, locateTOMLProblems(problems, groups, root, doc), append(preSkipped, locateTOMLProblems(skipped, groups, root, doc)...)
}

func normalizeGrouped(root map[string]any, doc *configdoc.Document, mode manifestReadMode) (map[string]any, []groupedContribution, []string, []string) {
	out := make(map[string]any, len(root)+1)
	var groups []groupedContribution
	var problems []string
	var skipped []string
	for _, key := range sortedKeys(root) {
		value := root[key]
		switch {
		case key == "contributes":
			loc := locationFor(doc, []configdoc.Step{{Key: key}}, key)
			problems = append(problems, formatManifestLocation(loc, "\"contributes\" is the legacy pack.json shape; group entries by kind as [[program]], [[overlay]], and similar tables"))
		case key == "providers":
			providerTable, ok := value.(map[string]any)
			if !ok {
				loc := locationFor(doc, []configdoc.Step{{Key: key}}, key)
				problems = append(problems, formatManifestLocation(loc, "providers must be a table keyed by provider name"))
				continue
			}
			for _, name := range orderedTableKeys(doc, []configdoc.Step{{Key: "providers"}}, providerTable) {
				raw := providerTable[name]
				fields, ok := raw.(map[string]any)
				if !ok {
					loc := locationFor(doc, []configdoc.Step{{Key: "providers"}, {Key: name}}, "providers."+name)
					problems = append(problems, formatManifestLocation(loc, "provider declaration must be a table"))
					continue
				}
				loc := locationFor(doc, []configdoc.Step{{Key: "providers"}, {Key: name}}, "providers."+name)
				copyFields := cloneMap(fields)
				replace, err := takeReplace(copyFields)
				if err != nil {
					problems = append(problems, formatManifestLocation(loc, "replace: "+err.Error()))
					continue
				}
				if renamed := providerRenamedField(copyFields); renamed != nil {
					fieldLoc := providerUnknownLocation(doc, name, copyFields, renamed.Path)
					problems = append(problems, formatManifestLocation(fieldLoc, renameMessage(*renamed.Rename)))
					continue
				}
				if unknown := providerShapeUnknownField(copyFields); unknown != "" {
					problem := "unknown provider model field " + strconv.Quote(unknown)
					if mode == manifestStrict {
						problems = append(problems, formatManifestLocation(providerUnknownLocation(doc, name, copyFields, unknown), problem))
					} else {
						skipped = append(skipped, formatManifestLocation(loc, "skipping provider declaration with "+problem+"; update yolo to read it"))
					}
					continue
				}
				value, shape, err := normalizeProvider(name, copyFields, doc)
				if err != nil {
					problems = append(problems, formatManifestLocation(loc, err.Error()))
					continue
				}
				groups = append(groups, groupedContribution{kind: KindProvider, group: "providers." + name, index: 0, start: locOffset(doc, loc), value: value, replace: replace, location: loc, provider: shape})
			}
		case isGroupedKind(key, value):
			kind := groupKind(key)
			if kind == "" {
				kind = Kind(strings.ReplaceAll(key, "_", "-"))
			}
			items, ok := value.([]any)
			if !ok {
				loc := locationFor(doc, []configdoc.Step{{Key: key}}, key)
				problems = append(problems, formatManifestLocation(loc, fmt.Sprintf("[[%s]] must be an array of tables", key)))
				continue
			}
			for i, item := range items {
				fields, ok := item.(map[string]any)
				if !ok {
					loc := locationFor(doc, []configdoc.Step{{Key: key}, {Index: i, IsIndex: true}}, fmt.Sprintf("%s[%d]", key, i))
					problems = append(problems, formatManifestLocation(loc, "contribution entry must be a table"))
					continue
				}
				loc := locationFor(doc, []configdoc.Step{{Key: key}, {Index: i, IsIndex: true}}, fmt.Sprintf("%s[%d]", key, i))
				copyFields := cloneMap(fields)
				if rename, old := groupedRename(kind, copyFields); rename != nil {
					problems = append(problems, formatManifestLocation(loc, renameMessage(*rename)+" (field "+strconv.Quote(old)+")"))
					continue
				}
				if kind == KindConfigOverlay {
					if set, ok := copyFields["set"]; ok {
						copyFields["config"] = map[string]any{"managed": set}
						delete(copyFields, "set")
					}
				}
				replace, err := takeReplace(copyFields)
				if err != nil {
					problems = append(problems, formatManifestLocation(loc, "replace: "+err.Error()))
					continue
				}
				copyFields["kind"] = string(kind)
				groups = append(groups, groupedContribution{kind: kind, group: key, index: i, start: locOffset(doc, loc), value: copyFields, replace: replace, location: loc})
			}
		default:
			out[key] = value
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].start < groups[j].start })
	contributes := make([]any, 0, len(groups))
	for _, entry := range groups {
		contributes = append(contributes, entry.value)
	}
	if len(contributes) > 0 {
		out["contributes"] = contributes
	}
	return out, groups, problems, skipped
}

func isGroupedKind(group string, value any) bool {
	if groupKind(group) != "" {
		return true
	}
	switch group {
	case "name", "description", "skills_tier", "supersedes", "needs", "model_list_check", "provider_keys", "provider_env", "launch_computed", "session_homes":
		return false
	}
	_, isArray := value.([]any)
	return isArray
}

func groupKind(group string) Kind {
	if group == "overlay" {
		return KindConfigOverlay
	}
	candidate := Kind(strings.ReplaceAll(group, "_", "-"))
	if KnownKind(candidate) {
		return candidate
	}
	return ""
}

func groupedRename(kind Kind, fields map[string]any) (*fieldRename, string) {
	scope := ""
	if kind == KindConfigOverlay {
		scope = "overlay"
	}
	for _, key := range sortedKeys(fields) {
		if rename, ok := renamedFieldInScope(scope, key); ok {
			return &rename, key
		}
	}
	return nil, ""
}

func providerRenamedField(fields map[string]any) *scopedRename {
	if raw, ok := fields["every_model"]; ok {
		if path := modelRename(raw, "every_model"); path != nil {
			return path
		}
	}
	if raw, ok := fields["models"].(map[string]any); ok {
		for _, id := range sortedKeys(raw) {
			if path := modelRename(raw[id], "models."+id); path != nil {
				return path
			}
		}
	}
	return nil
}

func modelRename(raw any, prefix string) *scopedRename {
	model, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range sortedKeys(model) {
		if rename, ok := renamedFieldInScope("provider-model", key); ok {
			return &scopedRename{Path: prefix + "." + key, Rename: &rename}
		}
	}
	return nil
}

func orderedTableKeys(doc *configdoc.Document, path []configdoc.Step, table map[string]any) []string {
	keys := sortedKeys(table)
	positions := make(map[string]int, len(keys))
	for _, key := range keys {
		memberPath := append(append([]configdoc.Step(nil), path...), configdoc.Step{Key: key})
		span, ok, err := doc.Index.Value(memberPath...)
		if err != nil || !ok {
			positions[key] = len(doc.Bytes) + len(keys)
			continue
		}
		positions[key] = span.Start
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return positions[keys[i]] < positions[keys[j]]
	})
	return keys
}

func providerShapeUnknownField(fields map[string]any) string {
	if raw, ok := fields["every_model"]; ok {
		if path := unknownModelField(raw, "every_model"); path != "" {
			return path
		}
	}
	if raw, ok := fields["models"]; ok {
		if rows, ok := raw.(map[string]any); ok {
			for _, id := range sortedKeys(rows) {
				if path := unknownModelField(rows[id], "models."+id); path != "" {
					return path
				}
			}
		}
	}
	if raw, ok := fields["variants"]; ok {
		if variants, ok := raw.(map[string]any); ok {
			for _, name := range sortedKeys(variants) {
				if variant, ok := variants[name].(map[string]any); ok {
					if key := firstUnknownKey(variant, "model", "via", "options", "subagents", "enforce_models", "capabilities", "protocols"); key != "" {
						return "variants." + name + "." + key
					}
				}
			}
		}
	}
	return ""
}

func providerUnknownLocation(doc *configdoc.Document, provider string, fields map[string]any, unknown string) ManifestLocation {
	path := []configdoc.Step{{Key: "providers"}, {Key: provider}}
	key := "providers." + provider + "." + unknown
	if strings.HasPrefix(unknown, "every_model.") {
		path = append(path, configdoc.Step{Key: "every_model"})
	} else if strings.HasPrefix(unknown, "models.") {
		if rows, ok := fields["models"].(map[string]any); ok {
			for _, id := range sortedKeys(rows) {
				if strings.HasPrefix(unknown, "models."+id+".") {
					path = append(path, configdoc.Step{Key: "models"}, configdoc.Step{Key: id})
					break
				}
			}
		}
	} else if strings.HasPrefix(unknown, "variants.") {
		if variants, ok := fields["variants"].(map[string]any); ok {
			for _, name := range sortedKeys(variants) {
				if strings.HasPrefix(unknown, "variants."+name+".") {
					path = append(path, configdoc.Step{Key: "variants"}, configdoc.Step{Key: name})
					break
				}
			}
		}
	}
	return locationFor(doc, path, key)
}

func unknownModelField(raw any, path string) string {
	model, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	if key := firstUnknownKey(model, "name", "description", "vendor", "api", "thinking", "images", "price", "context", "max_tokens", "reasoning", "accepts", "base", "wire_id", "extends", "openrouter_routing", "twin_of", "compact_at"); key != "" {
		return path + "." + key
	}
	if images, ok := model["images"].(map[string]any); ok {
		if key := firstUnknownKey(images, "max_width", "max_height", "max_bytes", "jpeg_quality"); key != "" {
			return path + ".images." + key
		}
	}
	if price, ok := model["price"].(map[string]any); ok {
		if key := firstUnknownKey(price, "input", "output", "cache_read", "cache_write"); key != "" {
			return path + ".price." + key
		}
	}
	return ""
}

func firstUnknownKey(fields map[string]any, allowed ...string) string {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	for _, key := range sortedKeys(fields) {
		if !known[key] {
			return key
		}
	}
	return ""
}

func normalizeProvider(name string, fields map[string]any, doc *configdoc.Document) (map[string]any, *providerShape, error) {
	providerName := name
	shape := &providerShape{}
	if raw, ok := fields["alias"]; ok {
		value, ok := raw.(string)
		if !ok || value == "" || strings.ContainsAny(value, " \t\r\n") {
			return nil, nil, fmt.Errorf("alias must be a non-empty name without whitespace")
		}
		shape.alias = value
		delete(fields, "alias")
	}
	if raw, ok := fields["model"]; ok {
		value, ok := raw.(string)
		if !ok || value == "" || strings.ContainsAny(value, " \t\r\n") {
			return nil, nil, fmt.Errorf("model must be a non-empty provider model name")
		}
		shape.model = value
		delete(fields, "model")
	}
	if raw, ok := fields["every_model"]; ok {
		model, err := decodeProviderModel(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("every_model: %w", err)
		}
		if err := validateProviderModel("every_model", raw, model); err != nil {
			return nil, nil, err
		}
		shape.everyModel = &model
		delete(fields, "every_model")
	}
	if raw, ok := fields["models"]; ok {
		rows, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("models must be a table keyed by model id")
		}
		shape.models = make(map[string]ProviderModel, len(rows))
		for _, id := range orderedTableKeys(doc, []configdoc.Step{{Key: "providers"}, {Key: name}, {Key: "models"}}, rows) {
			value := rows[id]
			if id == "" {
				return nil, nil, fmt.Errorf("models contains an empty model id")
			}
			model, err := decodeProviderModel(value)
			if err != nil {
				return nil, nil, fmt.Errorf("models.%s: %w", id, err)
			}
			if err := validateProviderModel("models."+id, value, model); err != nil {
				return nil, nil, err
			}
			shape.models[id] = model
			shape.modelOrder = append(shape.modelOrder, id)
		}
		delete(fields, "models")
	}
	if raw, ok := fields["variants"]; ok {
		variants, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("variants must be a table keyed by variant name")
		}
		shape.variants = make(map[string]ProviderVariant, len(variants))
		for _, name := range orderedTableKeys(doc, []configdoc.Step{{Key: "providers"}, {Key: providerName}, {Key: "variants"}}, variants) {
			value := variants[name]
			if name == "" {
				return nil, nil, fmt.Errorf("variants contains an empty name")
			}
			var variant ProviderVariant
			if err := decodeStrictValue(value, &variant); err != nil {
				return nil, nil, fmt.Errorf("variants.%s: %w", name, err)
			}
			if variant.Model == "" && variant.Via == "" {
				return nil, nil, fmt.Errorf("variants.%s must set model or via", name)
			}
			if variant.Model != "" && strings.ContainsAny(variant.Model, " \t\r\n") {
				return nil, nil, fmt.Errorf("variants.%s.model must be a name", name)
			}
			if variant.Via == "" && hasKey(value, "via") {
				return nil, nil, fmt.Errorf("variants.%s.via must be non-empty", name)
			}
			if err := uniqueNonempty("variants."+name+".capabilities", variant.Capabilities); err != nil {
				return nil, nil, err
			}
			if err := uniqueNonempty("variants."+name+".protocols", variant.Protocols); err != nil {
				return nil, nil, err
			}
			shape.variants[name] = variant
			shape.variantOrder = append(shape.variantOrder, name)
		}
		delete(fields, "variants")
	}
	fields["kind"] = string(KindProvider)
	fields["name"] = name
	return fields, shape, nil
}

func decodeProviderModel(raw any) (ProviderModel, error) {
	var model ProviderModel
	err := decodeStrictValue(raw, &model)
	return model, err
}

func decodeStrictValue(raw any, dst any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func validateProviderModel(label string, raw any, model ProviderModel) error {
	facts, _ := raw.(map[string]any)
	if value, ok := facts["api"]; ok {
		api, valid := value.(string)
		if !valid || api == "" {
			return fmt.Errorf("%s.api must be a non-empty string", label)
		}
	}
	if value, ok := facts["context"]; ok && !positiveInteger(value) {
		return fmt.Errorf("%s.context must be a positive integer", label)
	}
	if value, ok := facts["max_tokens"]; ok && !positiveInteger(value) {
		return fmt.Errorf("%s.max_tokens must be a positive integer", label)
	}
	if value, ok := facts["compact_at"]; ok && !positiveInteger(value) {
		return fmt.Errorf("%s.compact_at must be a positive integer", label)
	}
	if model.TwinOf != "" && label == "models."+model.TwinOf {
		return fmt.Errorf("%s.twin_of cannot name itself", label)
	}
	if value, ok := facts["vendor"]; ok {
		vendor, valid := value.(string)
		if !valid || !ValidModelVendor(vendor) {
			return fmt.Errorf("%s.vendor must be a non-empty lowercase vendor token", label)
		}
	}
	if value, ok := facts["base"]; ok {
		if text, valid := value.(string); !valid || text == "" {
			return fmt.Errorf("%s.base must be a non-empty model id string", label)
		}
	}
	if value, ok := facts["wire_id"]; ok {
		if text, valid := value.(string); !valid || text == "" {
			return fmt.Errorf("%s.wire_id must be a non-empty model id string", label)
		}
	}
	if value, ok := facts["extends"]; ok {
		if text, valid := value.(string); !valid || text == "" {
			return fmt.Errorf("%s.extends must be a non-empty shared model name", label)
		}
	}
	if value, ok := facts["openrouter_routing"]; ok && value != nil {
		if _, valid := value.(map[string]any); !valid {
			return fmt.Errorf("%s.openrouter_routing must be a table", label)
		}
	}
	if err := uniqueNonempty(label+".thinking", model.Thinking); err != nil {
		return err
	}
	if err := uniqueNonempty(label+".accepts", model.Accepts); err != nil {
		return err
	}
	for _, value := range model.Accepts {
		if value != "text" && value != "image" {
			return fmt.Errorf("%s.accepts has unsupported value %q (want text or image)", label, value)
		}
	}
	if model.Images != nil {
		images, _ := facts["images"].(map[string]any)
		for _, key := range []string{"max_width", "max_height", "max_bytes"} {
			if value, ok := images[key]; !ok || !positiveInteger(value) {
				return fmt.Errorf("%s.images.%s must be a positive integer", label, key)
			}
		}
		if value, ok := images["jpeg_quality"]; !ok || !positiveInteger(value) || model.Images.JPEGQuality > 100 {
			return fmt.Errorf("%s.images.jpeg_quality must be between 1 and 100", label)
		}
	}
	if model.Price != nil {
		for name, value := range map[string]*float64{
			"input": model.Price.Input, "output": model.Price.Output,
			"cache_read": model.Price.CacheRead, "cache_write": model.Price.CacheWrite,
		} {
			if value != nil && *value < 0 {
				return fmt.Errorf("%s.price.%s must be non-negative", label, name)
			}
		}
	}
	return nil
}

func hasKey(value any, key string) bool {
	fields, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, exists := fields[key]
	return exists
}

func positiveInteger(value any) bool {
	number, ok := value.(int64)
	if ok {
		return number > 0
	}
	float, ok := value.(float64)
	return ok && float >= 1 && float == float64(int64(float))
}

func (m *Manifest) setLegacyOrigin() {
	m.Origin = ManifestOrigin{Format: ManifestFormatJSON, File: ManifestName}
	for i := range m.Contributes {
		m.Contributes[i].Origin = m.Origin
		m.Contributes[i].Location = ManifestLocation{File: ManifestName}
	}
}

func uniqueNonempty(label string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			return fmt.Errorf("%s must contain unique non-empty strings", label)
		}
		seen[value] = true
	}
	return nil
}

func takeReplace(fields map[string]any) (bool, error) {
	raw, ok := fields["replace"]
	if !ok {
		return false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("must be a boolean")
	}
	delete(fields, "replace")
	return value, nil
}

func locationFor(doc *configdoc.Document, path []configdoc.Step, key string) ManifestLocation {
	span, ok, err := doc.Index.Item(path...)
	if err != nil || !ok {
		span, ok, err = doc.Index.Table(path...)
	}
	if err != nil || !ok {
		span, ok, _ = doc.Index.Member(path...)
	}
	if !ok {
		return ManifestLocation{File: ManifestTOMLName, Key: key}
	}
	line, column := doc.Index.Position(span.Start)
	return ManifestLocation{File: ManifestTOMLName, Key: key, Line: line, Column: column}
}

func locOffset(doc *configdoc.Document, loc ManifestLocation) int {
	if loc.Line < 1 {
		return len(doc.Bytes)
	}
	offset, line := 0, 1
	for offset < len(doc.Bytes) && line < loc.Line {
		if doc.Bytes[offset] == '\n' {
			line++
		}
		offset++
	}
	return offset
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func matchGroupedContribution(c Contribution, groups []groupedContribution, used map[int]bool) (groupedContribution, int, bool) {
	for i, group := range groups {
		if used[i] || group.kind != c.Kind {
			continue
		}
		if stringValue(group.value["name"]) != c.Name || stringValue(group.value["bin"]) != c.Bin || stringValue(group.value["surface"]) != c.Surface || stringValue(group.value["from"]) != c.From || stringValue(group.value["path"]) != c.Path || stringValue(group.value["hook"]) != c.Hook || stringValue(group.value["provider"]) != c.Provider {
			continue
		}
		return group, i, true
	}
	return groupedContribution{}, -1, false
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func formatManifestLocation(location ManifestLocation, problem string) string {
	if location.Line > 0 {
		return fmt.Sprintf("%s:%d:%d (%s): %s", location.File, location.Line, location.Column, location.Key, problem)
	}
	return fmt.Sprintf("%s (%s): %s", location.File, location.Key, problem)
}

func locateTOMLProblems(messages []string, groups []groupedContribution, root map[string]any, doc *configdoc.Document) []string {
	out := append([]string(nil), messages...)
	for i, message := range out {
		message = strings.ReplaceAll(message, ManifestName+":", ManifestTOMLName+":")
		if index := parseIndex(message); index >= 0 && index < len(groups) {
			group := groups[index]
			if group.location.Line > 0 {
				message = fmt.Sprintf("%s:%d:%d (%s): %s", ManifestTOMLName, group.location.Line, group.location.Column, group.location.Key, strings.TrimSpace(strings.TrimPrefix(message, "contributes["+strconv.Itoa(index)+"]:")))
			}
		}
		if strings.HasPrefix(message, ManifestTOMLName+":") && strings.Contains(message, "unknown field ") {
			if group, ok := groupForUnknownField(message, groups); ok {
				message = fmt.Sprintf("%s:%d:%d (%s): %s", ManifestTOMLName, group.location.Line, group.location.Column, group.location.Key, strings.TrimSpace(strings.TrimPrefix(message, ManifestTOMLName+":")))
			} else {
				for key := range root {
					if strings.Contains(message, fmt.Sprintf("unknown field %q", key)) {
						loc := locationFor(doc, []configdoc.Step{{Key: key}}, key)
						message = fmt.Sprintf("%s:%d:%d (%s): %s", ManifestTOMLName, loc.Line, loc.Column, key, strings.TrimSpace(strings.TrimPrefix(message, ManifestTOMLName+":")))
						break
					}
				}
			}
		}
		if !strings.HasPrefix(message, ManifestTOMLName+":") && !strings.HasPrefix(message, ManifestTOMLName+" ") {
			message = ManifestTOMLName + ": " + message
		}
		out[i] = message
	}
	return out
}

func groupForUnknownField(message string, groups []groupedContribution) (groupedContribution, bool) {
	start := strings.Index(message, `unknown field "`)
	if start < 0 {
		return groupedContribution{}, false
	}
	start += len(`unknown field "`)
	end := strings.IndexByte(message[start:], '"')
	if end < 0 {
		return groupedContribution{}, false
	}
	field := message[start : start+end]
	for _, group := range groups {
		if _, ok := group.value[field]; ok {
			return group, true
		}
	}
	return groupedContribution{}, false
}

func parseIndex(message string) int {
	start := strings.Index(message, "contributes[")
	if start < 0 {
		return -1
	}
	start += len("contributes[")
	end := strings.IndexByte(message[start:], ']')
	if end < 0 {
		return -1
	}
	i, _ := strconv.Atoi(message[start : start+end])
	return i
}
