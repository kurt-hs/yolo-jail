package cli

// patchbranchrelative_test.go drives a RELATIVE patch branch locator (packsrc/relative.go) through the real
// call sites: `yolo pack lint`, offline and `--online`, and the launch's advance, for a fork pack that lives
// inside the repository its `patch_branch` names — `../..?ref=mine` from `<repo>/yolo-packs/forkpack` — and the
// refusals of a locator that climbs out of that repository and of a pack in no repository at all.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// inRepoPatchBranch moves fx's fork pack into the patch branch's own repository, at yolo-packs/forkpack,
// declaring patchBranch, and points the user config at it. The moved pack is untracked in that repository,
// as an author's pack is before its first commit.
func inRepoPatchBranch(t *testing.T, fx *patchBranchAdvanceFixture, patchBranch string) {
	t.Helper()
	fx.forkDir = filepath.Join(fx.patchBranch, "yolo-packs", "forkpack")
	fx.manifest = filepath.Join(fx.forkDir, "pack.json")
	if err := os.MkdirAll(fx.forkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fx.writePatchBranchManifest(t, patchBranch)
	fx.writeUserConfig(t, "")
}

func lintPack(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errw bytes.Buffer
	rc := packMain(append([]string{"lint"}, args...), &out, &errw, false)
	return rc, out.String() + errw.String()
}

func TestARelativePatchBranchNamesThePacksOwnRepository(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	inRepoPatchBranch(t, fx, "../..?ref=mine")
	tip := upstreamGit(t, fx.patchBranch, "rev-parse", "mine")

	if rc, out := lintPack(t, fx.forkDir); rc != 0 {
		t.Fatalf("offline lint of a relative patch branch = %d\n%s", rc, out)
	}
	rc, out := lintPack(t, "--online", fx.forkDir)
	if rc != 0 || !strings.Contains(out, "the patch branch mine@"+shortSHA(tip)+" resolves") {
		t.Fatalf("online lint did not resolve the relative patch branch in the pack's own repository: rc=%d\n%s", rc, out)
	}

	// THE LAUNCH resolves it against the directory the pack was read from, not the copy it staged.
	_, launched, _ := fx.launch(t, "podman")
	if len(fx.builds) != 1 || fx.builds[0] != lines30(map[int]string{10: "ten", 12: "twelve", 14: "fourteen"}) {
		t.Fatalf("builds = %q, want v1.1.0 with the relative patch branch merged\n%s", fx.builds, launched)
	}
	if g := fx.record(t).Good; g == nil || g.Series != packsrc.PatchBranchDigest(tip) {
		t.Fatalf("the good build = %+v, want it keyed on the patch branch's commit %s", g, tip)
	}
}

func TestARelativePatchBranchThatClimbsOutOfItsRepositoryIsRefusedNamingTheBound(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	inRepoPatchBranch(t, fx, "../../..?ref=mine")
	top, err := filepath.EvalSymlinks(fx.patchBranch)
	if err != nil {
		t.Fatal(err)
	}
	rc, out := lintPack(t, fx.forkDir)
	for _, w := range []string{"climbs to", "above the git repository the pack lives in", "bounded by " + top,
		"rev-parse --show-toplevel", "../..?ref=mine"} {
		if rc == 0 || !strings.Contains(out, w) {
			t.Errorf("lint of a climb out of the pack's repository: rc=%d, want a refusal containing %q\n%s", rc, w, out)
		}
	}
}

func TestARelativePatchBranchOfAPackInNoRepositoryIsRefused(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.writePatchBranchManifest(t, "../..?ref=mine") // the fixture's pack directory is in no repository
	rc, out := lintPack(t, fx.forkDir)
	if rc == 0 || !strings.Contains(out, "is in no git repository") || !strings.Contains(out, "git+file:///") {
		t.Errorf("lint of a relative patch branch in no repository: rc=%d, want a refusal naming the absolute form\n%s", rc, out)
	}
}

// A FILTERED pack entry is read from a copy (config.ResolvePack), so the launch resolves a relative patch
// branch against the directory the copy was made from, never the copy.
func TestARelativePatchBranchOfAFilteredPackResolvesAgainstItsSource(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	inRepoPatchBranch(t, fx, "../..?ref=mine")
	writeFile(t, filepath.Join(fx.home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[`+
		`{"source":"file://`+filepath.Join(fx.packs, "basepack")+`","name":"basepack"},`+
		`{"source":"file://`+fx.forkDir+`","name":"forkpack","exclude":["notes/**"]}]}`)
	_, launched, _ := fx.launch(t, "podman")
	if len(fx.builds) != 1 {
		t.Fatalf("a filtered pack's relative patch branch built %d\n%s", len(fx.builds), launched)
	}
}

// `yolo pack series check` resolves a relative patch branch against the pack directory it is named, and merges it.
func TestSeriesCheckOfARelativePatchBranchMergesThePacksOwnRepository(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	inRepoPatchBranch(t, fx, "../..?ref=mine")
	tip := upstreamGit(t, fx.patchBranch, "rev-parse", "mine")
	rc, out, errw := seriesVerb(t, "check", "--online", fx.forkDir)
	if rc != 0 || !strings.Contains(out, "merges the patch branch mine@"+shortSHA(tip)+" cleanly") {
		t.Errorf("series check of a relative patch branch: rc=%d\n%s%s", rc, out, errw)
	}
}

// THE HOST FLOOR's fork carries the directory a relative patch branch resolves against.
func TestTheFloorForkKeepsThePatchBranchDir(t *testing.T) {
	in := packdecl.Install{Bin: "tool", ForkedBy: "forkpack", PatchBranch: "../..?ref=mine", ForkRoot: "/staged",
		ForkPatchBranchDir: "/src/forkpack"}
	if f := floorFork(hostfloor.Program{Pack: "basepack", Install: in}); f.PatchBranchDir != "/src/forkpack" || f.Root != "/staged" {
		t.Errorf("floorFork = %+v, want the fork's root and patch branch directory", f)
	}
}

// A RELATIVE LOCATOR NAMING A DIRECTORY INSIDE THE REPOSITORY, not its root, is refused with the spelling
// that names the root.
func TestARelativePatchBranchNamingASubdirectoryNamesTheRoot(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	inRepoPatchBranch(t, fx, "..?ref=mine")
	rc, out := lintPack(t, fx.forkDir)
	if rc == 0 || !strings.Contains(out, "not a repository's root") || !strings.Contains(out, "../..?ref=mine") {
		t.Errorf("lint of a relative patch branch naming yolo-packs/: rc=%d\n%s", rc, out)
	}
}

// A FETCHED-ONLY PATCH BRANCH (yolo-fix-requests §28): the pack's own repository holds `mine` only as
// refs/remotes/origin/mine. The launch builds from it with no local branch made and names the ref that
// answered; `yolo pack series check` names the same ref and commit; `yolo pack update` records the same
// resolution, so a verdict and a build cannot disagree about which ref they judged.
func TestAFetchedOnlyRelativePatchBranchBuildsAndEveryActNamesTheSameRef(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	published := fx.patchBranch
	tip := upstreamGit(t, published, "rev-parse", "mine")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	machine := filepath.Join(root, "machine")
	upstreamGit(t, root, "clone", "-q", published, machine)
	upstreamGit(t, machine, "checkout", "-q", "-b", "work", fx.base)
	upstreamGit(t, machine, "branch", "-q", "-D", "mine")
	hasLocal := func() bool {
		return exec.Command("git", "-C", machine, "show-ref", "--verify", "--quiet", "refs/heads/mine").Run() == nil
	}
	if hasLocal() {
		t.Fatal("fixture: the machine clone still holds a local mine")
	}
	fx.patchBranch = machine
	inRepoPatchBranch(t, fx, "../..?ref=mine")
	label := "mine@" + shortSHA(tip) + " (refs/remotes/origin/mine)"

	_, out, _ := fx.launch(t, "podman")
	if len(fx.builds) != 1 || fx.builds[0] != lines30(map[int]string{10: "ten", 12: "twelve", 14: "fourteen"}) {
		t.Fatalf("builds = %q, want v1.1.0 with the fetched patch branch merged\n%s", fx.builds, out)
	}
	if !strings.Contains(out, "+ patch branch "+label) {
		t.Errorf("the launch does not name the ref that answered and its commit (%q):\n%s", label, out)
	}
	rec := fx.record(t)
	if rec.Good == nil || rec.Good.PatchBranch != label || rec.PatchBranch == nil ||
		rec.PatchBranch.Ref != "refs/remotes/origin/mine" || rec.PatchBranch.Commit != tip {
		t.Fatalf("the record = good %+v, patch branch %+v; want %q at %s", rec.Good, rec.PatchBranch, label, tip)
	}
	if hasLocal() {
		t.Error("the launch created a local branch in the pack's repository")
	}

	if rc, sc, errw := seriesVerb(t, "check", fx.forkDir); rc != 0 || !strings.Contains(sc, "merges the patch branch "+label+" cleanly") {
		t.Errorf("`yolo pack series check` does not name the same ref and commit: rc=%d\n%s\n%s", rc, sc, errw)
	}

	// The branch moves on origin and the machine fetches: `yolo pack update` records the new commit from the
	// same remote-tracking ref.
	fx.patchBranch = published
	moved := fx.patchBranchCommit(t, map[int]string{10: "ten", 12: "twelve", 20: "twenty"})
	fx.patchBranch = machine
	upstreamGit(t, machine, "fetch", "-q", "origin")
	if rc, uo, ue := packVerb(t, "update"); rc != 0 {
		t.Fatalf("`yolo pack update`: rc=%d\n%s\n%s", rc, uo, ue)
	}
	if r := fx.record(t).PatchBranch; r == nil || r.Ref != "refs/remotes/origin/mine" || r.Commit != moved {
		t.Errorf("`yolo pack update` recorded %+v, want refs/remotes/origin/mine at %s", r, moved)
	}
	if hasLocal() {
		t.Error("`yolo pack update` created a local branch in the pack's repository")
	}
}
