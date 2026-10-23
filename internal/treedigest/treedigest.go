// Package treedigest answers "are these two directory trees the same?" by CONTENT, in one
// canonical form that more than one subsystem needs to agree on.
//
// It was `hostskills.treeDigest` until 2026-09-04, where it answered "is this the same
// SKILL?" — and it moved here unchanged because the installer-capture store
// (internal/capture, program-delivery.md §6.3) asks the same question of a captured
// install tree and must get the same answer from the same bytes. Two spellings of one
// canonical form is the drift that would make a capture key and a skill digest disagree
// about what "identical" means; hostskills' own tests are the gate that this lift changed
// nothing (they call the package-local wrappers, which now delegate here).
//
// THE CANONICAL FORM, exactly — one line per entry, walked depth-first with each
// directory's children sorted by name, relative to the root (whose own line is `d .`):
//
//	d <rel>                  a directory
//	l <rel> <target>         a symlink, by READLINK — never followed
//	f <rel> <exec-octal>     a file header, immediately followed by its raw bytes
//
// The digest is hex(sha256(that stream)). Nothing else is in it: not mtimes, not owners,
// not the non-exec permission bits, not the root's own path.
package treedigest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/nofollow"
)

// Of hashes a whole subtree — relative paths, entry kinds, file bytes and SYMLINK
// TARGETS — so "is this the same tree?" is answered by content rather than by name.
//
// Symlink targets are part of the identity rather than followed, because a dotfile-manager
// deployment is a tree of links: two agents' copies of one skill that link to the SAME source are
// the same skill, and two that link to different sources are not. Following them would make every
// such pair compare equal to whatever they happen to point at today. The capture store inherits
// the same rule for a second reason: an installer's tree contains absolute self-references, and a
// digest that followed them would hash whatever else is installed on the machine.
func Of(root string) (string, error) { return OfSkipping(root, nil) }

// OfSkipping is Of omitting a set of RELATIVE paths (and their subtrees)
// entirely — not just their content, but their names, matching hostskills' copyTreeExcept,
// which returns before it so much as creates an excluded directory. See hostskills'
// changedExcept for the one caller that passes a set and why omitting content is honest there.
func OfSkipping(root string, skip map[string]bool) (string, error) {
	h := sha256.New()
	// THE WALK FOLLOWS NO LINK, NOT EVEN A RACING ONE (internal/nofollow): every entry is opened from
	// its parent's descriptor with O_NOFOLLOW and classified by what was opened, so a directory or
	// file an agent swaps for a symlink mid-walk fails the digest instead of hashing what the link
	// names. A device, socket or pipe has no content form here, and is an error rather than a hang.
	err := nofollow.Walk(root, skip, func(e nofollow.Entry) error {
		switch e.Kind {
		case nofollow.Symlink:
			fmt.Fprintf(h, "l %s %s\n", e.Rel, e.Target)
		case nofollow.Dir:
			fmt.Fprintf(h, "d %s\n", e.Rel)
		case nofollow.File:
			// The exec bit is part of the identity: a skill that ships a script the user made
			// executable is not the same skill as one that did not — and a captured install
			// whose binary lost its exec bit is not the same install.
			fmt.Fprintf(h, "f %s %o\n", e.Rel, e.Perm&0o111)
			if _, err := io.Copy(h, e.File); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: not a regular file, directory or symlink", e.Rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
