package run

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// macoshomeoverlay.go builds the HOME OVERLAY: the staged skills, briefings and pack
// `files` trees laid out at their home-relative destinations, ready to be copied over a
// jail home.
//
// WHY A TREE AND NOT A MAPPING. On the container backends each staged dir is bind-
// mounted at its destination, so the mapping "staging dir → home path" lives in the
// mount list and never crosses into the jail. macos-user has no mounts, so the
// mapping has to reach the sandbox somehow. Sending it as data (a JSON table from
// staging names to home paths that the bootstrap interprets) would put the same
// mapping in two implementations — the mount assembler's and the bootstrap's — which
// is the drift the transport unification exists to end. Laying the tree out by
// DESTINATION host-side instead means the paths in the tree ARE the home paths.
//
// ⚠ BUT THE TREE CANNOT SAY WHERE A DESTINATION STARTS, and that is why a LIST of the
// destinations rides beside it (entrypoint.HomeOverlayManifestName). `.pi/agent/skills/x`
// in the tree does not say whether `.pi/agent/skills` or `.pi/agent` is what a bind would
// have covered, and the install that guessed — replacing the first real directory below
// the home's layout links — deleted pi's whole state dir on every launch (G36). The list
// carries only the roots the tree already spells, written by the same loops that lay the
// tree out, so it maps nothing and cannot disagree with the tree about a path.
//
// It also survives the change that should replace it. The per-workspace sandbox home
// (docs/reference/macos-user-home-tiers.md) moves only the DESTINATION home; the overlay
// itself, and everything below, is unchanged.
//
// WHAT THIS IS NOT: it is not a merge. The overlay holds only what yolo composes, and
// the install that applies it replaces exactly the listed destinations and leaves the
// rest of the home alone — the same semantics a bind mount has. The read-only part is
// the session's Seatbelt profile, which denies writes to every destination this returns.

// buildMacosHomeOverlay lays the staged skills, briefings and pack `files` trees out under
// one root at the home-relative paths they belong at, and returns that root with the
// destinations it holds (a zero HomeOverlay when there is nothing to deliver).
//
// It reads the SAME three declaration lists the container path mounts from —
// packSkillTargets, briefingDestinations and packFilesTargets — so a pack that declares a
// destination gets it on both backends or on neither. A fourth list here would be the shape
// that lets them disagree silently. `files` was the list this used to leave out, so pi's two
// extensions and every agent footer script were absent on this backend and nothing said so
// (docs/plans/notch-convergence.md row D8). A `files` tree whose source is missing is
// skipped with the jail's own warning (packFilesSkipWarning), passed to warn.
//
// THE DESTINATIONS TRAVEL WITH THE TREE because the container's `:ro` is part of the
// delivery, not an extra: the Seatbelt profile write-protects exactly what this wrote
// (macosuser.ResolveHomeReadonly, G14). ⚠ AND THEY ARE ONE LIST WITH THE ONE THE SANDBOX
// INSTALL READS: Dests is what entrypoint.WriteHomeOverlayManifest returned when it wrote
// the tree's destination list, so the destinations the profile protects and the ones the
// bootstrap replaces are the same list rather than two derivations of it. They are the
// destinations the loop below WROTE rather than a second walk of the declarations, so a
// destination nothing was staged for is neither delivered nor protected. WorkspaceDirs is
// the selection's scope:workspace state list, which is what decides where each
// destination physically lands once the bootstrap lays the home-tier layout.
func buildMacosHomeOverlay(staging string, packs []*packload.Pack, warn func(string)) (macosuser.HomeOverlay, error) {
	return buildMacosHomeOverlayWith(staging, packs, nil, nil, warn)
}

// overlayFile is one file CORE composes for the sandbox home — not a pack's, so no declaration
// list names it — delivered through the overlay at Rel, its home-relative path, with Body as its
// bytes. The one today is the generated user scope (macosUserInheritedScope), which the container
// path mounts as a single `:ro` file and this backend can only copy.
type overlayFile struct {
	Rel  string
	Body []byte
}

