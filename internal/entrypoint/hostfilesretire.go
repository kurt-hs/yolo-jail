package entrypoint

// hostfilesretire.go makes the host `files` render CONVERGE: a path a pack's render wrote on an
// earlier apply, and which that pack no longer ships, leaves the home on the next one.
//
// The render was additive-only. A pack that renamed or removed a shipped file, or moved a
// directory contribution's `into`, got the new set written and the old copy left in place
// forever. In an agent's load-all directory that is not inert: pi loads every top-level file
// under ~/.pi/agent/extensions/ as an extension, so a library module a pack had shipped there
// for four minutes failed every later launch with "Extension does not export a valid factory
// function" — naming a file no pack shipped (dotfiles §25). A host session home links that
// directory from the real home, so the orphan surfaced there too; fixing the real home's render
// is the whole fix.
//
// THE EVIDENCE IS THE OWNERSHIP RECORD THE RENDER ALREADY KEEPS (hostskills.Manifest), not a
// second one: it maps every path the render wrote to the pack that wrote it, and now also to
// the digest of the bytes written. A pack's previous output is its entries in that record; what
// it ships now is computed from its declaration; the difference is the set to retire. A pack
// DROPPED from config is the other axis, and keeps its own confirmed pass
// (cli.pruneDroppedPackOutput) — this one runs only for a pack the apply is rendering.
//
// What happens to each orphan is decided by what can be PROVEN about it:
//
//   - its bytes still match the recorded digest: yolo wrote exactly this and nobody changed it,
//     so it is REMOVED, and the line says so. Nothing of the user's is lost — the bytes are an
//     old version of the pack's own file.
//   - its bytes differ from the digest, or the path is no longer a regular file: the user
//     changed it, which makes it the user's. LEFT IN PLACE, and the line names it, the pack
//     that used to ship it, and the `rm` that removes it. The record keeps naming it, so every
//     apply says it again until the user decides — the launch gate's auto-apply is buffered,
//     and a line said once there would be said to nobody.
//   - no digest (recorded by a yolo older than the digest — the upgrade case, which is exactly
//     the field host's): the record proves yolo wrote it, but not that it is unchanged. While
//     it still carries the read-only mode the render gives every file (hostFileTreeMode) it is
//     ARCHIVED — moved, one `mv` back, the host notch's R2 posture — and the line says where.
//     Once its mode has changed it is left in place and named, as above.
//
// A path the pack still declares is never touched, including every path under a contribution
// whose source is missing this run (the render reports that as a pack problem), and so is
// every path under the pack's skills destinations (per-entry skill delivery shared this record
// before composition, and a skill is not this kind's). A record whose path is already gone is
// forgotten, with no line: nothing was lost.
//
// Retirement never fails an apply. Every error here degrades to a "left in place" or
// "refused" line naming the path.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/hostskills"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// HostFilesLeftAction prefixes the line for an orphan the retire leaves because the user
// changed it — a fact the report prints whatever its verbosity, like a refusal.
const HostFilesLeftAction = "left in place: "

// hostFilesShip is what one pack's `files` render delivers now.
type hostFilesShip struct {
	// paths are the exact destinations this render writes.
	paths map[string]bool
	// roots are destinations under which nothing is retired: a declared contribution whose
	// source is missing this run, a slot or patched extension, a skills destination.
	roots []string
}

