package packsrc

// patchbranch_test.go drives a PATCH BRANCH (patchbranch.go; docs/reference/patched-forks.md §3.5,
// OQ-PFK6) through the real check and walk against local repositories: the check fetches the patch
// branch's repository into a mirror of its own and resolves its commit and merge base; the walk merges
// it into the newest candidate it fits, its tree both sides' changes; a conflict names the patch branch
// and the paths; a moved branch is a new digest; a patch branch with no shared history is the check's
// problem; a patch branch repository that cannot be reached is the check's failed fetch (PF-D88); and an
// old git is refused by name.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// patchBranchFixture is the patched upstream with a patch branch repository beside it: a clone of the upstream
// whose branch `patch_branch`, started at v1.0.0, changes line 10 and then line 12 — the series' two
// members, as commits of a branch rather than files of a pack.
type patchBranchFixture struct {
	*patchedUpstream
	patchBranchRepo string
}

func newPatchBranchFixture(t *testing.T) *patchBranchFixture {
	t.Helper()
	u := newPatchedUpstream(t)
	c := filepath.Join(t.TempDir(), "branch")
	gitIn(t, filepath.Dir(c), "clone", "-q", u.repo, c)
	gitIn(t, c, "checkout", "-q", "-b", "mine", u.base)
	commitFile(t, c, "f.txt", thirtyLines(map[int]string{10: "ten"}))
	commitFile(t, c, "f.txt", thirtyLines(map[int]string{10: "ten", 12: "twelve"}))
	return &patchBranchFixture{patchedUpstream: u, patchBranchRepo: c}
}

func (f *patchBranchFixture) patchBranchSource() string {
	return "git+file://" + f.patchBranchRepo + "?ref=mine"
}

func (f *patchBranchFixture) patchBranchWant(ref string) PatchedWant {
	return PatchedWant{Owner: "forkpack/tool", Source: f.source(ref), PatchBranch: f.patchBranchSource()}
}

// checkedPatchBranch runs a forced check and returns the patch branch's series as the record now names it.
func (f *patchBranchFixture) checkedPatchBranch(t *testing.T) (*Series, CheckResult) {
	t.Helper()
	res := f.check(t, f.patchBranchWant("main"), true)
	if p := res.Record.Check.Problem; p != "" {
		t.Fatalf("check problem: %s", p)
	}
	s, err := PatchBranchSeries(f.patchBranchSource(), f.source("main"), res.Record)
	if err != nil {
		t.Fatalf("PatchBranchSeries: %v", err)
	}
	return s, res
}

func TestACheckResolvesThePatchBranchAndTheWalkMergesItIntoTheNewestFit(t *testing.T) {
	f := newPatchBranchFixture(t)
	v11 := f.release(t, "v1.1.0", map[int]string{14: "fourteen"})
	tip := gitIn(t, f.patchBranchRepo, "rev-parse", "mine")
	series, res := f.checkedPatchBranch(t)
	if series.PatchBranch == nil || series.PatchBranch.Commit != tip || series.Base != f.base || series.Digest != PatchBranchDigest(tip) {
		t.Fatalf("the patch branch's series = %+v (patch branch %+v), want commit %s and base %s", series, series.PatchBranch, tip, f.base)
	}
	if got := listLabels(res.Record.Check.List); got != "v1.1.0 v1.0.0" {
		t.Errorf("the walk's list = %q, want the versions containing the patch branch's base", got)
	}
	var tree string
	w := f.store.WalkSeries(res.Inputs.Repo, "", series, res.Record.Check.List, WalkOptions{
		OnFit: func(dir string) error {
			data, err := os.ReadFile(filepath.Join(dir, "f.txt"))
			tree = string(data)
			return err
		}})
	if w.Err != nil || w.Fit != 0 || w.Results[0].Entry.Commit != v11 {
		t.Fatalf("the walk = %+v, want v1.1.0 the fit", w)
	}
	if want := thirtyLines(map[int]string{10: "ten", 12: "twelve", 14: "fourteen"}); tree != want {
		t.Errorf("the merged tree's f.txt =\n%s\nwant both sides' changes", tree)
	}
}

func TestAPatchBranchConflictNamesThePatchBranchAndThePaths(t *testing.T) {
	f := newPatchBranchFixture(t)
	f.release(t, "v1.1.0", map[int]string{11: "eleven"})
	series, res := f.checkedPatchBranch(t)
	w := f.store.WalkSeries(res.Inputs.Repo, "", series, res.Record.Check.List, WalkOptions{StopOnPatchFailure: true})
	pf := w.PatchFailure()
	if pf == nil || pf.Kind != "conflict" || pf.Target.Tag != "v1.1.0" || len(pf.Paths) != 1 || pf.Paths[0] != "f.txt" ||
		!strings.Contains(pf.Member, "mine@") {
		t.Fatalf("the walk's failure = %+v, want a conflict at v1.1.0 naming the patch branch and f.txt", pf)
	}
}