// buildMacosHomeOverlayWith is buildMacosHomeOverlay with this launch's patched extensions and
// core's own files laid in beside the packs' content.
//
// trees is the launch's per-launch copies by extension key (Options.patchedTreeDirs), the SAME map
// the container path hands packFilesTargets for its mounts, so a delivered patched extension is a
// `files` target like any other — read through the one declaration list, not a fourth — copied to
// its `~/<into>` and write-protected with it (docs/reference/pi-extensions.md §11). A tree with no
// copy has no key, so packFilesTargets makes it no target and nothing lands at its `into`.
//
// Each core file is a destination of its own in the list the tree carries, so the bootstrap
// installs it as it installs a briefing (replacing that one file, and nothing beside it) and the
// Seatbelt profile write-protects it (macosuser.ResolveHomeReadonly), which is this backend's `:ro`.
func buildMacosHomeOverlayWith(staging string, packs []*packload.Pack, trees map[string]string, core []overlayFile,
	warn func(string)) (macosuser.HomeOverlay, error) {
	return buildMacosHomeOverlayDelivering(staging, packs, trees, nil, core, warn)
}

// buildMacosHomeOverlayDelivering is buildMacosHomeOverlayWith with each patched extension's
// IDENTITY beside its per-launch copy: treeIDs maps an extension key to a string naming the
// copy's content (Options.patchedTreeIdentities — the content-addressed store entry it was copied
// from). The overlay's digest takes that string in place of reading every byte of the copy, which
// for a node_modules tree is most of what an unchanged relaunch would otherwise pay. A key with no
// identity is hashed by content like every other source.
func buildMacosHomeOverlayDelivering(staging string, packs []*packload.Pack, trees, treeIDs map[string]string,
	core []overlayFile, warn func(string)) (macosuser.HomeOverlay, error) {
	tree, dests, err := buildMacosHomeOverlayTreeIDs(staging, packSkillTargets(packs),
		briefingDestinations(packs), packFilesTargets(packs, trees), core, treeIDs, warn)
	if err != nil || tree == "" {
		return macosuser.HomeOverlay{}, err
	}
	return macosuser.HomeOverlay{Tree: tree, Dests: dests, WorkspaceDirs: packload.WritableDirs(packs)}, nil
}

// buildMacosHomeOverlayFor is the body, taking the three declaration lists directly, and
// returning the tree and the destination list it wrote beside the tree
// (entrypoint.WriteHomeOverlayManifest's result: cleaned, once each, sorted, with a
// destination inside another dropped).
//
// Split from the wrapper so a test can state the destinations rather than construct
// packs that produce them: the property under test is "staged name → home path", and
// routing it through pack parsing would test the parser instead.
func buildMacosHomeOverlayFor(staging string, skills []jailcontent.SkillTarget,
	briefings []briefingDest, files []packFilesTarget, warn func(string)) (string, []string, error) {
	return buildMacosHomeOverlayTree(staging, skills, briefings, files, nil, warn)
}

// buildMacosHomeOverlayTree is buildMacosHomeOverlayFor with core's files (overlayFile) laid
// out last, each at its own destination.
func buildMacosHomeOverlayTree(staging string, skills []jailcontent.SkillTarget,
	briefings []briefingDest, files []packFilesTarget, core []overlayFile, warn func(string)) (string, []string, error) {
	return buildMacosHomeOverlayTreeIDs(staging, skills, briefings, files, core, nil, warn)
}

// homeOverlayBuildVersion is folded into every overlay digest. Bump it whenever what the builder
// writes for the same inputs changes (a mode rule, a layout, the list's shape), so an overlay an
// older yolo built is never taken for the one this yolo would build.
const homeOverlayBuildVersion = "home-overlay/1"

// overlayItem is one destination the overlay delivers, planned before anything is written: the
// plan is what the digest is computed over and what the write loop lays out, so the two cannot
// disagree about a destination.
type overlayItem struct {
	// kind is "skills", "briefing", "files" or "core".
	kind string
	// dest is the home-relative destination, slash-separated.
	dest string
	// src is the source tree or file of a skills or files item; body the bytes of a briefing or a
	// core file.
	src  string
	body []byte
	// mode is the rule a tree's files are copied under (stagedFileMode or ownFileMode), and
	// modeName its name in the digest.
	mode     func(fs.FileMode) fs.FileMode
	modeName string
	// id, when set, stands for src's whole content in the digest (a patched extension's store
	// entry), so its bytes are not read to compute it.
	id string
	// pack names a files item's pack, for its error.
	pack string
}

