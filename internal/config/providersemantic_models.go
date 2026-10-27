package config

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// ReadableModel is one selectable canonical model identity and its fully composed facts. CompactAt
// is selection metadata, not a replacement id; WireID is the inherited/overridden wire identity.
type ReadableModel struct {
	ID             string
	WireID         string
	WireIDAuthored bool
	CompactAt      int64
	Facts          packdecl.ProviderModel
}

// ReadableModels is provider model composition keyed by selectable id, plus declaration order.
type ReadableModels struct {
	Declared bool
	ByID     map[string]ReadableModel
	Order    []string
}

// ValidateProviderModelGraph validates authored twin identities and model ordering, whether the
// provider is selected or not. It does not require catalog facts or project a consumer.
func ValidateProviderModelGraph(provider packdecl.ProviderContribution) error {
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 2 {
			return nil
		}
		if state[id] == 1 {
			return fmt.Errorf("provider %s model %q: twin cycle detected", provider.Name, id)
		}
		row, ok := provider.ModelRows[id]
		if !ok {
			return fmt.Errorf("provider %s model %q: twin_of names a missing model row", provider.Name, id)
		}
		state[id] = 1
		if row.TwinOf != "" {
			if row.TwinOf == id {
				return fmt.Errorf("provider %s model %q: twin_of cannot name itself", provider.Name, id)
			}
			if err := visit(row.TwinOf); err != nil {
				return fmt.Errorf("provider %s model %q twin_of %q: %w", provider.Name, id, row.TwinOf, err)
			}
		}
		state[id] = 2
		return nil
	}
	for id := range provider.ModelRows {
		if id == "" {
			return fmt.Errorf("provider %s has a model with an empty id", provider.Name)
		}
		if err := visit(id); err != nil {
			return err
		}
	}
	order := append([]string(nil), provider.ModelOrder...)
	if len(order) == 0 {
		for id := range provider.ModelRows {
			order = append(order, id)
		}
		sort.Strings(order)
	}
	seen := map[string]bool{}
	for _, id := range order {
		if _, exists := provider.ModelRows[id]; !exists {
			return fmt.Errorf("provider %s model order names undeclared row %q", provider.Name, id)
		}
		if seen[id] {
			return fmt.Errorf("provider %s model order repeats row %q", provider.Name, id)
		}
		seen[id] = true
	}
	if len(order) != len(provider.ModelRows) {
		return fmt.Errorf("provider %s model order does not cover every declared row", provider.Name)
	}
	if provider.EveryModel != nil {
		if err := validateProviderModelFacts(*provider.EveryModel); err != nil {
			return fmt.Errorf("provider %s every_model: %w", provider.Name, err)
		}
	}
	for id, row := range provider.ModelRows {
		if err := validateProviderModelFacts(row); err != nil {
			return fmt.Errorf("provider %s model %q: %w", provider.Name, id, err)
		}
	}
	return nil
}

