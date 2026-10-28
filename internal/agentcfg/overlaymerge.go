package agentcfg

import "github.com/mschulkind-oss/yolo-jail/internal/arraymerge"

// MergeAppendingArrays merges one grouped-manifest object layer using the same array
// semantics as Compose. Packload uses it for the own-surface posture fold before Compose;
// foreign posture overlays use Overlay.AppendArrays directly.
func MergeAppendingArrays(base, patch map[string]any) map[string]any {
	return arraymerge.MergeAppending(base, patch)
}

func mergeAppendingArrays(base, patch map[string]any) map[string]any {
	return arraymerge.MergeAppending(base, patch)
}
