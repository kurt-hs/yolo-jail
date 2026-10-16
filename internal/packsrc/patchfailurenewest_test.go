package packsrc

// patchfailurenewest_test.go pins CurrentPatchFailure's two recovery rules
// (docs/reference/patched-forks.md): A RECORDED FAILURE FOLLOWS UPSTREAM'S NEWEST (§6.2) — it
// governs only while its target is the newest candidate no recorded conflict passes over — and THE
// WALK-BACK HOLD (§6.4), under which the newest's failure is the hold's and does not govern.

import (
	"strings"
	"testing"
)

func newestFixture(outcomes []EntryOutcome, good *GoodBuild, check *CheckFound) (*CheckRecord, CheckInputs) {
	in := CheckInputs{Repo: "example/repo", Ref: "main", Follow: "release", Base: "base"}
	if check == nil {
		check = &CheckFound{Seq: 3, RefKind: "branch", List: []ListEntry{
			{Commit: "c15", Tag: "v1.5.0", Version: "1.5.0"}, {Commit: "c14", Tag: "v1.4.0", Version: "1.4.0"},
			{Commit: "c11", Tag: "v1.1.0", Version: "1.1.0"}}}
	}
	return &CheckRecord{Owner: "forkpack/tool", Seq: 3, Read: in, Check: check, Good: good, Outcomes: outcomes,
		PatchFailure: &PatchFailure{Owner: "forkpack/tool", Inputs: in, Series: "s", Kind: "conflict", Seq: 3,
			Target: ListEntry{Commit: "c14", Tag: "v1.4.0", Version: "1.4.0"}, Member: "0001.patch"}}, in
}

func TestARecordedFailureFollowsTheNewestCandidate(t *testing.T) {
	good := &GoodBuild{Commit: "c11", Tag: "v1.1.0", Version: "1.1.0", Series: "s"}
	conflict := func(commit string) EntryOutcome {
		return EntryOutcome{Commit: commit, Kind: OutcomeConflict, Series: "s", Yolo: "y"}
	}
	for _, tc := range []struct {
		name     string
		outcomes []EntryOutcome
		good     *GoodBuild
		check    *CheckFound
		current  bool
	}{
		{name: "a newer release untried passes it by", outcomes: []EntryOutcome{conflict("c14")}, good: good},
		{name: "a newer release that also conflicts does not", outcomes: []EntryOutcome{conflict("c15"), conflict("c14")},
			good: good, current: true},
		{name: "a newer conflict of another series does not count", good: good, current: false,
			outcomes: []EntryOutcome{{Commit: "c15", Kind: OutcomeConflict, Series: "other"}}},
		{name: "a good build of this series past it", good: &GoodBuild{Commit: "c15", Tag: "v1.5.0", Version: "1.5.0", Series: "s"}},
		{name: "a good build of another series past it keeps it", current: true,
			good: &GoodBuild{Commit: "c15", Tag: "v1.5.0", Version: "1.5.0", Series: "old"}},
		{name: "a failed check keeps it", good: good, current: true,
			check: &CheckFound{Seq: 3, FetchErr: "offline", List: []ListEntry{{Commit: "c15", Tag: "v1.5.0"}, {Commit: "c14", Tag: "v1.4.0"}}}},
		{name: "the newest itself", good: good, current: true,
			check: &CheckFound{Seq: 3, RefKind: "branch", List: []ListEntry{{Commit: "c14", Tag: "v1.4.0", Version: "1.4.0"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, in := newestFixture(tc.outcomes, tc.good, tc.check)
			if got := r.CurrentPatchFailure(in, "s") != nil; got != tc.current {
				t.Fatalf("current = %v, want %v", got, tc.current)
			}
		})
	}
}

func TestABaseFailureIsNeverPassedBy(t *testing.T) {
	r, in := newestFixture(nil, nil, nil)
	r.PatchFailure.Kind, r.PatchFailure.Target = "base", ListEntry{Commit: "base"}
	if r.CurrentPatchFailure(in, "s") == nil {
		t.Fatal("a failure of the series at its own base was passed by")
	}
}

func TestAWalkBackHoldCoversTheNewestsFailure(t *testing.T) {
	check := &CheckFound{Seq: 3, RefKind: "branch", List: []ListEntry{{Commit: "c14", Tag: "v1.4.0", Version: "1.4.0"},
		{Commit: "c11", Tag: "v1.1.0", Version: "1.1.0"}}}
	r, in := newestFixture([]EntryOutcome{{Commit: "c14", Kind: OutcomeConflict, Series: "s"}},
		&GoodBuild{Commit: "c11", Tag: "v1.1.0", Version: "1.1.0", Series: "s", Recipe: "rx"}, check)
	r.WalkBack = &WalkBackHold{Newest: check.List[0], Kind: OutcomeConflict, Good: "c11", Series: "s"}
	if r.CurrentPatchFailure(in, "s") != nil {
		t.Fatal("the walk-back hold did not cover the newest's failure")
	}
	h := r.HeldAt(in, "s", "rx")
	if h == nil || h.Kind != HeldByWalkBack || !strings.Contains(h.Error, "upstream v1.4.0 does not take the series") {
		t.Fatalf("HeldAt = %+v", h)
	}
	// A NEWER ENTRY ENDS THE HOLD, and the failure governs again by the newest rule.
	r.Check.List = append([]ListEntry{{Commit: "c15", Tag: "v1.5.0", Version: "1.5.0"}}, r.Check.List...)
	r.Outcomes = append(r.Outcomes, EntryOutcome{Commit: "c15", Kind: OutcomeConflict, Series: "s"})
	if r.WalkBackHolds(in, "s") != nil || r.CurrentPatchFailure(in, "s") == nil {
		t.Fatal("a newer release did not end the walk-back hold")
	}
}
