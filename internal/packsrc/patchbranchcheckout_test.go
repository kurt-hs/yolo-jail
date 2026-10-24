package packsrc

// patchbranchcheckout_test.go pins a PATCH BRANCH GIVEN AS A LOCAL CHECKOUT (a relative or git+file://
// locator; PF-D-ruling of 2026-10-24, docs/reference/patched-forks.md §3.5): yolo fetches the checkout's
// remote into its own mirror — never into the user's repository — compares the checkout's local branch with
// the fetched remote tip, builds from whichever copy is newest, and says which copy answered and how it
// relates to the other, with the fetch's freshness. A failed fetch never refuses.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// checkoutFixture is the upstream, a bare "origin" holding branch mine (pushed by "pusher", a second
// clone), and "machine", a clone of origin that declares the patch branch.
type checkoutFixture struct {
	*patchedUpstream
	origin, pusher, machine string
}

func newCheckoutFixture(t *testing.T) *checkoutFixture {
	t.Helper()
	u := newPatchedUpstream(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &checkoutFixture{patchedUpstream: u, origin: filepath.Join(root, "origin.git"),
		pusher: filepath.Join(root, "pusher"), machine: filepath.Join(root, "machine")}
	gitIn(t, root, "clone", "-q", "--bare", u.repo, f.origin)
	gitIn(t, f.origin, "config", "uploadpack.allowFilter", "true")
	gitIn(t, root, "clone", "-q", f.origin, f.pusher)
	gitIn(t, f.pusher, "checkout", "-q", "-b", "mine", u.base)
	commitFile(t, f.pusher, "f.txt", thirtyLines(map[int]string{10: "ten"}))
	gitIn(t, f.pusher, "push", "-q", "origin", "mine")
	gitIn(t, root, "clone", "-q", f.origin, f.machine)
	return f
}

func (f *checkoutFixture) want() PatchedWant {
	return PatchedWant{Owner: "forkpack/tool", Source: f.source("main"), PatchBranch: "git+file://" + f.machine + "?ref=mine"}
}

// push commits on the pusher's mine and pushes it, returning the new tip.
func (f *checkoutFixture) push(t *testing.T, edits map[int]string) string {
	t.Helper()
	c := commitFile(t, f.pusher, "f.txt", thirtyLines(edits))
	gitIn(t, f.pusher, "push", "-q", "origin", "mine")
	return c
}

// localMine makes the machine's local branch mine at start and commits n more on it, returning its tip.
func (f *checkoutFixture) localMine(t *testing.T, start string, n int) string {
	t.Helper()
	gitIn(t, f.machine, "checkout", "-q", "-b", "mine", start)
	tip := gitIn(t, f.machine, "rev-parse", "HEAD")
	for i := 0; i < n; i++ {
		tip = commitFile(t, f.machine, "local.txt", strings.Repeat("x", i+1)+"\n")
	}
	gitIn(t, f.machine, "checkout", "-q", "main")
	return tip
}

// launch runs the check a fresh launch runs (RefreshPatchBranch, not forced) at the fixture's clock.
func (f *checkoutFixture) launch(t *testing.T) CheckResult {
	t.Helper()
	res := f.store.CheckPatched(f.want(), CheckOptions{RefreshPatchBranch: true, Now: func() time.Time { return f.now }})
	if res.Err != nil {
		t.Fatalf("CheckPatched: %v", res.Err)
	}
	if res.Record == nil || res.Record.Check == nil {
		t.Fatal("no check record")
	}
	if p := res.Record.Check.Problem; p != "" {
		t.Fatalf("check problem: %s", p)
	}
	if res.Record.PatchBranch == nil {
		t.Fatal("the check recorded no patch branch resolution")
	}
	return res
}

// userRefs is everything a launch must leave alone in the user's repository: its refs and FETCH_HEAD.
func userRefs(t *testing.T, repo string) string {
	t.Helper()
	refs := gitIn(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	head, _ := os.ReadFile(filepath.Join(repo, ".git", "FETCH_HEAD"))
	st, err := os.Stat(filepath.Join(repo, ".git", "FETCH_HEAD"))
	mtime := ""
	if err == nil {
		mtime = st.ModTime().String()
	}
	return refs + "\n--FETCH_HEAD " + mtime + "\n" + string(head)
}

func wantLine(t *testing.T, r *PatchBranchResolved, now time.Time, parts ...string) {
	t.Helper()
	line := r.Line(now)
	t.Logf("line: %s", line)
	for _, p := range parts {
		if !strings.Contains(line, p) {
			t.Errorf("the provenance line %q lacks %q", line, p)
		}
	}
}

func TestACheckoutWithNoLocalBranchBuildsTheRemoteTipAndPicksUpAPushAtTheNextLaunch(t *testing.T) {
	f := newCheckoutFixture(t)
	before := userRefs(t, f.machine)
	first := gitIn(t, f.pusher, "rev-parse", "mine")
	r := f.launch(t).Record.PatchBranch
	if r.Commit != first || r.Copy != "origin" || !r.Verified {
		t.Fatalf("resolved = %+v, want origin's %s, verified", r, first)
	}
	wantLine(t, r, f.now, "mine  origin "+shortCommit(first)+"  no local branch  fetched just now")

	pushed := f.push(t, map[int]string{10: "ten", 12: "twelve"})
	f.now = f.now.Add(2 * time.Minute) // inside the hourly throttle: only the patch branch refresh runs
	res := f.launch(t)
	if r := res.Record.PatchBranch; r.Commit != pushed || r.Copy != "origin" {
		t.Fatalf("after a push the next launch resolved %+v, want origin's new tip %s", r, pushed)
	}
	if after := userRefs(t, f.machine); after != before {
		t.Errorf("a launch changed the user's repository:\nbefore %s\nafter %s", before, after)
	}
}

func TestACheckoutEqualToOriginBuildsIt(t *testing.T) {
	f := newCheckoutFixture(t)
	tip := gitIn(t, f.pusher, "rev-parse", "mine")
	f.localMine(t, "origin/mine", 0)
	r := f.launch(t).Record.PatchBranch
	if r.Commit != tip || r.Copy != "local" {
		t.Fatalf("resolved = %+v, want %s from the local branch", r, tip)
	}
	wantLine(t, r, f.now, "mine  local "+shortCommit(tip)+"  equal to origin  fetched just now")
}

func TestAStaleLocalBranchLosesToOrigin(t *testing.T) {
	f := newCheckoutFixture(t)
	f.localMine(t, "origin/mine", 0)
	pushed := f.push(t, map[int]string{10: "ten", 12: "twelve"})
	before := userRefs(t, f.machine)
	r := f.launch(t).Record.PatchBranch
	if r.Commit != pushed || r.Copy != "origin" || r.Behind != 1 || r.Ahead != 0 {
		t.Fatalf("resolved = %+v, want origin's %s with the local branch 1 behind", r, pushed)
	}
	wantLine(t, r, f.now, "mine  origin "+shortCommit(pushed)+"  local branch is stale, 1 behind origin  fetched just now")
	if after := userRefs(t, f.machine); after != before {
		t.Errorf("a launch changed the user's repository:\nbefore %s\nafter %s", before, after)
	}
}

func TestUnpushedLocalCommitsWin(t *testing.T) {
	f := newCheckoutFixture(t)
	local := f.localMine(t, "origin/mine", 2)
	r := f.launch(t).Record.PatchBranch
	if r.Commit != local || r.Copy != "local" || r.Ahead != 2 {
		t.Fatalf("resolved = %+v, want the local %s, 2 ahead", r, local)
	}
	wantLine(t, r, f.now, "mine  local "+shortCommit(local)+"  2 unpushed  fetched just now")
}

func TestADivergedLocalBranchWinsWithTheWarningAndTheFix(t *testing.T) {
	f := newCheckoutFixture(t)
	local := f.localMine(t, "origin/mine", 1)
	f.push(t, map[int]string{10: "ten", 12: "twelve"})
	pushed := f.push(t, map[int]string{10: "ten", 12: "twelve", 14: "fourteen"})
	r := f.launch(t).Record.PatchBranch
	if r.Commit != local || r.Copy != "local" || r.Ahead != 1 || r.Behind != 2 || !r.Diverged() {
		t.Fatalf("resolved = %+v, want the local %s, diverged 1+2", r, local)
	}
	wantLine(t, r, f.now, "mine  local "+shortCommit(local), "diverged from origin",
		"local "+shortCommit(local)+" has 1 commit origin lacks", "origin "+shortCommit(pushed)+" has 2 the local branch lacks",
		"`git -C "+f.machine+" push --force-with-lease origin mine` (if you rebased)", "fetched just now")
}

func TestAFailedFetchBuildsTheBestCopyAndSaysNotVerified(t *testing.T) {
	f := newCheckoutFixture(t)
	tip := gitIn(t, f.pusher, "rev-parse", "mine")
	f.launch(t) // the mirror holds origin's tip from here on
	if err := os.Rename(f.origin, f.origin+".gone"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(3 * time.Hour)
	r := f.launch(t).Record.PatchBranch
	if r.Commit != tip || r.Copy != "origin" || r.Verified || r.FetchErr == "" {
		t.Fatalf("resolved = %+v, want the mirror's last fetched %s, not verified", r, tip)
	}
	wantLine(t, r, f.now, "mine  origin "+shortCommit(tip), "not verified", "last fetched 3 hours ago",
		"`git -C "+f.machine+" fetch origin mine`")

	// With a local branch, the local branch answers a failed fetch.
	local := f.localMine(t, tip, 1)
	f.now = f.now.Add(2 * time.Minute)
	r = f.launch(t).Record.PatchBranch
	if r.Commit != local || r.Copy != "local" || r.Verified {
		t.Fatalf("resolved = %+v, want the local %s, not verified", r, local)
	}
	wantLine(t, r, f.now, "mine  local "+shortCommit(local), "not verified")
}
