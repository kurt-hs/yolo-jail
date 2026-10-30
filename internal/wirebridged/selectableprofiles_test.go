package wirebridged

import (
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// resolveShippedProfiles is packload.ResolveProfiles over the profiles a launch can select: the
// user's, plus the implied profile each declared provider is selectable by (OQ-RC3), which is what
// the shipped packs used to declare as a profile of the same name. A launch gets that table from
// the config layer, so a test that resolves a shipped provider by name goes through it too.
func resolveShippedProfiles(packs []*packload.Pack, user map[string]packload.UserProfile,
	providers *jsonx.OrderedMap) (map[string]packload.ResolvedProfile, error) {
	selectable, err := config.SelectableProfiles(user, nil, packs)
	if err != nil {
		return nil, err
	}
	return packload.ResolveProfiles(packs, selectable, providers)
}