// shipped reports whether dest is still this pack's to deliver.
func (s hostFilesShip) shipped(dest string) bool {
	if s.paths[dest] {
		return true
	}
	for _, r := range s.roots {
		if dest == r || strings.HasPrefix(dest, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// keeps reports whether dir must survive an empty-directory sweep: it holds, or is, something
// the pack still delivers.
func (s hostFilesShip) keeps(dir string) bool {
	under := func(p string) bool { return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) }
	for p := range s.paths {
		if under(p) {
			return true
		}
	}
	for _, r := range s.roots {
		if under(r) {
			return true
		}
	}
	return false
}

// retireHostFiles retires pack's previously rendered paths that ship no longer names, and
// returns one result per path it acted on or left. observe computes without writing.
func retireHostFiles(pack, homeDir string, req HostFilesRequest, ship hostFilesShip, observe bool) []HostRenderResult {
	if req.Manifest == nil {
		return nil
	}
	id := pack + "/files"
	var out []HostRenderResult
	for _, dest := range req.Manifest.EntriesOwnedBy(pack) {
		if ship.shipped(dest) || !pathStrictlyUnder(dest, homeDir) {
			continue
		}
		info, err := os.Lstat(dest)
		if err != nil {
			if os.IsNotExist(err) && !observe {
				req.Manifest.Forget(dest) // the record outlived the path: bookkeeping only
			}
			continue
		}
		digest, hasDigest := req.Manifest.Digest(dest)
		left := func(why string) {
			out = append(out, HostRenderResult{Surface: id, Path: dest, Action: fmt.Sprintf(
				"%spack %q no longer ships it, but %s — remove it with `rm %s` if you do not want it",
				HostFilesLeftAction, pack, why, shquote.Quote(dest))})
		}
		switch {
		case !info.Mode().IsRegular():
			if hasDigest {
				left("it is no longer the file yolo wrote")
			}
			// No digest and not a regular file: another kind's entry (a patched extension's
			// link, a pre-composition skill directory). Not this kind's to judge.
			continue
		case hasDigest:
			now, derr := fileDigest(dest)
			if derr != nil || now != digest {
				left("it changed since yolo wrote it")
				continue
			}
			res := HostRenderResult{Surface: id, Path: dest, WouldChange: true}
			if observe {
				res.Action = "would remove (the pack no longer ships it)"
				out = append(out, res)
				continue
			}
			// The file's read-only mode does not stand in the way: unlink needs the
			// directory's write bit, not the file's.
			if rerr := os.Remove(dest); rerr != nil {
				res.Action, res.WouldChange = "refused: "+rerr.Error(), false
				out = append(out, res)
				continue
			}
			req.Manifest.Forget(dest)
			res.Action = "removed (the pack no longer ships it)"
			out = append(out, res)
			sweepEmptyParents(filepath.Dir(dest), homeDir, ship)
		default:
			// Recorded before the record kept digests. The read-only mode the render set is
			// the one trace of "untouched since" there is; a changed mode means someone did.
			if perm := info.Mode().Perm(); perm != 0o444 && perm != 0o555 {
				left("yolo wrote it before it recorded file contents, and its mode has changed since")
				continue
			}
			res := HostRenderResult{Surface: id, Path: dest, WouldChange: true}
			if observe {
				res.Action = "would remove (the pack no longer ships it; it moves to the archive)"
				out = append(out, res)
				continue
			}
			at, aerr := hostskills.Archive(req.ArchiveRoot, req.Stamp, pack, dest)
			if aerr != nil {
				res.Action, res.WouldChange = "refused: "+aerr.Error(), false
				out = append(out, res)
				continue
			}
			req.Manifest.Forget(dest)
			res.Action = "removed (the pack no longer ships it; yolo wrote it before it recorded " +
				"file contents, so it was archived to " + at + " — `mv` it back if it was yours)"
			out = append(out, res)
			sweepEmptyParents(filepath.Dir(dest), homeDir, ship)
		}
	}
	return out
}

// sweepEmptyParents removes dir and each ancestor while it is empty, so a directory
// contribution whose `into` moved does not leave its old location behind as an empty
// directory. It stops at the first directory that is not empty (os.Remove refuses one), at a
// directory the pack still delivers into, and at a symbolic link, and it never removes the home
// or a directory directly under it — a dotdir such as ~/.pi is the agent's, whatever yolo put in
// it.
func sweepEmptyParents(dir, homeDir string, ship hostFilesShip) {
	home := filepath.Clean(homeDir)
	for {
		dir = filepath.Clean(dir)
		parent := filepath.Dir(dir)
		if dir == home || parent == home || !pathStrictlyUnder(dir, home) || ship.keeps(dir) {
			return
		}
		// A link is never swept: os.Remove unlinks one whatever its target holds, and a
		// directory the user linked in (a dotfiles checkout) is theirs.
		if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
			return
		}
		if os.Remove(dir) != nil {
			return
		}
		dir = parent
	}
}

// pathStrictlyUnder reports whether path is strictly below root.
func pathStrictlyUnder(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// bytesDigest is the hex SHA-256 the record keeps for a written file.
func bytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fileDigest is bytesDigest of a regular file's content, read without following a link.
func fileDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
