package entrypoint

// hostbrokenlink.go is the BROKEN-LINK RULE for the destinations a host apply writes THROUGH — a
// config surface and a composed briefing: a destination that is a symlink into a directory that
// does not exist is refused by name, per destination, and never fails the pack it belongs to. The
// `files` kind is not a caller: it archives a destination it wrote before replacing it, a link
// included, refuses one it did not write, and turns any write error into that destination's own
// `refused:` line, so a broken link there already costs one destination and never the pack.
//
// A **broken link** *(coined here)* is a symlink on a destination's path — the destination itself
// or one of its parent directories — whose chain ends at a path whose DIRECTORY does not exist.
// A dotfiles manager (rcm, stow, a home-manager checkout) leaves exactly that when the directory
// it linked into is deleted or moved. Measured on the maintainer's host 2026-09-28:
// ~/.pi/agent/settings.json linked into ~/.dotfiles/pi, which had been deleted when that config
// moved into a pack; the write followed the link, open(2) failed ENOENT, RenderHostPack returned
// the error, and the whole pi pack — and with it the launch of an unrelated agent — failed.
//
// WHY A REFUSAL, and not "create the missing directory" or "replace the link": both of those
// decide something about the user's dotfiles layout that yolo cannot know. Creating the target's
// directory recreates a tree the user deleted, somewhere they are no longer looking; replacing the
// link silently detaches the destination from whatever manages it. The link is the user's, so the
// report names it and its target, and the remedy is theirs to pick.
//
// WHAT IS NOT BROKEN. A link whose target does not exist but whose target's directory DOES is a
// dotfiles checkout that has not created the file yet, and HC-D4 rules that it is written THROUGH
// (hostcreatedfile_test.go's dangling-link case, reported as a change). A plain missing
// destination, with or without its parent directories, is created. Only the case the writer cannot
// complete without inventing a directory is refused.
//
// THE THIRD SHAPE IS LoopingLink, below the read side: a cycle, which a writer repairs rather
// than refuses.
//
// THE READ SIDE IS FindDanglingLink, in this file because it takes the same walk up the path
// (unresolvedLinkOnPath) and along the same chain (linkChainEnd), and a second walker is what
// docs/design/agent-directory-map.md AM-D5 rules out. A file yolo
// READS has no write-through exemption: a link to a missing file reads nothing whether or not the
// target's directory exists. UnreadHostSource builds the whole read-side question on it — a
// dangling link, a non-regular file, a read error — for both notches.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// BrokenLink names a destination's broken link: Link is the symlink on the destination's path,
// Target the end of its chain — the path whose directory does not exist.
type BrokenLink struct {
	Link   string
	Target string
}

// Reason is the refusal sentence: what is wrong, and the remedy. The file is never touched, so
// the sentence says so.
func (b BrokenLink) Reason() string {
	return fmt.Sprintf("%s is a symlink to %s, whose directory does not exist — remove the link "+
		"(`rm %s`) or recreate %s, then apply again; nothing was written there",
		b.Link, b.Target, shquote.Quote(b.Link), filepath.Dir(b.Target))
}

// maxLinkHops bounds a chain walk, as the kernel's own 40 does; a longer chain (or a loop) is
// reported as broken at the destination's first link.
const maxLinkHops = 40

// FindBrokenLink reports the broken link on path's way to the filesystem, or nil when a write to
// path can complete without inventing a directory. See the file header for what counts.
//
// It walks UP from path until something exists (unresolvedLinkOnPath), because a missing path
// means either a plain missing file (created, parents included) or a missing parent whose own
// ancestor may be the link. The first existing entry decides: a regular file or directory means
// nothing on the way is a link to nowhere; a symlink that does not resolve is the candidate.
func FindBrokenLink(path string) *BrokenLink {
	p, serr, found := unresolvedLinkOnPath(path)
	if !found || !os.IsNotExist(serr) {
		// Nothing on the way is a link that does not resolve, or the one there fails for a
		// reason that is not absence, which the writer reports as itself: not this rule's case.
		return nil
	}
	end, ok := linkChainEnd(p)
	if !ok {
		return &BrokenLink{Link: p, Target: firstHop(p)}
	}
	if p == filepath.Clean(path) {
		// The destination itself: broken only when the chain's end has no directory.
		if dirExists(filepath.Dir(end)) {
			return nil
		}
	}
	// An ANCESTOR link that resolves to nothing is broken whatever its parent holds:
	// writing below it means creating the directory it names, which is the same
	// invented tree one level up.
	return &BrokenLink{Link: p, Target: end}
}