// A MOVED BRANCH is a new commit and a new digest, which keys a new build (§3.5).
func TestAMovedPatchBranchIsANewDigest(t *testing.T) {
	f := newPatchBranchFixture(t)
	before, _ := f.checkedPatchBranch(t)
	moved := commitFile(t, f.patchBranchRepo, "g.txt", "more\n")
	after, _ := f.checkedPatchBranch(t)
	if after.PatchBranch.Commit != moved || after.Digest == before.Digest {
		t.Fatalf("after the branch moved the series is %+v, want commit %s under a new digest", after.PatchBranch, moved)
	}
}

func TestAPatchBranchWithNoSharedHistoryIsTheChecksProblem(t *testing.T) {
	f := newPatchBranchFixture(t)
	gitIn(t, f.patchBranchRepo, "checkout", "-q", "--orphan", "lonely")
	commitFile(t, f.patchBranchRepo, "f.txt", "alone\n")
	w := f.patchBranchWant("main")
	w.PatchBranch = "git+file://" + f.patchBranchRepo + "?ref=lonely"
	res := f.check(t, w, true)
	if p := res.Record.Check.Problem; !strings.Contains(p, "shares no history") || !strings.Contains(p, "merge the upstream into it") {
		t.Errorf("the check's problem = %q, want the unrelated histories named with the next step", p)
	}
}

// PF-D88 FOR THE PATCH BRANCH: a patch branch repository that cannot be fetched is the check's failed fetch, so an
// advance with nothing to run checks again at once rather than an hour later; the copy the mirror
// already holds keeps answering.
func TestAPatchBranchThatCannotBeFetchedIsAFailedFetch(t *testing.T) {
	f := newPatchBranchFixture(t)
	w := f.patchBranchWant("main")
	w.PatchBranch = "git+file://" + filepath.Join(t.TempDir(), "gone") + "?ref=mine"
	res := f.check(t, w, true)
	c := res.Record.Check
	if c.FetchErr == "" || !strings.Contains(c.FetchErr, "fetching the patch branch") || !strings.Contains(c.Problem, "has never fetched the patch branch") {
		t.Fatalf("a patch branch never fetched = fetch %q, problem %q", c.FetchErr, c.Problem)
	}
	f.now = f.now.Add(time.Minute)
	retry := f.store.CheckPatched(w, CheckOptions{RetryFailedFetch: true, Now: func() time.Time { return f.now }})
	if !retry.Ran {
		t.Error("with nothing to run, the failed patch branch fetch held the next check for the hour")
	}

	// Once fetched, a patch branch repository gone missing still answers from the mirror's copy.
	good := f.patchBranchWant("main")
	if _, res := f.checkedPatchBranch(t); res.Record.PatchBranch == nil {
		t.Fatal("the patch branch did not resolve")
	}
	if err := os.Rename(f.patchBranchRepo, f.patchBranchRepo+".moved"); err != nil {
		t.Fatal(err)
	}
	res = f.check(t, good, true)
	if res.Record.Check.FetchErr == "" || res.Record.Check.Problem != "" || res.Record.Check.PatchBranch == nil {
		t.Errorf("a failed fetch of a fetched patch branch = %+v, want the mirror's copy to answer", res.Record.Check)
	}
}

// AN OLD GIT is refused by the replay's one error, in the patch branch's words, and never merged otherwise.
func TestAnOldGitRefusesThePatchBranchByName(t *testing.T) {
	f := newPatchBranchFixture(t)
	f.release(t, "v1.1.0", map[int]string{14: "fourteen"})
	series, res := f.checkedPatchBranch(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	wrapper := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo \"git version 2.37.1\"; exit 0; fi\n"+
		"exec "+realGit+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := *f.store
	old.Git = wrapper
	w := old.WalkSeries(res.Inputs.Repo, "", series, res.Record.Check.List, WalkOptions{})
	var tooOld *GitTooOldError
	if !errors.As(w.Err, &tooOld) || !strings.Contains(tooOld.Error(), "merging a patch branch needs git 2.40 or newer (`git merge-tree --write-tree`)") ||
		!strings.Contains(tooOld.Error(), "2.37.1") || len(w.Results) != 0 {
		t.Fatalf("under git 2.37.1 the walk = %+v (err %v), want the patch branch refused by name", w, w.Err)
	}
}
