package packsrc

// carry_test.go drives a CARRY (carry.go; docs/design/patched-forks.md §3.5, OQ-PFK6) through the
// real check and walk against local repositories: the check fetches the carry into the upstream's
// mirror and resolves its commit and merge base; the walk merges it into the newest candidate it
// fits, its tree both sides' changes; a conflict names the carry and the paths; a moved branch is a
// new digest; a carry with no shared history is the check's problem; a carry repository that
// cannot be reached is the check's failed fetch (PF-D88); and an old git is refused by name.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// carryFixture is the patched upstream with a carry repository beside it: a clone of the upstream
// whose branch `carry`, started at v1.0.0, changes line 10 and then line 12 — the series' two
// members, as commits of a branch rather than files of a pack.
type carryFixture struct {
	*patchedUpstream
	carryRepo string
}

func newCarryFixture(t *testing.T) *carryFixture {
	t.Helper()
	u := newPatchedUpstream(t)
	c := filepath.Join(t.TempDir(), "carry")
	gitIn(t, filepath.Dir(c), "clone", "-q", u.repo, c)
	gitIn(t, c, "checkout", "-q", "-b", "carry", u.base)
	commitFile(t, c, "f.txt", thirtyLines(map[int]string{10: "ten"}))
	commitFile(t, c, "f.txt", thirtyLines(map[int]string{10: "ten", 12: "twelve"}))
	return &carryFixture{patchedUpstream: u, carryRepo: c}
}

func (f *carryFixture) carrySource() string { return "git+file://" + f.carryRepo + "?ref=carry" }

func (f *carryFixture) carryWant(ref string) PatchedWant {
	return PatchedWant{Owner: "forkpack/tool", Source: f.source(ref), Carry: f.carrySource()}
}

// checkedCarry runs a forced check and returns the carry's series as the record now names it.
func (f *carryFixture) checkedCarry(t *testing.T) (*Series, CheckResult) {
	t.Helper()
	res := f.check(t, f.carryWant("main"), true)
	if p := res.Record.Check.Problem; p != "" {
		t.Fatalf("check problem: %s", p)
	}
	s, err := CarrySeries(f.carrySource(), f.source("main"), res.Record)
	if err != nil {
		t.Fatalf("CarrySeries: %v", err)
	}
	return s, res
}

func TestACheckResolvesTheCarryAndTheWalkMergesItIntoTheNewestFit(t *testing.T) {
	f := newCarryFixture(t)
	v11 := f.release(t, "v1.1.0", map[int]string{14: "fourteen"})
	tip := gitIn(t, f.carryRepo, "rev-parse", "carry")
	series, res := f.checkedCarry(t)
	if series.Carry == nil || series.Carry.Commit != tip || series.Base != f.base || series.Digest != CarryDigest(tip) {
		t.Fatalf("the carry's series = %+v (carry %+v), want commit %s and base %s", series, series.Carry, tip, f.base)
	}
	if got := listLabels(res.Record.Check.List); got != "v1.1.0 v1.0.0" {
		t.Errorf("the walk's list = %q, want the versions containing the carry's base", got)
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

func TestACarryConflictNamesTheCarryAndThePaths(t *testing.T) {
	f := newCarryFixture(t)
	f.release(t, "v1.1.0", map[int]string{11: "eleven"})
	series, res := f.checkedCarry(t)
	w := f.store.WalkSeries(res.Inputs.Repo, "", series, res.Record.Check.List, WalkOptions{StopOnPatchFailure: true})
	pf := w.PatchFailure()
	if pf == nil || pf.Kind != "conflict" || pf.Target.Tag != "v1.1.0" || len(pf.Paths) != 1 || pf.Paths[0] != "f.txt" ||
		!strings.Contains(pf.Member, "carry@") {
		t.Fatalf("the walk's failure = %+v, want a conflict at v1.1.0 naming the carry and f.txt", pf)
	}
}

// A MOVED BRANCH is a new commit and a new digest, which keys a new build (§3.5).
func TestAMovedCarryBranchIsANewDigest(t *testing.T) {
	f := newCarryFixture(t)
	before, _ := f.checkedCarry(t)
	moved := commitFile(t, f.carryRepo, "g.txt", "more\n")
	after, _ := f.checkedCarry(t)
	if after.Carry.Commit != moved || after.Digest == before.Digest {
		t.Fatalf("after the branch moved the series is %+v, want commit %s under a new digest", after.Carry, moved)
	}
}

func TestACarryWithNoSharedHistoryIsTheChecksProblem(t *testing.T) {
	f := newCarryFixture(t)
	gitIn(t, f.carryRepo, "checkout", "-q", "--orphan", "lonely")
	commitFile(t, f.carryRepo, "f.txt", "alone\n")
	w := f.carryWant("main")
	w.Carry = "git+file://" + f.carryRepo + "?ref=lonely"
	res := f.check(t, w, true)
	if p := res.Record.Check.Problem; !strings.Contains(p, "shares no history") || !strings.Contains(p, "merge the upstream into it") {
		t.Errorf("the check's problem = %q, want the unrelated histories named with the next step", p)
	}
}

// PF-D88 FOR THE CARRY: a carry repository that cannot be fetched is the check's failed fetch, so an
// advance with nothing to run checks again at once rather than an hour later; the copy the mirror
// already holds keeps answering.
func TestACarryThatCannotBeFetchedIsAFailedFetch(t *testing.T) {
	f := newCarryFixture(t)
	w := f.carryWant("main")
	w.Carry = "git+file://" + filepath.Join(t.TempDir(), "gone") + "?ref=carry"
	res := f.check(t, w, true)
	c := res.Record.Check
	if c.FetchErr == "" || !strings.Contains(c.FetchErr, "fetching the carry") || !strings.Contains(c.Problem, "has never fetched the carry") {
		t.Fatalf("a carry never fetched = fetch %q, problem %q", c.FetchErr, c.Problem)
	}
	f.now = f.now.Add(time.Minute)
	retry := f.store.CheckPatched(w, CheckOptions{RetryFailedFetch: true, Now: func() time.Time { return f.now }})
	if !retry.Ran {
		t.Error("with nothing to run, the failed carry fetch held the next check for the hour")
	}

	// Once fetched, a carry repository gone missing still answers from the mirror's copy.
	good := f.carryWant("main")
	if _, res := f.checkedCarry(t); res.Record.Carry == nil {
		t.Fatal("the carry did not resolve")
	}
	if err := os.Rename(f.carryRepo, f.carryRepo+".moved"); err != nil {
		t.Fatal(err)
	}
	res = f.check(t, good, true)
	if res.Record.Check.FetchErr == "" || res.Record.Check.Problem != "" || res.Record.Check.Carry == nil {
		t.Errorf("a failed fetch of a fetched carry = %+v, want the mirror's copy to answer", res.Record.Check)
	}
}

// AN OLD GIT is refused by the replay's one error, in the carry's words, and never merged otherwise.
func TestAnOldGitRefusesTheCarryByName(t *testing.T) {
	f := newCarryFixture(t)
	f.release(t, "v1.1.0", map[int]string{14: "fourteen"})
	series, res := f.checkedCarry(t)
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
	if !errors.As(w.Err, &tooOld) || !strings.Contains(tooOld.Error(), "merging a carry needs git 2.40 or newer (`git merge-tree --write-tree`)") ||
		!strings.Contains(tooOld.Error(), "2.37.1") || len(w.Results) != 0 {
		t.Fatalf("under git 2.37.1 the walk = %+v (err %v), want the carry refused by name", w, w.Err)
	}
}
