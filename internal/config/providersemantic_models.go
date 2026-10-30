package config

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// The readable model-row composition lives in packload (modelrows.go), beside the launch's one
// provider composition that lowers it; these names keep the semantic seam's vocabulary.

// ReadableModel is one selectable canonical model identity and its fully composed facts.
type ReadableModel = packload.ReadableModel

// ReadableModels is provider model composition keyed by selectable id, plus declaration order.
type ReadableModels = packload.ReadableModels

// ValidateProviderModelGraph validates authored twin identities and model ordering.
func ValidateProviderModelGraph(provider packdecl.ProviderContribution) error {
	return packload.ValidateProviderModelGraph(provider)
}

// ComposeProviderModels composes offline catalog facts, provider every_model facts, explicit rows,
// then twin inheritance and overrides (packload.ComposeProviderModels).
func ComposeProviderModels(provider packdecl.ProviderContribution, catalog map[string]packdecl.ProviderModel) (ReadableModels, error) {
	return packload.ComposeProviderModels(provider, catalog)
}

func validateProviderModelFacts(model packdecl.ProviderModel) error {
	return packload.ValidateProviderModelFacts(model)
}

func completeProviderPrice(price *packdecl.ProviderPrice) error {
	return packload.CompleteProviderPrice(price)
}

func mergeProviderModel(base, overlay packdecl.ProviderModel) packdecl.ProviderModel {
	return packload.MergeProviderModel(base, overlay)
}

func cloneProviderModel(model packdecl.ProviderModel) packdecl.ProviderModel {
	return packload.CloneProviderModel(model)
}