// buildMacosHomeOverlayTreeIDs is the builder.
//
// CONTENT-KEYED, NOT REBUILT EVERY LAUNCH. It plans every destination first, computes a digest
// over the plan (homeOverlayDigest: each destination's kind, path, mode rule and content), and when
// the overlay already on disk carries that digest in its destination list, returns it as it is:
// nothing removed, nothing copied. The list is written last, after every destination, so a digest
// on disk marks a complete tree. Any other overlay — none, an older one, one a crash left half
// built — is removed and rebuilt from scratch, which is what keeps a destination that LEFT the
// config from being delivered: its absence changes the digest. The same digest is what lets the
// sealed staged copy skip its re-staging (macosuser.HomeOverlayStagedCurrent).
func buildMacosHomeOverlayTreeIDs(staging string, skills []jailcontent.SkillTarget,
	briefings []briefingDest, files []packFilesTarget, core []overlayFile, treeIDs map[string]string,
	warn func(string)) (string, []string, error) {
	overlay := filepath.Join(staging, "home-overlay")
	items := planHomeOverlay(staging, skills, briefings, files, core, treeIDs, warn)
	if len(items) == 0 {
		// Nothing to deliver — no packs, or none declaring skills, briefings or files, and no
		// file of core's.
		// Returning "" rather than an empty dir keeps the staging and the bootstrap
		// step off the launch entirely, so a bare `yolo -- bash` pays nothing.
		_ = os.RemoveAll(overlay)
		return "", nil, nil
	}
	digest, err := homeOverlayDigest(items)
	if err != nil {
		return "", nil, fmt.Errorf("reading the macos-user home overlay's sources: %w", err)
	}
	if fi, err := os.Lstat(overlay); err == nil && fi.IsDir() {
		if have, dests, ok := entrypoint.HomeOverlayDigest(overlay); ok && have == digest {
			return overlay, dests, nil
		}
	}
	// Rebuilt from scratch: RemoveAll is fine because nothing binds this path (unlike the jail's
	// ~/.yolo/bin anchor, which is why that one is cleared contents-only).
	if err := os.RemoveAll(overlay); err != nil {
		return "", nil, fmt.Errorf("clearing the macos-user home overlay: %w", err)
	}
	// EVERY WRITE BELOW IS BENEATH THE OVERLAY (os.Root): a link one destination lays — a patched
	// extension's tree keeps its links, absolute ones included — must not turn a later
	// destination's write into one through it, onto a host file (copyTreeLinksBeneath, CD-1).
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		return "", nil, err
	}
	root, err := os.OpenRoot(overlay)
	if err != nil {
		return "", nil, err
	}
	defer root.Close()

	// Every destination laid out below, in the order it was written — the input to the
	// list the sandbox install walks (entrypoint.HomeOverlayManifestName).
	var written []string
	for _, it := range items {
		switch it.kind {
		case "skills":
			if err := copyTreeLinksBeneath(it.src, root, filepath.FromSlash(it.dest), it.mode); err != nil {
				return "", nil, fmt.Errorf("staging skills for %s: %w", it.dest, err)
			}
		case "files":
			// A directory or a single file, copied at its destination; the Seatbelt profile then
			// denies the agent writes to it, which is this backend's `:ro`.
			if err := copyTreeLinksBeneath(it.src, root, filepath.FromSlash(it.dest), it.mode); err != nil {
				return "", nil, fmt.Errorf("staging pack %s's files for %s: %w", it.pack, it.dest, err)
			}
		default: // a briefing, or a file of core's
			dst := filepath.FromSlash(it.dest)
			if err := root.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return "", nil, err
			}
			if err := root.WriteFile(dst, it.body, 0o644); err != nil {
				return "", nil, fmt.Errorf("staging %s: %w", it.dest, err)
			}
		}
		written = append(written, it.dest)
	}
	// THE DESTINATION LIST, beside the tree it describes (G36), written LAST and carrying the
	// digest. Without it the install can only guess where a destination starts, and its guess —
	// the first real directory below the home's links — was pi's whole state dir. What it wrote
	// is what this returns, so the profile (Dests) and the install read one list.
	dests, err := entrypoint.WriteHomeOverlayManifestDigest(overlay, written, digest)
	if err != nil {
		return "", nil, fmt.Errorf("listing the macos-user home overlay's destinations: %w", err)
	}
	return overlay, dests, nil
}

