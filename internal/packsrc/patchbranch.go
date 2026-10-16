package packsrc

// patchbranch.go is a PATCH BRANCH (docs/design/patched-forks.md §3.5, OQ-PFK6): a commit graph a patched fork
// or extension names in place of a `patches` series — a repository and a branch whose ancestry
// includes the upstream's — merged against each upstream candidate by `git merge-tree
// --write-tree` instead of replayed.
//
// ONE MACHINERY WITH THE SERIES (PF-D89). A patch branch is a Series whose members are not files but one
// commit, so the check, its record and throttle, the walk, the ratchet, the store key and every
// explicit act read it through the same types:
//
//   - THE CHECK FETCHES IT. Whenever a check runs (its own throttle, an explicit act, or a retry of
//     a failed fetch with nothing to run, PF-D88), after the upstream's fetch step and in the same
//     budget, the patch branch's repository is fetched INTO ITS OWN MIRROR by the per-mirror fetch step a
//     pack's repository takes (blobless, fsck'd, its tags put back), under both mirrors' locks taken
//     in one order (lockMirrors). A blobless mirror cannot take another repository's objects: a
//     fetch from a remote that is not its promisor, under transfer.fsckObjects, refuses every tree
//     naming a blob it lacks (MEASURED, git 2.55: "did not receive expected object"). The replay's
//     scratch repository borrows both mirrors' objects instead (newScratchOf). A fetch that fails is
//     the check's failed fetch, classified by TransportFailure, and the copy the patch branch's mirror
//     already holds keeps answering, as an upstream's does.
//   - THE CHECK RESOLVES IT: the branch to a commit, and the commit's MERGE BASE with the upstream
//     ref's tip, which is the patch branch's base — the newest upstream commit it already contains. The
//     walk's list holds only versions containing it, and the first advance falls back to it, where
//     the merge is the patch branch's own tree. A patch branch that shares no history with the upstream names no
//     base: the check's problem, never a conflict.
//   - THE RECORD KEEPS IT (CheckRecord.Patch branch), apart from the last check's findings, so a later
//     check with a problem does not lose what the good build is keyed by; every reader rebuilds
//     the series from it with no git (PatchBranchSeries), so a launch inside the hour runs no git (P4).
//   - THE DIGEST is PatchBranchDigest(commit), which takes the series digest's place in the recipe: a
//     build is keyed on the upstream commit and the patch branch's commit, so a moved branch is a new
//     build and two machines that resolve one commit build the same bytes.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// SeriesPatchBranch is the patch branch a Series stands for.
type SeriesPatchBranch struct {
	// Source is the patch branch's address as the manifest writes it; Repo and Ref are its parts.
	Source, Repo, Ref string
	// Commit is the commit the branch resolved to at the last check that fetched it.
	Commit string
}

// Label is the patch branch as a line names it: "<ref>@<short commit>".
func (c *SeriesPatchBranch) Label() string { return c.Ref + "@" + shortCommit(c.Commit) }

// PatchBranchResolved is what a check resolved a patch branch to, kept in the check record.
type PatchBranchResolved struct {
	// Source is the patch branch as the manifest wrote it, and Upstream the upstream repository it was
	// resolved against: a record whose either differs from what is asked names nothing.
	Source   string `json:"source"`
	Upstream string `json:"upstream"`
	// Commit is the branch's commit; Base its merge base with the upstream ref's tip.
	Commit string `json:"commit"`
	Base   string `json:"base"`
	// At is when it was resolved, in unix seconds.
	At int64 `json:"at,omitempty"`
}

