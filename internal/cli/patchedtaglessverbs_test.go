package cli

// patchedtaglessverbs_test.go pins what the explicit acts say of a patched fork over an upstream
// branch that carries no version tag the default release rule reads (docs/reference/patched-forks.md
// PF-D60): `yolo pack series check` says why the list is empty and names `follow: "head"`, as `yolo
// pack update` and `status` do, and the note a
// launch says once is said again when what the check reads changes.

import (
	"strings"
	"testing"
	"time"
)

// THE SERIES CHECK ON A TAGLESS UPSTREAM says it too: it gives the base's verdict with the note,
// and with no check record behind it names this machine's good build or the series' base.
func TestTheSeriesCheckOnATaglessUpstreamSaysWhy(t *testing.T) {
	f := newPatchedFixture(t, "")
	upstreamGit(t, f.repo, "tag", "-d", "v1.0.0")
	f.commitMsg(t, "untagged one", "", map[int]string{14: "fourteen"})
	tmp := inAJail(t, f.packs)
	stays := "so what runs stays at this machine's good build, or with none the series' base " + shortSHA(f.base) +
		", until a tag appears or `follow` changes — `follow: \"head\"` follows the branch's commits"
	rc, out, errw := seriesVerb(t, "check", f.forkDir)
	if rc != 0 || !strings.Contains(out, "the series' base "+shortSHA(f.base)+" (no version of the branch contains it) "+
		"takes the series") || !strings.Contains(out, stays) {
		t.Errorf("the series check on a tagless upstream: rc=%d\n%s\n%s", rc, out, errw)
	}
	noScratchLeft(t, tmp)
}

// THE NOTE IS SAID AGAIN WHEN `follow` CHANGES ("the first launch whose check finds it, what it
// reads unchanged since, says it once"): a turn from `release` to `release:agent@` over the same
// tagless branch is a new rule that finds nothing, and its note names the new rule.
func TestTheTaglessNoteIsSaidAgainWhenFollowChanges(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "head")
	upstreamGit(t, fx.repo, "tag", "-d", "v1.0.0")
	fx.commitMsg(t, "untagged", "", map[int]string{14: "fourteen"})
	if r, out, _ := fx.launch(t, "podman"); r.delivery.Key == "" {
		t.Fatalf("the head's first advance handed nothing\n%s", out)
	}
	fx.writeManifest(t, "main", "")
	if _, out, _ := fx.launch(t, "podman"); !strings.Contains(out, "`follow: \"release\"` reads") {
		t.Fatalf("the turn to release did not say the note:\n%s", out)
	}
	fx.later(10 * time.Minute)
	fx.writeManifest(t, "main", "release:agent@")
	if _, out, _ := fx.launch(t, "podman"); !strings.Contains(out, "`follow: \"release:agent@\"` reads") {
		t.Errorf("the turn to release:agent@ did not say its note:\n%s", out)
	}
}

// `yolo pack status` AND `update` WITH A GOOD BUILD on a tagless upstream name the good build as
// where the fork stays (stayAt), not the series' base, which is not what runs.
func TestStatusOnATaglessUpstreamNamesTheGoodBuild(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "head")
	upstreamGit(t, fx.repo, "tag", "-d", "v1.0.0")
	tip := fx.commitMsg(t, "untagged", "", map[int]string{14: "fourteen"})
	if r, out, _ := fx.launch(t, "podman"); r.delivery.Key == "" {
		t.Fatalf("the head's first advance handed nothing\n%s", out)
	}
	fx.writeManifest(t, "main", "")
	for _, verb := range []string{"update", "status"} {
		_, out, errw := packVerb(t, verb)
		if w := "so what runs stays at the good build " + shortSHA(tip) + " until a tag appears"; !strings.Contains(out, w) {
			t.Errorf("%s lacks %q:\n%s\n%s", verb, w, out, errw)
		}
	}
}