// unresolvedLinkOnPath is the WALK UP both predicates in this file share: from path toward the
// root until something exists (Lstat), because a missing path means either a plain missing file
// or a missing parent whose own ancestor may be the link. The first existing entry decides. found
// is true only when that entry is a symlink that os.Stat cannot follow, and serr is that Stat's
// error, so each caller rules on it: ENOENT, a loop (ELOOP), or something else such as EACCES.
// found is false when the first existing entry is not a link, when it resolves, when an Lstat on
// the way fails for a reason that is not absence, and when nothing on the way exists at all.
func unresolvedLinkOnPath(path string) (link string, serr error, found bool) {
	p := filepath.Clean(path)
	for {
		fi, err := os.Lstat(p)
		if err == nil {
			if fi.Mode()&os.ModeSymlink == 0 {
				return "", nil, false
			}
			if _, serr := os.Stat(p); serr != nil {
				return p, serr, true
			}
			return "", nil, false
		}
		if !os.IsNotExist(err) {
			return "", nil, false
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", nil, false
		}
		p = parent
	}
}

// FindDanglingLink reports the symlink that keeps a file yolo READS from being read: a link on
// path's way to the filesystem — path itself or one of its parent directories — whose chain ends
// at nothing, or loops. ok is false when path resolves, when it fails for a reason that is not a
// link to nowhere, and when nothing is there and no link explains it: a plain missing file, which
// is the user not having created it.
//
// It walks up from path with FindBrokenLink's own walk (unresolvedLinkOnPath), because a dotfiles
// manager that linked a whole directory leaves the file's own path absent and the link one or more
// levels up. Unlike FindBrokenLink it exempts nothing: the write-through case (a missing target in
// a directory that exists) is a write's, and a read of it fails all the same.
func FindDanglingLink(path string) (link, target string, ok bool) {
	p, serr, found := unresolvedLinkOnPath(path)
	if !found {
		return "", "", false
	}
	end, chained := linkChainEnd(p)
	switch {
	case !chained:
		// A loop, or a chain longer than the kernel follows: nothing at its end either.
		return p, firstHop(p), true
	case os.IsNotExist(serr):
		return p, end, true
	default:
		// Resolves to something that cannot be read for another reason (EACCES on the
		// way, say): the caller's to report as itself.
		return "", "", false
	}
}

// HostSourceSkip is why a host file yolo was declared to READ was not read: a pack's `reads-host`
// grant or `readsHost` surface, or a briefing's `after: "host:<path>"` file. Why is a clause ("it
// is a symlink to …, which does not exist"); Remedy is a sentence, "" when no one remedy fits.
type HostSourceSkip struct {
	Why    string
	Remedy string
}

// UnreadHostSource says why the host file at src (an absolute path) cannot be read, or nil when
// there is nothing to say: it is a regular file, or nothing is there and no link explains its
// absence — the user has not created it, the normal state.
//
// IT NEVER OPENS src, and that is the point of asking before reading. A dangling link reads as
// absent (ENOENT), so the read cannot tell it from a file the user never made, and a FIFO would
// block the read until something writes into it. Both notches ask this first: the jail launch
// (internal/cli/run's unreadHostSource, which prints the launch line) and the host briefing
// composition (hostBriefingOverlay). One predicate, so the two say the same thing about one file.
func UnreadHostSource(src string) *HostSourceSkip {
	if link, target, ok := FindDanglingLink(src); ok {
		subject := link + " is"
		if link == src {
			subject = "it is"
		}
		return &HostSourceSkip{
			Why:    subject + " a symlink to " + target + ", which does not exist",
			Remedy: "Restore the target, or remove the link",
		}
	}
	info, err := os.Stat(src)
	switch {
	case err == nil && info.Mode().IsRegular():
		return nil
	case err == nil:
		return &HostSourceSkip{Why: "it is not a regular file"}
	case os.IsNotExist(err):
		return nil
	default:
		return &HostSourceSkip{Why: "it could not be read (" + err.Error() + ")"}
	}
}

