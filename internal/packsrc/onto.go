package packsrc

import (
	"fmt"
	"strings"
	"time"
)

// ScratchTimeout bounds a scratch act's network work and replay (`yolo pack series check`, which
// fetches the upstream into a scratch mirror that starts empty): longer than a launch's fetch bound,
// since a blobless clone of a large upstream is the whole history's commits and trees, and the act
// runs at the user's terminal, where a Ctrl-C ends it.
const ScratchTimeout = 10 * time.Minute

// ResolveOnto resolves ref, as `yolo pack series check --onto` names it, to a commit in repo's
// mirror, with no network: a tag (git's precedence, as classifyRef reads a ref), a branch, or any
// other revision git names a commit by, an abbreviated commit included. The entry carries the tag's
// short name for a tag, so a line names it as the walk names a version.
func (s *Store) ResolveOnto(repo, ref string, waiting func(string)) (ListEntry, error) {
	if strings.HasPrefix(ref, "-") {
		return ListEntry{}, fmt.Errorf("--onto %s is not a revision", ref)
	}
	unlock, err := s.lockMirror(repo, waiting)
	if err != nil {
		return ListEntry{}, err
	}
	defer unlock()
	mirror := s.mirrorPath(repo)
	if !mirrorExists(mirror) {
		return ListEntry{}, fmt.Errorf("this machine holds no copy of %s — a check fetches it", repo)
	}
	kind, name, sha, err := s.classifyRef(mirror, ref)
	switch {
	case err != nil:
		return ListEntry{}, err
	case kind == refUnresolved:
		return ListEntry{}, fmt.Errorf("--onto %s names no branch, tag or commit of %s", ref, repo)
	case kind == refTag:
		return ListEntry{Commit: sha, Tag: strings.TrimPrefix(name, "refs/tags/")}, nil
	}
	return ListEntry{Commit: sha}, nil
}
