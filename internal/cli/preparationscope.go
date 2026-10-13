package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// preparationScopedResolver owns only its filtered snapshots, not the leased process tree.
// The caller releases after all readers of the returned declarations have finished.
func preparationScopedResolver(ctx context.Context) (func(config.PackEntry) (*packload.Pack, error), func()) {
	var owned []string
	resolve := func(e config.PackEntry) (*packload.Pack, error) {
		spec := config.ResolvePackSpec{Ctx: ctx, NoWait: true, Detached: true}
		if len(e.Only) > 0 || len(e.Exclude) > 0 {
			dir, err := packload.ProcessPackDir(e.Slug())
			if err != nil {
				return nil, err
			}
			owned = append(owned, filepath.Dir(dir))
			spec.Dest = dir
		}
		res, err := config.ResolvePack(e, spec)
		if err != nil {
			if errors.Is(err, packsrc.ErrNotFetched) && !e.IsLocal() && !e.Embedded() {
				return nil, storeMissError{err}
			}
			return nil, err
		}
		return resolvedOrProblems(e, res)
	}
	return resolve, func() {
		for _, dir := range owned {
			_ = os.RemoveAll(dir)
		}
		owned = nil
	}
}