// planHomeOverlay lists what the overlay delivers, in the order the builder writes it: skills,
// briefings, pack `files`, then core's own files. It reads the three declaration lists the
// container path mounts from, skips a source that is absent (a pack that declared a skills dest
// but staged nothing, a briefing nothing wrote), and warns once per missing `files` source with
// the jail's own warning (packFilesSkipWarning) — on every launch, whether or not the overlay is
// then rebuilt.
func planHomeOverlay(staging string, skills []jailcontent.SkillTarget, briefings []briefingDest,
	files []packFilesTarget, core []overlayFile, treeIDs map[string]string, warn func(string)) []overlayItem {
	var items []overlayItem
	for _, t := range skills {
		src := filepath.Join(staging, t.Staging)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		items = append(items, overlayItem{kind: "skills", dest: t.Dest, src: src, mode: stagedFileMode, modeName: "staged"})
	}
	for _, d := range briefings {
		body, err := os.ReadFile(filepath.Join(staging, briefingStagingName(d.Into)))
		if err != nil {
			continue
		}
		items = append(items, overlayItem{kind: "briefing", dest: d.Into, body: body})
	}
	for _, t := range files {
		if !isDir(t.Src) && !isFile(t.Src) {
			if warn != nil {
				warn(packFilesSkipWarning(t))
			}
			continue
		}
		it := overlayItem{kind: "files", dest: t.Dest, src: t.Src, mode: stagedFileMode, modeName: "staged", pack: t.Pack}
		if t.Tree != "" {
			// A PATCHED EXTENSION'S TREE keeps each file's own permission bits (pi-extensions.md
			// §7.1, PPX-D45). Its links stay links, as in every copy here (copyTreeLinksBeneath, CD-1).
			it.mode, it.modeName, it.id = ownFileMode, "own", treeIDs[t.Tree]
		}
		items = append(items, it)
	}
	for _, f := range core {
		items = append(items, overlayItem{kind: "core", dest: f.Rel, body: f.Body})
	}
	return items
}

// homeOverlayDigest is the content identity of the overlay the items build: the builder version,
// then for each item in order its kind, destination and mode rule, and its content — the bytes of
// a briefing or core file, the item's identity when it has one, and otherwise every entry of its
// source (path, type, link text, the mode the copy gives a file, and the file's bytes). Every
// field is length-prefixed, so two different plans cannot hash alike by running into each other.
func homeOverlayDigest(items []overlayItem) (string, error) {
	h := sha256.New()
	field := func(s string) { fmt.Fprintf(h, "%d:%s;", len(s), s) }
	field(homeOverlayBuildVersion)
	for _, it := range items {
		field(it.kind)
		field(it.dest)
		field(it.modeName)
		switch {
		case it.src == "":
			sum := sha256.Sum256(it.body)
			field("bytes")
			field(hex.EncodeToString(sum[:]))
		case it.id != "":
			field("id")
			field(it.id)
		default:
			field("tree")
			if err := hashOverlaySource(it.src, it.mode, field); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashOverlaySource feeds src's entries to the digest as copyTreeLinksBeneath copies them: a link
// by its text, never read through, and a regular file by the mode the copy gives it and its bytes,
// opened O_NOFOLLOW and checked regular after the open.
func hashOverlaySource(src string, mode func(fs.FileMode) fs.FileMode, field func(string)) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		field(filepath.ToSlash(rel))
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			field("link")
			field(link)
		case d.IsDir():
			field("dir")
		case d.Type().IsRegular():
			sum, perm, err := hashRegularFile(p)
			if err != nil {
				return err
			}
			field("file")
			field(fmt.Sprintf("%o", mode(perm)))
			field(sum)
		default:
			return fmt.Errorf("%s is a %s, which the overlay copy cannot reproduce", p, d.Type())
		}
		return nil
	})
}

// hashRegularFile is the sha256 of the regular file p and its permission bits, read without
// following a link and without blocking.
func hashRegularFile(p string) (string, fs.FileMode, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !fi.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s is not a regular file", p)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), fi.Mode().Perm(), nil
}