// LoopingLink reports the symlink on path's way to the filesystem whose chain never ends — a
// loop, or a chain longer than the kernel follows — or "" when nothing on the way is one.
//
// It is the third shape of link this file rules on, and the one a WRITER may repair. A broken
// link (FindBrokenLink) is refused because it is the user's dotfiles layout, and replacing it
// would detach the destination from whatever manages it. A loop is not a layout: nothing can
// read through it, hold bytes behind it, or mean anything by it, so a writer that owns the
// destination replaces it with the file it was asked to write, and says so.
//
// Measured 2026-10-29: agy rewrites its DECLARED mcp_config.json into a link into
// ~/.gemini/config, and a second link back at that end made ~/.gemini/antigravity-cli/
// mcp_config.json a cycle, so open(2) answered ELOOP and the whole jail refused to boot on one
// boot step (configure_agy_mcp) — an agent's config-layout accident costing the launch every
// other agent too. ~/.gemini is workspace state, so it came back on every launch in that
// workspace until the link was removed by hand.
//
// THE WALK IS unresolvedLinkOnPath's, with one difference that walk cannot cover: Lstat follows
// the directories ABOVE the path, so a cycle one directory up surfaces as ELOOP rather than as
// a link to be examined, and this walk steps up a level until it holds the link itself.
//
// IT RETURNS THE MEMBER TO REMOVE, and the choice matters: when the repeat is a link OTHER than
// the destination, that link is the one taken away and the destination keeps its link, so the
// writer still lands where the agent pointed it. The measured case is exactly that —
// ~/.gemini/config/mcp_config.json, a link to its own in-jail path, at the far end of agy's own
// link — and removing it leaves agy's declared link resolving to the file the render writes.
func LoopingLink(path string) (link, target string, looped bool) {
	p, found := firstLinkOnPath(path)
	if !found {
		return "", "", false
	}
	end, cycle := linkChainWalk(p)
	if end != "" {
		return "", "", false // a chain that ENDS is the agent's own redirection
	}
	if cycle == "" {
		cycle = p // a chain longer than the kernel follows: nothing repeats, so the path goes
	}
	return cycle, firstHop(cycle), true
}

// firstLinkOnPath is the first entry on path's way to the filesystem that exists, when it is a
// symlink: path itself, or the nearest existing directory above it. found is false when that
// entry is a real file or directory, when the walk cannot read it for a reason of its own
// (EACCES), and when nothing on the way exists at all — a plain missing file, which the writer
// creates.
func firstLinkOnPath(path string) (string, bool) {
	p := filepath.Clean(path)
	for {
		fi, err := os.Lstat(p)
		if err == nil {
			return p, fi.Mode()&os.ModeSymlink != 0
		}
		if !os.IsNotExist(err) && !errors.Is(err, syscall.ELOOP) {
			return "", false
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", false
		}
		p = parent
	}
}

// linkChainEnd follows the symlink chain starting at link to the first path that is not itself an
// existing symlink, resolving relative targets against each link's directory. ok is false for a
// loop or an over-long chain.
func linkChainEnd(link string) (string, bool) {
	end, cycle := linkChainWalk(link)
	return end, cycle == "" && end != ""
}

// linkChainWalk is linkChainEnd's walk, reporting WHICH member repeats when the chain never ends
// (cycle), so the one walk can also answer LoopingLink's question. cycle is "" for an over-long
// chain, which repeats nothing.
func linkChainWalk(link string) (end, cycle string) {
	seen := map[string]bool{}
	cur := link
	for i := 0; i < maxLinkHops; i++ {
		fi, err := os.Lstat(cur)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			return cur, ""
		}
		if seen[cur] {
			return "", cur
		}
		seen[cur] = true
		target, err := os.Readlink(cur)
		if err != nil {
			return cur, ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(cur), target)
		}
		cur = filepath.Clean(target)
	}
	return "", ""
}

// firstHop is link's own raw target, resolved against its directory — what a loop is reported as.
func firstHop(link string) string {
	target, err := os.Readlink(link)
	if err != nil {
		return link
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return filepath.Clean(target)
}

func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
