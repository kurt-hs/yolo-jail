package cli

// patchedcarry_test.go drives a CARRY (docs/design/patched-forks.md §3.5, OQ-PFK6; packsrc/carry.go)
// through the real launch advance, `yolo pack lint` and `yolo pack status`, against local
// repositories with the build jail substituted: a clean merge builds the merged tree and keys the
// good build on the carry's commit; a moved branch rebuilds; a conflict refuses naming the paths and
// the merge that repairs it; an old git is refused by name; and a carry repository that cannot be
// fetched is a failed fetch a launch with nothing to run retries at once (PF-D88).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// carryAdvanceFixture is the patched advance fixture with its series replaced by a carry: a clone of
// the upstream whose branch `carry`, started at v1.0.0, makes the series' two edits as commits.
type carryAdvanceFixture struct {
	*patchedAdvanceFixture
	carry string
}

func newCarryAdvanceFixture(t *testing.T) *carryAdvanceFixture {
	t.Helper()
	fx := &carryAdvanceFixture{patchedAdvanceFixture: newPatchedAdvanceFixture(t, "")}
	fx.carry = filepath.Join(t.TempDir(), "carry")
	upstreamGit(t, filepath.Dir(fx.carry), "clone", "-q", fx.repo, fx.carry)
	upstreamGit(t, fx.carry, "checkout", "-q", "-b", "carry", fx.base)
	fx.carryCommit(t, map[int]string{10: "ten"})
	fx.carryCommit(t, map[int]string{10: "ten", 12: "twelve"})
	fx.writeCarryManifest(t, "git+file://"+fx.carry+"?ref=carry")
	if err := os.RemoveAll(filepath.Join(fx.forkDir, "patches")); err != nil { // a carry ships no series
		t.Fatal(err)
	}
	return fx
}

func (fx *carryAdvanceFixture) writeCarryManifest(t *testing.T, carry string) {
	t.Helper()
	writeFile(t, fx.manifest, `{"name":"forkpack","contributes":[{"kind":"program","bin":"tool","via":"source",`+
		`"fork_of":"basepack","source":"git+file://`+fx.repo+`?ref=main","carry":"`+carry+`",`+
		`"build":"sh build.sh","produces":[".local/bin/tool"]}]}`)
}

// carryCommit commits f.txt with edits on the carry branch, returning the commit.
func (fx *carryAdvanceFixture) carryCommit(t *testing.T, edits map[int]string) string {
	t.Helper()
	writeFile(t, filepath.Join(fx.carry, "f.txt"), lines30(edits))
	upstreamGit(t, fx.carry, "add", "-A")
	upstreamGit(t, fx.carry, "commit", "-qm", "carry")
	return upstreamGit(t, fx.carry, "rev-parse", "HEAD")
}

func TestACarryLaunchBuildsTheMergedTreeKeyedOnTheCarryCommit(t *testing.T) {
	fx := newCarryAdvanceFixture(t)
	v11 := fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	tip := upstreamGit(t, fx.carry, "rev-parse", "carry")
	r, out, handed := fx.launch(t, "podman")
	if len(fx.builds) != 1 || fx.builds[0] != lines30(map[int]string{10: "ten", 12: "twelve", 14: "fourteen"}) {
		t.Fatalf("builds = %q, want one of v1.1.0 with the carry merged\n%s", fx.builds, out)
	}
	g := fx.record(t).Good
	label := "carry@" + shortSHA(tip)
	if g == nil || g.Commit != v11 || g.Series != packsrc.CarryDigest(tip) || g.Carry != label || g.Entry != r.delivery.Key {
		t.Fatalf("the good build = %+v, want v1.1.0 keyed on the carry's commit %s", g, tip)
	}
	if len(handed) != 1 || handed[0].Key != g.Entry {
		t.Errorf("handed %+v, want the good build", handed)
	}
	if !strings.Contains(out, "v1.1.0 ("+shortSHA(v11)+") + carry "+label) {
		t.Errorf("the launch does not name the carry it merged:\n%s", out)
	}
	entry, err := (&capture.Store{Dir: paths.CapturesDir()}).Resolve(g.Entry)
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := entrypoint.ReadBuildReceipts(capture.ReceiptsPath(entry.Root))
	if len(recs) != 1 || recs[0].Series != packsrc.CarryDigest(tip) || recs[0].Revision != v11 {
		t.Errorf("the build receipt = %+v, want the upstream commit and the carry's digest", recs)
	}

	// `yolo pack status` describes the carry, its merge base and the good build's carry.
	rc, status, _ := packVerb(t, "status")
	for _, w := range []string{"carry: " + label + " of file://" + fx.carry, "merge base with upstream " + shortSHA(fx.base),
		"+ carry " + label, "carry lag: the carry's merge base with upstream v1.0.0", "which takes the carry cleanly"} {
		if !strings.Contains(status, w) {
			t.Errorf("status (rc %d) lacks %q:\n%s", rc, w, status)
		}
	}
}

