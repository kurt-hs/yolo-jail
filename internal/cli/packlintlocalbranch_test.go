package cli

import (
	"strings"
	"testing"
)

// `yolo pack lint` warns of a patch branch named by a relative or git+file:// locator, naming the refusal whoever
// installs the pack by URL meets and the published URL to name instead; it still passes, and a URL is quiet.
func TestLintWarnsOfALocalPatchBranchLocator(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t) // git+file://<clone>?ref=mine
	rc, out := lintPack(t, fx.forkDir)
	for _, w := range []string{"names a repository on this machine", "their launch is refused",
		"name the published URL instead", "local_clones"} {
		if rc != 0 || !strings.Contains(out, w) {
			t.Errorf("lint of a git+file:// patch branch: rc=%d, want a passing lint warning %q\n%s", rc, w, out)
		}
	}

	inRepoPatchBranch(t, fx, "../..?ref=mine")
	upstreamGit(t, fx.patchBranch, "remote", "set-url", "origin", "git@example.com:o/r.git")
	rc, out = lintPack(t, fx.forkDir)
	if rc != 0 || !strings.Contains(out, "the patch branch ../..?ref=mine names a repository on this machine") ||
		!strings.Contains(out, "git+ssh://git@example.com/o/r.git?ref=mine") {
		t.Errorf("lint of a relative patch branch: rc=%d, want the warning suggesting the clone's published URL\n%s", rc, out)
	}

	fx.writePatchBranchManifest(t, "git+https://example.com/o/r?ref=mine")
	if _, out = lintPack(t, fx.forkDir); strings.Contains(out, "names a repository on this machine") {
		t.Errorf("lint warned of a published URL:\n%s", out)
	}
}
