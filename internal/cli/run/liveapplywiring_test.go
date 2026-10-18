package run

import (
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

// TestLiveApplySeamsAreTheRealSteps fails when a live-apply seam's default stops being the step
// it stands for: the apply's tests swap every seam, so a default left as a stub (or replaced by
// one) would ship a command that applies nothing while its whole suite stays green.
func TestLiveApplySeamsAreTheRealSteps(t *testing.T) {
	ptr := func(f any) uintptr { return reflect.ValueOf(f).Pointer() }
	for _, c := range []struct {
		name       string
		seam, real any
	}{
		{"replaceScopeRepos", replaceScopeReposSeam, brokerscope.ReplaceRepos},
		{"ctxMountsLive", ctxMountsLiveSeam, (*Options).ctxMountsLive},
		{"readCtxMountRecord", readCtxMountRecordSeam, readCtxMountRecord},
		{"diffCtxMounts", diffCtxMountsSeam, diffCtxMounts},
		{"injectCtxMount", injectCtxMountSeam, (*Options).injectCtxMount},
		{"removeCtxMount", removeCtxMountSeam, (*Options).removeCtxMount},
	} {
		if ptr(c.seam) != ptr(c.real) {
			t.Errorf("%s seam is not the real step", c.name)
		}
	}
}
