package packload

// packtree.go is the pack-set half of a PACK-TREE PROGRAM (docs/design/pack-buildable-programs.md,
// a `program` delivered `via: "pack"`): which ones a selection carries, as Fork values, and the
// digest of the source directory that is its build's revision.
//
// A PACK-TREE PROGRAM IS A Fork WITH A Run, for the patched extension's reason (patchedtrees.go):
// the fork route's build act, its per-build lock, its store admission and its launch pool take a
// Fork, and a pack-tree program is built by that act with a different source (P1, BG-D2). It is
// not a fork: it owns its bin (Base is ""), it has no source address and no pin, and its revision
// is the content of its pack's own tree. So nothing that pins, checks or rewrites forks ever
// lists one — Forks and ApplyForks read only contributions with a `fork_of` — and PackTreePrograms
// is the one list that does.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/treedigest"
)

// IsPackTree reports whether f is a PACK-TREE PROGRAM's build, not a fork's or an extension's.
func (f Fork) IsPackTree() bool { return f.Run != "" }

// BuildSource is a pack-tree build's source string, "pack:<pack>/<from>"
// (packdecl.PackTreeBuildSource), "" for a fork, whose source is its address (Source).
func (f Fork) BuildSource() string {
	if !f.IsPackTree() {
		return ""
	}
	return packdecl.PackTreeBuildSource(f.Pack, f.From)
}

// PackTreeRecipe is packdecl.PackTreeRecipe for f's declaration, "" for a fork.
func (f Fork) PackTreeRecipe() string {
	if !f.IsPackTree() {
		return ""
	}
	return packdecl.PackTreeRecipe(f.From, f.Build, f.Run)
}

// SourceDir is the absolute source directory a pack-tree build copies and digests: From under
// the pack's root. "" for a fork.
func (f Fork) SourceDir() string {
	if !f.IsPackTree() {
		return ""
	}
	return filepath.Join(f.Root, filepath.FromSlash(f.From))
}

// PackTreePrograms lists every pack-tree program the packs carry, in pack order and then
// declaration order. Root is each pack's root as loaded: at a launch, the staged tree nothing
// edits afterwards, which is the tree the digest must read (the plan's "don't digest the original
// pack directory").
func PackTreePrograms(packs []*Pack) []Fork {
	var out []Fork
	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, c := range p.Decl.Contributions() {
			if c.Kind != packdecl.KindProgram || c.Via != packdecl.ViaPack {
				continue
			}
			out = append(out, Fork{
				Pack: p.Name, Bin: c.Bin, Build: c.Build,
				Produces:  packdecl.PackTreeProduces(c.Bin),
				Platforms: append([]string(nil), c.Platforms...),
				Root:      p.Root, From: c.From, Run: c.Run,
				NodeFloor: c.NodeFloor,
			})
		}
	}
	return out
}

// packTreeDigestSkip is what a pack-tree digest leaves out: the source directory's own `.git`,
// which a `file://` pack checked out in place carries and which reaches no build (BG-D3). The
// build's staging copies the same set, so the build is handed exactly the digested bytes.
var packTreeDigestSkip = map[string]bool{".git": true}

// PackTreeDigestSkip is packTreeDigestSkip, copied, for the staging copy that must leave out
// exactly what the digest does.
func PackTreeDigestSkip() map[string]bool {
	out := make(map[string]bool, len(packTreeDigestSkip))
	for k, v := range packTreeDigestSkip {
		out[k] = v
	}
	return out
}

// PackTreeDigest is a pack-tree build's REVISION: treedigest's canonical digest of dir, the
// program's source directory, with its `.git` left out. An ABSENT directory is "" and no error:
// the manifest may name one the pack does not ship, and the resolver then turns to the prebuilt
// artifact (docs/design/pack-buildable-programs.md §4). A path that exists and is not a directory
// is an error, since nothing could be built from it.
func PackTreeDigest(dir string) (string, error) {
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("the source directory %s is not a directory", dir)
	}
	return treedigest.OfSkipping(dir, packTreeDigestSkip)
}

// Digest is PackTreeDigest of f's source directory, "" and no error for a fork.
func (f Fork) Digest() (string, error) {
	if !f.IsPackTree() {
		return "", nil
	}
	return PackTreeDigest(f.SourceDir())
}
