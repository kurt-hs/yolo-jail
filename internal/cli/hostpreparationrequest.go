package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// prepareHostLaunchUpdate is the real host flag's owner-filtered request, before
// render/readiness. Ordinary installers retain their explicit existing route.
var prepareHostLaunchUpdate = func(cmd0 string, out io.Writer, act *run.ActInterrupt) error {
	sel := selectConfiguredHostPacks()
	if problems := sel.problems(); len(problems) > 0 {
		return fmt.Errorf("fresh host selection unavailable: %v — run `yolo check --no-build` and retry", problems)
	}
	bin := filepath.Base(cmd0)
	var keys []packload.Fork
	for _, f := range packload.PatchedTrees(sel.packs) {
		if f.DeliveredAtHost() && ownerRuns(sel.packs, f, bin) {
			keys = append(keys, f)
		}
	}
	for _, f := range packload.PatchedForks(sel.packs) {
		if f.Bin == bin {
			keys = append(keys, f)
		}
	}
	var result error
	sig := act.Scope(func(ctx context.Context) {
		if len(keys) > 0 {
			if config.InJail() {
				result = fmt.Errorf("fresh host source preparation must run on the host; launch `yolo host --update -- %s` there", bin)
				return
			}
			request, err := newPreparationRequest(ctx, keys, captureRuntime(), floorPatchedPlatform())
			if err != nil {
				result = err
				return
			}
			release, err := registerPreparationDemand(ctx, request)
			if err != nil {
				result = err
				return
			}
			defer release()
			for _, f := range keys {
				if _, err = waitFreshPreparation(ctx, request, f.Key()); err != nil {
					result = err
					return
				}
			}
		}
		programs := floorPrograms(sel.packs)
		floor := newHostFloor(out, programs)
		for _, p := range programs {
			if p.Bin() != bin {
				continue
			}
			if p.Install.Kind == packdecl.InstallKindSource {
				if !p.Install.IsPatchedFork() {
					result = fmt.Errorf("%s needs explicit source-pin resolution; run `yolo pack update` then retry the fresh launch", bin)
				}
				return
			}
			if filepath.Base(cmd0) != cmd0 {
				result = fmt.Errorf("--update cannot replace a path-owned %s; update that copy with its owning manager, or launch its bare name through yolo's floor", cmd0)
				return
			}
			if config.InJail() {
				result = fmt.Errorf("--update for host installers must run on the host; run `yolo host --update -- %s` there", bin)
				return
			}
			if floor.OutsideTheFloor(p) {
				result = fmt.Errorf("%s is outside yolo's host floor; update it with its owning provisioner, or launch without --update", bin)
				return
			}
			_, _, result = floor.EnsureFresh(ctx, p)
			return
		}
	})
	if sig != nil && result == nil {
		return context.Canceled
	}
	return result
}