// ComposeProviderModels composes offline catalog facts, provider every_model facts, explicit rows,
// then twin inheritance and overrides. Catalog is an explicit offline input; no files or network
// are read here. Facts absent at every layer stay absent rather than receiving invented defaults.
func ComposeProviderModels(provider packdecl.ProviderContribution, catalog map[string]packdecl.ProviderModel) (ReadableModels, error) {
	if err := ValidateProviderModelGraph(provider); err != nil {
		return ReadableModels{}, err
	}
	rows := ReadableModels{Declared: provider.ModelRows != nil, ByID: map[string]ReadableModel{}}
	order := append([]string(nil), provider.ModelOrder...)
	if len(order) == 0 {
		for id := range provider.ModelRows {
			order = append(order, id)
		}
		sort.Strings(order)
	}
	state := map[string]uint8{}
	var resolve func(string) (ReadableModel, error)
	resolve = func(id string) (ReadableModel, error) {
		if state[id] == 2 {
			return rows.ByID[id], nil
		}
		row := provider.ModelRows[id]
		state[id] = 1
		facts := packdecl.ProviderModel{}
		compactAt := int64(0)
		if catalogFacts, ok := catalog[id]; ok {
			if err := validateProviderModelFacts(catalogFacts); err != nil {
				return ReadableModel{}, fmt.Errorf("provider %s model %q catalog facts: %w", provider.Name, id, err)
			}
			facts = cloneProviderModel(catalogFacts)
			compactAt = catalogFacts.CompactAt
		}
		if provider.EveryModel != nil {
			facts = mergeProviderModel(facts, *provider.EveryModel)
			if provider.EveryModel.CompactAt != 0 {
				compactAt = provider.EveryModel.CompactAt
			}
		}
		var inheritedWireID string
		if row.TwinOf != "" {
			base, err := resolve(row.TwinOf)
			if err != nil {
				return ReadableModel{}, fmt.Errorf("provider %s model %q twin_of %q: %w", provider.Name, id, row.TwinOf, err)
			}
			facts = cloneProviderModel(base.Facts)
			inheritedWireID = base.WireID
		}
		facts = mergeProviderModel(facts, row)
		if row.CompactAt != 0 {
			compactAt = row.CompactAt
		}
		if facts.Vendor == "" {
			if maker := inferProviderMaker(id); maker != "" {
				facts.Vendor = maker
			}
		}
		if facts.Vendor == "" {
			return ReadableModel{}, fmt.Errorf("provider %s model %q: maker is ambiguous; author vendor", provider.Name, id)
		}
		if !packdecl.ValidModelVendor(facts.Vendor) {
			return ReadableModel{}, fmt.Errorf("provider %s model %q: vendor %q is not a valid maker token", provider.Name, id, facts.Vendor)
		}
		if err := validateProviderModelFacts(facts); err != nil {
			return ReadableModel{}, fmt.Errorf("provider %s model %q: %w", provider.Name, id, err)
		}
		if err := completeProviderPrice(facts.Price); err != nil {
			return ReadableModel{}, fmt.Errorf("provider %s model %q: %w", provider.Name, id, err)
		}
		wireID := facts.WireID
		if row.TwinOf != "" {
			wireID = inheritedWireID
		}
		if row.WireID != "" {
			wireID = row.WireID
		}
		if wireID == "" {
			wireID = id
		}
		resolved := ReadableModel{ID: id, WireID: wireID, WireIDAuthored: facts.WireID != "" || row.TwinOf != "", CompactAt: compactAt, Facts: cloneProviderModel(facts)}
		rows.ByID[id] = resolved
		state[id] = 2
		return resolved, nil
	}
	for _, id := range order {
		if _, err := resolve(id); err != nil {
			return ReadableModels{}, err
		}
		rows.Order = append(rows.Order, id)
	}
	return rows, nil
}

func validateProviderModelFacts(model packdecl.ProviderModel) error {
	if model.Vendor != "" && !packdecl.ValidModelVendor(model.Vendor) {
		return fmt.Errorf("vendor %q is not a valid lowercase maker token", model.Vendor)
	}
	if model.API != "" {
		if strings.TrimSpace(model.API) != model.API || strings.IndexFunc(model.API, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' }) >= 0 {
			return fmt.Errorf("api must be a non-empty, whitespace-trimmed string")
		}
		if !packdecl.KnownWireAPI(model.API) {
			return fmt.Errorf("api %q is unknown; use one of %s", model.API, strings.Join(packdecl.KnownWireAPIs(), ", "))
		}
	}
	for _, field := range []struct {
		name  string
		value int64
	}{{"context", model.Context}, {"max_tokens", model.MaxTokens}, {"compact_at", model.CompactAt}} {
		if field.value < 0 {
			return fmt.Errorf("%s must be positive when authored", field.name)
		}
	}
	if model.Thinking != nil {
		seen := map[string]bool{}
		for _, value := range model.Thinking {
			if value == "" || seen[value] {
				return fmt.Errorf("thinking must contain unique non-empty values")
			}
			seen[value] = true
		}
	}
	if model.Accepts != nil {
		seen := map[string]bool{}
		for _, value := range model.Accepts {
			if (value != "text" && value != "image") || seen[value] {
				return fmt.Errorf("accepts must contain unique text and/or image values")
			}
			seen[value] = true
		}
	}
	if err := validateProviderImage(model.Images); err != nil {
		return err
	}
	if err := validateProviderPrice(model.Price); err != nil {
		return err
	}
	return nil
}

func validateProviderPrice(price *packdecl.ProviderPrice) error {
	if price == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value *float64
	}{
		{"input", price.Input}, {"output", price.Output}, {"cache_read", price.CacheRead}, {"cache_write", price.CacheWrite},
	} {
		if field.value != nil && (math.IsNaN(*field.value) || math.IsInf(*field.value, 0) || *field.value < 0) {
			return fmt.Errorf("price.%s must be finite and non-negative", field.name)
		}
	}
	return nil
}

