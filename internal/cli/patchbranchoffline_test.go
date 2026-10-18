package cli

// patchbranchoffline_test.go pins that the default (offline) `yolo pack lint` SAYS it resolved no ref: a
// patch branch whose ?ref= names nothing lints clean offline, so a bare ✓ read as "the ref is good" until
// a launch refused it. `--online`, which does resolve it, says nothing of the kind.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

func TestOfflineLintSaysItDidNotResolveThePatchBranchsRef(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.writePatchBranchManifest(t, "git+file://"+fx.patchBranch+"?ref=no-such-branch")
	rc, out := lintPack(t, fx.forkDir)
	want := "fork forkpack/tool: ref not resolved (offline) — `yolo pack lint --online " +
		shquote.QuoteDisplay(fx.forkDir) + "` checks it (the patch branch's ?ref=no-such-branch, the upstream's ?ref=main)"
	if rc != 0 || !strings.Contains(out, "✓ pack ok") || !strings.Contains(out, want) {
		t.Fatalf("offline lint: rc=%d, want a pass that says %q\n%s", rc, want, out)
	}
	rc, out = lintPack(t, "--online", fx.forkDir)
	if rc == 0 || strings.Contains(out, "not resolved (offline)") || !strings.Contains(out, "no-such-branch") {
		t.Errorf("online lint of a ref naming nothing: rc=%d, want a failure naming it and no offline line\n%s", rc, out)
	}
}

// A PATCH SERIES' upstream ref is not resolved offline either, and is said the same way.
func TestOfflineLintSaysItDidNotResolveASeriesUpstreamRef(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	rc, out := lintPack(t, fx.forkDir)
	if rc != 0 || !strings.Contains(out, "fork forkpack/tool: ref not resolved (offline)") ||
		!strings.Contains(out, "(the upstream's ?ref=main)") {
		t.Fatalf("offline lint of a series fork: rc=%d\n%s", rc, out)
	}
}
