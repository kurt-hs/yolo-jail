package brokerscope

import (
	"fmt"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// grant.go is a scope file's user grants (docs/design/git-broker-plan.md ID-10, BB-D96): the
// specific grants a workspace config names, each of a kind the source defines and the user's own
// config allows, as the config-change gate approved them. The file carries them for the broker
// to read at each write; the scope file is still written only from a gate's approved result.

// Grant is one user grant: its kind (`create-branch`, `branch`) and the repository it covers,
// a repository identity (`github.com/o/r`, gitproto.Identity) or AnyRepo. A kind that names a
// branch (GrantTakesBranch) carries it in Branch, and no other kind has one.
type Grant struct {
	Kind string `json:"kind"`
	Repo string `json:"repo"`
	// Branch is a `branch` grant's branch, one branch's short name (`main`, meaning
	// refs/heads/main): a working-set branch declared in config (docs/design/git-broker.md
	// GB-D12, GB-D13).
	Branch string `json:"branch,omitempty"`
}

// AnyRepo is a grant's written-out wildcard: every repository the scope holds.
const AnyRepo = "*"

// KindBranch is the grant kind that names a branch: that branch is in the workspace's working
// set, declared in config rather than recorded in the store (docs/design/git-broker.md GB-D12).
const KindBranch = "branch"

// KindRead is the grant kind every brokered source defines without declaring it: reading the
// repositories of the approved scope (docs/design/boundary-broker.md BB-D105, BB-D107). A broker
// whose live scope file holds no read grant covering a repository refuses every call on it.
const KindRead = "read"

// ReadCovers reports whether grants hold a read grant on repo, or on AnyRepo. same compares a
// grant's repository identity with repo, which each source spells its own way (git a remote's
// identity, gh `OWNER/REPO`).
func ReadCovers(grants []Grant, repo string, same func(grantRepo, repo string) bool) bool {
	for _, g := range grants {
		if g.Kind == KindRead && (g.Repo == AnyRepo || same(g.Repo, repo)) {
			return true
		}
	}
	return false
}

// HoldsRead reports whether grants hold any read grant.
func HoldsRead(grants []Grant) bool {
	for _, g := range grants {
		if g.Kind == KindRead {
			return true
		}
	}
	return false
}

// GrantTakesBranch reports whether a grant of kind names a branch.
func GrantTakesBranch(kind string) bool { return kind == KindBranch }

// String is the grant as the gate's block and the approval record spell it: `<kind> <repo>`,
// then ` <branch>` for a kind that names one. No part can hold a space (ValidGrant), so the
// spelling parses back (ParseGrant).
func (g Grant) String() string {
	if g.Branch != "" {
		return g.Kind + " " + g.Repo + " " + g.Branch
	}
	return g.Kind + " " + g.Repo
}

// ParseGrant reads a String spelling back, ok false for anything ValidGrant refuses.
func ParseGrant(s string) (Grant, bool) {
	kind, rest, found := strings.Cut(s, " ")
	repo, branch, _ := strings.Cut(rest, " ")
	g := Grant{Kind: kind, Repo: repo, Branch: branch}
	return g, found && ValidGrant(g)
}

// ValidGrant reports whether g is well formed: a grant-kind name, a repository the grant can
// name, and a branch exactly when the kind names one. It says nothing of whether a source
// defines the kind or the user allows it.
func ValidGrant(g Grant) bool {
	if !validGrantKind(g.Kind) || !ValidGrantRepo(g.Repo) {
		return false
	}
	if GrantTakesBranch(g.Kind) {
		// A branch grant names one repository (git-broker.md GB-D13): "*" would put the branch
		// on every approved remote in the working set.
		return g.Repo != AnyRepo && ValidGrantBranch(g.Branch)
	}
	return g.Branch == ""
}

// ValidGrantBranch reports whether s is a branch a `branch` grant can name: one branch's short
// name that git accepts as refs/heads/<s> (git check-ref-format's rules), that does not start
// with `-` and is not `HEAD` (which `git branch` refuses too), and that is not itself written as
// `refs/…` (the short name is what the grant means). There is no wildcard, and tags and other
// refs are never nameable: the grant is under refs/heads/ by construction.
func ValidGrantBranch(s string) bool {
	if s == "" || s == "HEAD" || s == "@" || s[0] == '-' || strings.HasPrefix(s, "refs/") {
		return false
	}
	if strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, c := range []byte(s) {
		if c < 0x20 || c == 0x7f || strings.IndexByte(" ~^:?*[\\", c) >= 0 {
			return false
		}
	}
	for _, comp := range strings.Split(s, "/") {
		if comp == "" || comp[0] == '.' || strings.HasSuffix(comp, ".lock") {
			return false
		}
	}
	return true
}

// validGrantKind is loopholedecl.ValidGrantKind, restated so this package does not import the
// manifest decoder: a lowercase letter, then lowercase letters, digits and hyphens.
func validGrantKind(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// ValidGrantRepo reports whether s is a repository a grant can name: AnyRepo, or a repository
// identity already in its reduced form (gitproto.Identity) whose host has a dot or a port. The
// host rule is what tells `github.com/o/r` from an `OWNER/REPO` written without one: a bare
// single-label host such as `gitserver` cannot be named, a limit stated in the refusal.
func ValidGrantRepo(s string) bool {
	if s == AnyRepo {
		return true
	}
	id, err := gitproto.Identity("https://" + s)
	if err != nil || id != s {
		return false
	}
	host, _, _ := strings.Cut(s, "/")
	return strings.ContainsAny(host, ".:")
}

// checkGrants is the error for the first grant ValidGrant refuses, nil for none.
func checkGrants(grants []Grant) error {
	for _, g := range grants {
		if !ValidGrant(g) {
			return fmt.Errorf("scope file: grant %q is not a grant kind, a repository identity and, for a branch grant, a branch", g.String())
		}
	}
	return nil
}
