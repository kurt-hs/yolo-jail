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

// Grant is one user grant: its kind (`create-branch`) and the repository it covers, a repository
// identity (`github.com/o/r`, gitproto.Identity) or AnyRepo.
type Grant struct {
	Kind string `json:"kind"`
	Repo string `json:"repo"`
}

// AnyRepo is a grant's written-out wildcard: every repository the scope holds.
const AnyRepo = "*"

// String is the grant as the gate's block and the approval record spell it: `<kind> <repo>`.
// Neither half can hold a space (ValidGrant), so the spelling parses back (ParseGrant).
func (g Grant) String() string { return g.Kind + " " + g.Repo }

// ParseGrant reads a String spelling back, ok false for anything ValidGrant refuses.
func ParseGrant(s string) (Grant, bool) {
	kind, repo, found := strings.Cut(s, " ")
	g := Grant{Kind: kind, Repo: repo}
	return g, found && ValidGrant(g)
}

// ValidGrant reports whether g is well formed: a grant-kind name and a repository the grant can
// name. It says nothing of whether a source defines the kind or the user allows it.
func ValidGrant(g Grant) bool { return validGrantKind(g.Kind) && ValidGrantRepo(g.Repo) }

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
			return fmt.Errorf("scope file: grant %q is not a grant kind and a repository identity", g.String())
		}
	}
	return nil
}
