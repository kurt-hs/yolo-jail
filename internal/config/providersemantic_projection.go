package config

import "github.com/mschulkind-oss/yolo-jail/internal/packload"

// LegacyDeriveProjection is the compatibility shape current pack derives consume through
// ctx.providers (packload.LegacyDeriveProjection).
type LegacyDeriveProjection = packload.LegacyDeriveProjection

// ProjectProviderModelsForLegacyDerives lowers composed rows into the derives' flat vocabulary
// (packload.ProjectProviderModelsForLegacyDerives).
func ProjectProviderModelsForLegacyDerives(models ReadableModels) (LegacyDeriveProjection, error) {
	return packload.ProjectProviderModelsForLegacyDerives(models)
}