func completeProviderPrice(price *packdecl.ProviderPrice) error {
	if price == nil {
		return fmt.Errorf("model has no authored price facts; add price with input, output, cache_read and cache_write rather than relying on consumer defaults")
	}
	for _, field := range []struct {
		name  string
		value *float64
	}{
		{"input", price.Input}, {"output", price.Output}, {"cache_read", price.CacheRead}, {"cache_write", price.CacheWrite},
	} {
		if field.value == nil {
			return fmt.Errorf("price.%s is missing; author all four price rates to avoid a consumer-invented zero", field.name)
		}
	}
	return nil
}

func validateProviderImage(image *packdecl.ProviderImage) error {
	if image == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value int64
	}{{"max_width", image.MaxWidth}, {"max_height", image.MaxHeight}, {"max_bytes", image.MaxBytes}} {
		if field.value <= 0 {
			return fmt.Errorf("images.%s must be a positive integer", field.name)
		}
	}
	if image.JPEGQuality < 1 || image.JPEGQuality > 100 {
		return fmt.Errorf("images.jpeg_quality must be between 1 and 100")
	}
	return nil
}

func inferProviderMaker(id string) string {
	if strings.HasPrefix(id, "global.") {
		parts := strings.Split(id, ".")
		if len(parts) >= 3 && parts[1] != "" && parts[2] != "" && packdecl.ValidModelVendor(parts[1]) {
			return parts[1]
		}
	}
	if maker, model, found := strings.Cut(id, "/"); found && model != "" && packdecl.ValidModelVendor(maker) {
		return maker
	}
	return ""
}

func mergeProviderModel(base, overlay packdecl.ProviderModel) packdecl.ProviderModel {
	if overlay.Name != "" {
		base.Name = overlay.Name
	}
	if overlay.Description != "" {
		base.Description = overlay.Description
	}
	if overlay.Vendor != "" {
		base.Vendor = overlay.Vendor
	}
	if overlay.API != "" {
		base.API = overlay.API
	}
	if overlay.Thinking != nil {
		base.Thinking = cloneStrings(overlay.Thinking)
	}
	if overlay.Images != nil {
		image := *overlay.Images
		base.Images = &image
	}
	if overlay.Price != nil {
		base.Price = mergeProviderPrice(base.Price, overlay.Price)
	}
	if overlay.Context != 0 {
		base.Context = overlay.Context
	}
	if overlay.MaxTokens != 0 {
		base.MaxTokens = overlay.MaxTokens
	}
	if overlay.Reasoning != nil {
		value := *overlay.Reasoning
		base.Reasoning = &value
	}
	if overlay.Accepts != nil {
		base.Accepts = cloneStrings(overlay.Accepts)
	}
	if overlay.Base != "" {
		base.Base = overlay.Base
	}
	if overlay.WireID != "" {
		base.WireID = overlay.WireID
	}
	if overlay.Extends != "" {
		base.Extends = overlay.Extends
	}
	if overlay.OpenRouterRouting != nil {
		base.OpenRouterRouting = cloneAnyMap(overlay.OpenRouterRouting)
	}
	return base
}

func mergeProviderPrice(base, overlay *packdecl.ProviderPrice) *packdecl.ProviderPrice {
	out := cloneProviderPrice(base)
	if out == nil {
		out = &packdecl.ProviderPrice{}
	}
	if overlay.Input != nil {
		value := *overlay.Input
		out.Input = &value
	}
	if overlay.Output != nil {
		value := *overlay.Output
		out.Output = &value
	}
	if overlay.CacheRead != nil {
		value := *overlay.CacheRead
		out.CacheRead = &value
	}
	if overlay.CacheWrite != nil {
		value := *overlay.CacheWrite
		out.CacheWrite = &value
	}
	return out
}

func cloneProviderPrice(price *packdecl.ProviderPrice) *packdecl.ProviderPrice {
	if price == nil {
		return nil
	}
	out := &packdecl.ProviderPrice{}
	if price.Input != nil {
		value := *price.Input
		out.Input = &value
	}
	if price.Output != nil {
		value := *price.Output
		out.Output = &value
	}
	if price.CacheRead != nil {
		value := *price.CacheRead
		out.CacheRead = &value
	}
	if price.CacheWrite != nil {
		value := *price.CacheWrite
		out.CacheWrite = &value
	}
	return out
}

func cloneProviderModel(model packdecl.ProviderModel) packdecl.ProviderModel {
	copy := mergeProviderModel(packdecl.ProviderModel{}, model)
	copy.Price = cloneProviderPrice(model.Price)
	copy.Thinking = cloneStrings(model.Thinking)
	copy.Accepts = cloneStrings(model.Accepts)
	copy.TwinOf, copy.CompactAt = model.TwinOf, model.CompactAt
	return copy
}