// PatchBranchDigest is a patch branch's SERIES DIGEST: the sha256, in hex, of the JSON array ["patch_branch", commit].
// The tag means no patch branch's digest can equal a patch series', whose array holds name pairs.
func PatchBranchDigest(commit string) string {
	canonical, _ := json.Marshal([]any{"patch_branch", commit})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// PatchBranchUnresolvedError is a patch branch no check has resolved on this machine yet: the next check fetches
// it. Readers that need the commit say so with its next step.
type PatchBranchUnresolvedError struct{ Source string }

func (e *PatchBranchUnresolvedError) Error() string {
	return "the patch branch " + e.Source + " has not been fetched on this machine yet — the next launch's check " +
		"fetches it, or `yolo pack update` fetches it now"
}

// UnresolvedPatchBranchSeries is the series a patch branch stands for before any check resolved it: no commit,
// no base, and a digest no build carries. An advance starts from it and takes the resolved one once
// its check has run (PatchBranchSeries); no walk runs on it.
func UnresolvedPatchBranchSeries(source string) *Series {
	a, _ := Parse(source)
	return &Series{PatchBranch: &SeriesPatchBranch{Source: source, Repo: a.Repo, Ref: a.Ref}, Digest: PatchBranchDigest("")}
}

// PatchBranchSeries is the series the patch branch source stands for by rec, the check record of an owner whose
// upstream is the source upstream: the commit and base the last check that resolved it found.
// PatchBranchUnresolvedError when rec names no resolution of this patch branch against this upstream.
func PatchBranchSeries(source, upstream string, rec *CheckRecord) (*Series, error) {
	if rec == nil || rec.PatchBranch == nil || rec.PatchBranch.Source != source || rec.PatchBranch.Upstream != CheckRepo(upstream) ||
		rec.PatchBranch.Commit == "" {
		return nil, &PatchBranchUnresolvedError{Source: source}
	}
	s := UnresolvedPatchBranchSeries(source)
	s.PatchBranch.Commit, s.Base, s.Digest = rec.PatchBranch.Commit, rec.PatchBranch.Base, PatchBranchDigest(rec.PatchBranch.Commit)
	return s, nil
}

// ReadPatchBranch is PatchBranchSeries from the owner's check record in this store: a file read, no git.
func (s *Store) ReadPatchBranch(owner, source, upstream string) (*Series, error) {
	rec, err := s.LoadCheckRecord(owner)
	if err != nil {
		if errors.Is(err, ErrNoCheckRecord) {
			return nil, &PatchBranchUnresolvedError{Source: source}
		}
		return nil, &PatchBranchRecordError{Source: source, Err: err}
	}
	return PatchBranchSeries(source, upstream, rec)
}

// PatchBranchRecordError is a check record that holds a patch branch's commit and cannot be read:
// not "not fetched yet", since a check did resolve it. A check starts the record over (§6.2), so an
// act that checks reads it as unresolved and goes on; every other reader says it.
type PatchBranchRecordError struct {
	Source string
	Err    error
}

func (e *PatchBranchRecordError) Error() string {
	return "the check record holding the commit of the patch branch " + e.Source + " cannot be read (" +
		oneLine(e.Err) + ") — `yolo pack update` checks it again and starts the record over"
}

func (e *PatchBranchRecordError) Unwrap() error { return e.Err }

// IsPatchBranchRecordError reports whether err is a PatchBranchRecordError.
func IsPatchBranchRecordError(err error) bool {
	var r *PatchBranchRecordError
	return errors.As(err, &r)
}

// localRetry is the next step after a git that could not answer about a mirror this machine holds (a
// timeout, a lock, a busy disk): the next check asks again, and `yolo pack update` asks now.
const localRetry = " — the next check asks again, or `yolo pack update` now"

// lockMirrors takes the mirror locks of repos, each once, in one order (sorted by repository), so
// two processes taking an upstream's and a patch branch's never wait on each other the other way round.
// Its release drops them all.
func (s *Store) lockMirrors(repos []string, waiting func(string)) (func(), error) {
	sorted := slices.Clone(repos)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	var unlocks []func()
	release := func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
	for _, r := range sorted {
		unlock, err := s.lockMirror(r, waiting)
		if err != nil {
			release()
			return nil, err
		}
		unlocks = append(unlocks, unlock)
	}
	return release, nil
}

// mirrorsOf is the repositories a check or a walk holds the mirror locks of: the upstream's, and the
// patch branch's when there is one.
func mirrorsOf(upstream string, patchBranch *Addr) []string {
	if patchBranch == nil {
		return []string{upstream}
	}
	return []string{upstream, patchBranch.Repo}
}

// resolvePatchBranch is the check's patch branch step (findCandidates), under both mirrors' locks and the
// check's budget: the per-mirror fetch step for the patch branch's own repository, always (the check is
// the throttle), then its ref read in its mirror and the commit's merge base with the upstream's
// tip, asked in a scratch repository that borrows both mirrors' objects. It returns the resolution,
// or the check's problem, and a fetch that failed (one line), which the caller records as the
// check's failed fetch: the copy the patch branch's mirror holds answers meanwhile.
func (s *Store) resolvePatchBranch(b budget, c Addr, upstream, tip string, now time.Time) (*PatchBranchResolved, string, string) {
	step := s.fetchStep(b, c.Repo, []Addr{c}, fetchMode{always: true, keepTags: true}, now, BranchRefreshInterval)
	fetchErr := ""
	if step.fetchErr != nil {
		fetchErr = "fetching the patch branch " + c.Raw + ": " + oneLine(step.fetchErr)
	}
	mirror := s.mirrorPath(c.Repo)
	if !mirrorExists(mirror) {
		return nil, "this machine has never fetched the patch branch " + c.Raw + ", and " + fetchFailed(fetchErr), fetchErr
	}
	kind, _, commit, err := s.classifyRef(mirror, c.Ref)
	switch {
	case err != nil:
		return nil, "could not read the patch branch " + c.Raw + ": " + oneLine(err) + localRetry, fetchErr
	case kind == refUnresolved && fetchErr != "":
		return nil, "the patch branch's ?ref=" + c.Ref + " is not in this machine's copy of " + c.Repo + ", and " +
			fetchFailed(fetchErr), fetchErr
	case kind == refUnresolved:
		return nil, "the patch branch's ?ref=" + c.Ref + " names no branch, tag or commit of " + c.Repo +
			" — check the patch branch's ?ref=", ""
	case kind == refOther:
		return nil, "the patch branch's ?ref=" + c.Ref + " names neither a branch, a tag nor a full commit of " + c.Repo +
			" — name the branch", ""
	}
	sc, err := newScratchOf(s.git(), b, s.mirrorPath(upstream), mirror)
	if err != nil {
		return nil, "could not ask for the patch branch's merge base: " + oneLine(err) + localRetry, fetchErr
	}
	defer sc.remove()
	out, code, err := sc.gitStatus("", "merge-base", commit, tip)
	switch {
	case code == 1:
		return nil, "the patch branch " + c.Raw + " (" + shortCommit(commit) + ") shares no history with " + upstream +
			" — git refuses to merge unrelated histories; a patch branch is a branch whose history includes the " +
			"upstream's: start it from an upstream commit and merge the upstream into it", fetchErr
	case err != nil:
		return nil, "could not ask for the patch branch's merge base with " + upstream + ": " + oneLine(err) + localRetry, fetchErr
	}
	return &PatchBranchResolved{Source: c.Raw, Upstream: upstream, Commit: commit, Base: strings.TrimSpace(out)}, "", fetchErr
}
