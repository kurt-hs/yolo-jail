package packsrc

// baselag.go is a series' BASE LAG (a term coined here): the version tags of the followed branch that
// are newer than the series' base, as the last finished check listed them. A series that still
// applies is otherwise silent about how old the base it was written against is, so `yolo pack status`
// and a launch's fork line name the lag (docs/design/patched-forks.md §7). Read from the check record
// alone: no git, no network.

// BaseLag is a series' base lag: Base is the base as a list entry (its tag when a version names it),
// Newer the versions above it oldest first, and Applies those of them a replay or the good build
// found to take the series as it stands.
type BaseLag struct {
	Base    ListEntry
	Newer   []ListEntry
	Applies []ListEntry
	// PatchBranch is set when the lag is a patch branch's (patchbranch.go): Base is its merge base with the upstream.
	PatchBranch bool
}

// BaseLag is the series' base lag by the last finished check of in, or nil: when that check read
// something else, followed no branch (a tag or commit ref holds, and lists no versions above a base),
// or listed no version newer than the base. series is the series' digest, which the outcomes are
// keyed by.
func (r *CheckRecord) BaseLag(in CheckInputs, series string) *BaseLag {
	base := in.Base
	if in.PatchBranch != "" && r.Check != nil && r.Check.PatchBranch != nil {
		// A PATCH BRANCH'S LAG is its merge base's: the newest upstream commit the branch already holds.
		base = r.Check.PatchBranch.Base
	}
	if !r.Answers(in) || r.Check.RefKind != "branch" || base == "" {
		return nil
	}
	lag := &BaseLag{Base: ListEntry{Commit: base}, PatchBranch: in.PatchBranch != ""}
	// The list is newest first, and holds only versions containing the base, the base's own tag
	// included: every other version entry is a descendant of it, so newer.
	for i := len(r.Check.List) - 1; i >= 0; i-- {
		e := r.Check.List[i]
		switch {
		case e.Commit == base:
			if e.Tag != "" {
				lag.Base = ListEntry{Commit: e.Commit, Tag: e.Tag, Version: e.Version}
			}
		case e.Tag != "":
			lag.Newer = append(lag.Newer, ListEntry{Commit: e.Commit, Tag: e.Tag, Version: e.Version})
			if r.appliesAt(e.Commit, series) {
				lag.Applies = append(lag.Applies, lag.Newer[len(lag.Newer)-1])
			}
		}
	}
	if len(lag.Newer) == 0 {
		return nil
	}
	return lag
}

// appliesAt reports whether the record knows the series takes commit as it stands: a clean replay's
// outcome there under any yolo or git version (it is informational, PF-D9), or the good build of
// this very series there.
func (r *CheckRecord) appliesAt(commit, series string) bool {
	if g := r.Good; g != nil && g.Commit == commit && g.Series == series {
		return true
	}
	for _, o := range r.Outcomes {
		if o.Kind == OutcomeApplies && o.Commit == commit && o.Series == series {
			return true
		}
	}
	return false
}