// A MOVED CARRY BRANCH is a new commit and a new recipe: the next check sees it and rebuilds.
func TestAMovedCarryBranchRebuilds(t *testing.T) {
	fx := newCarryAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	if _, out, _ := fx.launch(t, "podman"); len(fx.builds) != 1 {
		t.Fatalf("the first launch built %d\n%s", len(fx.builds), out)
	}
	moved := fx.carryCommit(t, map[int]string{10: "ten", 12: "twelve", 20: "twenty"})
	fx.later(2 * time.Hour)
	r, out, _ := fx.launch(t, "podman")
	if len(fx.builds) != 2 || fx.builds[1] != lines30(map[int]string{10: "ten", 12: "twelve", 14: "fourteen", 20: "twenty"}) {
		t.Fatalf("after the branch moved builds = %q, want v1.1.0 with the moved carry merged\n%s", fx.builds, out)
	}
	if g := fx.record(t).Good; g == nil || g.Series != packsrc.CarryDigest(moved) || g.Entry != r.delivery.Key {
		t.Errorf("the good build = %+v, want it keyed on the moved carry %s", g, moved)
	}
}

// A CONFLICT REFUSES, naming the conflicting paths and the merge that repairs the carry.
func TestACarryConflictRefusesNamingThePaths(t *testing.T) {
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "")
	fx := newCarryAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{11: "eleven"})
	r, out, _ := fx.launch(t, "podman")
	pf := r.patchFailure
	if len(fx.builds) != 0 || !r.failed || pf == nil || pf.Kind != "conflict" || pf.Target.Tag != "v1.1.0" ||
		len(pf.Paths) != 1 || pf.Paths[0] != "f.txt" || !strings.Contains(pf.Member, "carry@") {
		t.Fatalf("a conflicting carry built %d, result %+v\n%s", len(fx.builds), pf, out)
	}
	var block bytes.Buffer
	writePatchFailure(&block, pf, fx.fork(t), packsrc.PatchBypass{}, "")
	for _, w := range []string{"the carry does not merge into upstream v1.1.0", "Conflict: f.txt",
		packsrc.CarryRecipeLead, "git checkout carry", "git merge " + pf.Target.Commit, "git push origin carry"} {
		if !strings.Contains(block.String(), w) {
			t.Errorf("the error block lacks %q:\n%s", w, block.String())
		}
	}
}

// AN OLD GIT is refused by name, with updating git as the next step, and nothing is merged otherwise.
func TestAnOldGitRefusesACarryLaunchByName(t *testing.T) {
	fx := newCarryAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	patchedGitWrapper(t, `if [ "$1" = version ]; then echo "git version 2.37.1"; exit 0; fi`)
	r, out, _ := fx.launch(t, "podman")
	if len(fx.builds) != 0 || r.delivery.Key != "" {
		t.Fatalf("under git 2.37.1 the launch built %d and handed %+v\n%s", len(fx.builds), r.delivery, out)
	}
	if !strings.Contains(out, "merging a carry needs git 2.40 or newer (`git merge-tree --write-tree`), and this host's git is 2.37.1") ||
		!strings.Contains(out, "update git") {
		t.Errorf("the launch does not refuse the old git by name:\n%s", out)
	}
}

// PF-D88 FOR THE CARRY: a carry repository that cannot be fetched refuses a launch with nothing to
// run, and the next launch fetches again at once rather than an hour later.
func TestACarryThatCannotBeFetchedIsRetriedByTheNextLaunch(t *testing.T) {
	fx := newCarryAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	gone := filepath.Join(t.TempDir(), "gone")
	fx.writeCarryManifest(t, "git+file://"+gone+"?ref=carry")
	r, out, _ := fx.launch(t, "podman")
	if r.delivery.Key != "" || !strings.Contains(r.delivery.Reason, "could not be fetched") {
		t.Fatalf("an unreachable carry handed %+v\n%s", r.delivery, out)
	}
	seq := fx.record(t).Seq
	if err := os.Rename(fx.carry, gone); err != nil {
		t.Fatal(err)
	}
	fx.later(time.Minute)
	r, out, _ = fx.launch(t, "podman")
	if fx.record(t).Seq == seq || r.delivery.Key == "" || len(fx.builds) != 1 {
		t.Fatalf("a minute later the launch did not fetch the carry again: handed %+v, builds %d\n%s",
			r.delivery, len(fx.builds), out)
	}
}

// `yolo pack lint` accepts a pack whose fork names a carry, and refuses one naming both.
func TestPackLintAcceptsACarry(t *testing.T) {
	fx := newCarryAdvanceFixture(t)
	var out, errw bytes.Buffer
	if rc := packMain([]string{"lint", fx.forkDir}, &out, &errw, false); rc != 0 {
		t.Fatalf("lint of a carry fork = %d\n%s%s", rc, out.String(), errw.String())
	}
	writeFile(t, fx.manifest, strings.Replace(readFileT(t, fx.manifest), `"carry":`, `"patches":"patches","carry":`, 1))
	out.Reset()
	errw.Reset()
	if rc := packMain([]string{"lint", fx.forkDir}, &out, &errw, false); rc == 0 ||
		!strings.Contains(out.String()+errw.String(), "forkpack/tool") {
		t.Errorf("lint of a fork naming both = %d, want a refusal naming the owner key\n%s%s", rc, out.String(), errw.String())
	}
}
