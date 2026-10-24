package packsrc

// localclones.go is the LOCAL CLONE MAP (a term coined here; docs/reference/patched-forks.md §3.5, PF-D100):
// the git clones on this machine the user's config reaches, each keyed by the URLs of its remotes, so a patch
// branch a pack names by its PUBLISHED URL is resolved against the user's own clone of that repository with
// the newest-copy-wins rule of a local checkout (patchbranchcopy.go, PF-D99).
//
// The maintainer's rulings (2026-10-24): a pack file always names published URLs; "if you have a path into
// any local clone of a repository, that adds that repository to the mapping, and because of the newest wins,
// this is safe even for other packs"; and "we can just also have a user level config to register them
// regardless" — `local_clones`.
//
//   - WHO REGISTERS. config (localclones.go there) gathers the map from the USER config: every path a pack is
//     read from that lands inside a git clone (a pack named by path or by git+file://, the conventional local
//     pack), and every `local_clones` entry. A plain-files pack in no repository registers nothing.
//   - WHEN IT APPLIES. A patch branch whose repository is a URL (not a directory on this machine) that names
//     the same repository as a remote of a registered clone is resolved as that clone's local checkout, with
//     that remote compared against. Nothing else changes: the record still keys the resolution by the
//     locator as written, so a reader with no map (a jail) reads the same record.
//   - WHICH CLONE. An explicit `local_clones` entry before an implicit one, then config order (the order
//     LocalCloneSource returns them in). The provenance line names the clone it used and why.
//   - READ ONLY. Gathering runs `git rev-parse --show-toplevel` and reads the clone's remote URLs from its
//     configuration; nothing is ever written to the clone.

import (
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// LocalClone is one clone the local clone map holds.
type LocalClone struct {
	// Dir is the clone's top level, as `git rev-parse --show-toplevel` names it.
	Dir string
	// Explicit is true for a clone a `local_clones` entry registered, false for one a pack's path did.
	Explicit bool
	// From is the config entry that registered it, as written: the `local_clones` path, or the pack's name.
	From string
	// Remotes are the clone's remotes, by name and URL as its configuration writes them.
	Remotes []CloneRemote
}

// CloneRemote is one remote of a LocalClone.
type CloneRemote struct{ Name, URL string }

// Via is how the clone was registered, as a line says it.
func (c LocalClone) Via() string {
	if c.Explicit {
		return "local_clones entry " + c.From
	}
	return "the path of pack " + c.From
}

var (
	localCloneMu     sync.Mutex
	localCloneSource func() []LocalClone
)

// SetLocalCloneSource installs the function that gathers the local clone map, which config installs (it
// reads the user config, which this package does not), and returns the one it replaced. Nil means no map.
// The source is asked at each resolution that could use it, so a test's or a verb's change of config is seen.
func SetLocalCloneSource(f func() []LocalClone) func() []LocalClone {
	localCloneMu.Lock()
	defer localCloneMu.Unlock()
	old := localCloneSource
	localCloneSource = f
	return old
}

func localClones() []LocalClone {
	localCloneMu.Lock()
	f := localCloneSource
	localCloneMu.Unlock()
	if f == nil {
		return nil
	}
	return f()
}

// RepoIdentity is the identity a repository URL is matched by: gitproto.Identity for ssh://, https:// and
// scp-style spellings (host lower-cased, user, a trailing / and .git dropped), so
// git@github.com:o/r.git, ssh://git@github.com/o/r and https://github.com/o/r are one repository; for a
// file:// URL or a local path, "file:" and the cleaned absolute path. "" for anything else.
func RepoIdentity(raw string) string {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "git+")
	if raw == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(raw, "file://"); ok {
		if u, err := url.Parse(raw); err == nil && u.Path != "" {
			rest = u.Path
		}
		return localIdentity(rest)
	}
	if filepath.IsAbs(raw) {
		return localIdentity(raw)
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") {
		raw = "https://" + raw[len("http://"):]
	}
	id, err := gitproto.Identity(raw)
	if err != nil {
		return ""
	}
	return id
}

func localIdentity(p string) string {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return "file:" + strings.TrimSuffix(strings.TrimSuffix(p, "/"), "/.git")
}

// CloneMatch is the registered clone a patch branch URL resolved to, and every other clone that matched.
type CloneMatch struct {
	Clone  LocalClone
	Remote CloneRemote
	// Others are the further matching clones' directories, in the order they lost.
	Others []string
}

// matchLocalClone is the registered clone whose remote names repo, nil for none: an explicit clone first,
// then the map's order.
func matchLocalClone(repo string, clones []LocalClone) *CloneMatch {
	want := RepoIdentity(repo)
	if want == "" {
		return nil
	}
	var m *CloneMatch
	for _, explicit := range []bool{true, false} {
		for _, c := range clones {
			if c.Explicit != explicit {
				continue
			}
			for _, r := range c.Remotes {
				if RepoIdentity(r.URL) != want {
					continue
				}
				if m == nil {
					m = &CloneMatch{Clone: c, Remote: r}
				} else if c.Dir != m.Clone.Dir {
					m.Others = append(m.Others, c.Dir)
				}
				break
			}
		}
	}
	return m
}

// cloneFor is the registered clone a patch branch at c resolves against, nil when c is no URL or no clone
// matches. A repository that is a directory on this machine is a checkout already (checkoutPlanOf).
func cloneFor(c Addr) *CloneMatch {
	if c.Repo == "" || strings.HasPrefix(c.Repo, "file://") {
		return nil
	}
	return matchLocalClone(c.Repo, localClones())
}

// patchBranchPlan is the address a check fetches and resolves a patch branch at, and its checkout plan: a
// checkout's own (checkoutPlanOf), or, for a URL a registered clone's remote names, that clone's, with that
// remote as the one compared against. The returned address keeps c.Raw, which keys the record.
func (s *Store) patchBranchPlan(c Addr) (Addr, *checkoutPlan) {
	m := cloneFor(c)
	if m == nil {
		return c, s.checkoutPlanOf(c)
	}
	local := Addr{Kind: KindGit, Raw: c.Raw, Repo: "file://" + filepath.ToSlash(m.Clone.Dir), Ref: c.Ref}
	p := s.checkoutPlanOf(local)
	if p == nil {
		return c, s.checkoutPlanOf(c)
	}
	p.remote = m.Remote.Name
	if u, err := s.run("", "-C", m.Clone.Dir, "remote", "get-url", "--", m.Remote.Name); err == nil && strings.TrimSpace(u) != "" {
		p.url = strings.TrimSpace(u)
	} else {
		p.url = m.Remote.URL
	}
	if !strings.Contains(p.url, "://") && !scpLike(p.url) && !filepath.IsAbs(p.url) {
		p.url = filepath.Join(m.Clone.Dir, p.url)
	}
	p.clone = m
	return local, p
}
