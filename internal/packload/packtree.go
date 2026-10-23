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
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
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
	if f.IsPrebuilt() {
		return packdecl.PackTreePrebuiltRecipe(f.Bin)
	}
	return packdecl.PackTreeRecipe(f.From, f.Build, f.Run)
}

// IsPrebuilt reports whether f is a pack-tree program delivered from its PREBUILT artifact
// (docs/design/pack-buildable-programs.md §7): the resolver (PackTreeResolve) chose that rung, so
// the act fetches and verifies Prebuilt in place of building From.
func (f Fork) IsPrebuilt() bool { return f.IsPackTree() && f.Prebuilt != nil }

// ProgramRun is the program file inside its directory that the link names: `run` for a build or a
// copy, the artifact's own name (packdecl.PackTreePrebuiltRun) for a prebuilt. "" for a fork.
func (f Fork) ProgramRun() string {
	if f.IsPrebuilt() {
		return packdecl.PackTreePrebuiltRun(f.Bin)
	}
	return f.Run
}

// PrebuiltPlatforms are the platforms f's `prebuilt` declares, sorted; nil for none.
func (f Fork) PrebuiltPlatforms() []string {
	var out []string
	for p := range f.Prebuilts {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// PackTreeResolve is the resolver's build-or-prebuilt half for one pack-tree program on platform
// (docs/design/pack-buildable-programs.md §4; the dev override, the rung above, is decided
// elsewhere): which bytes this delivery takes, as the pin the fork route's build act keys on.
//
//   - The source directory is present and a build can run: the BUILD, its revision the tree
//     digest. The artifact is never consulted (P5: never fetch a prebuilt while a build is
//     possible).
//   - The source directory is present and buildable is false: no build can EVER run for this
//     notch with this yolo. A builder that is only busy is never passed as unbuildable (the
//     leaning of OQ-BG2, so the bytes never depend on timing). The artifact stands in when
//     `prebuilt` declares platform; otherwise the build pin is returned, and the caller's own
//     refusal of that notch applies.
//   - The source directory is absent: the PREBUILT when `prebuilt` declares platform, its revision
//     the artifact's sha256; otherwise a reason naming the pack, `from`, the platform and the
//     platforms `prebuilt` does declare.
//
// A prebuilt pin's Fork carries the artifact (Prebuilt) and why it was chosen (PrebuiltWhy).
func PackTreeResolve(f Fork, platform string, buildable bool) ForkPin {
	p := ForkPin{Fork: f}
	digest, err := f.Digest()
	if err != nil {
		p.Reason = "pack " + f.Pack + "'s source directory " + f.From + " could not be read (" + err.Error() +
			") — fix the pack's tree and launch again"
		return p
	}
	pb, declared := f.Prebuilts[platform]
	switch {
	case digest != "" && (buildable || !declared):
		p.Commit = digest
		return p
	case digest != "":
		f.PrebuiltWhy = "no build of a pack's own tree runs for " + platform + " with this yolo"
	case !declared:
		have := "none"
		if ps := f.PrebuiltPlatforms(); len(ps) > 0 {
			have = strings.Join(ps, ", ")
		}
		p.Reason = "pack " + f.Pack + " ships no " + f.From + " to build " + f.Bin + " from, and its `prebuilt` " +
			"declares no artifact for " + platform + " (it declares: " + have + ") — add that directory to the " +
			"pack, or a `prebuilt` entry for " + platform
		return p
	default:
		f.PrebuiltWhy = "no " + f.From + " in the pack"
	}
	f.Prebuilt = &pb
	p.Fork, p.Commit = f, pb.SHA256
	return p
}

// clonePrebuilts copies a `prebuilt` map, nil for none.
func clonePrebuilts(in map[string]packdecl.PrebuiltBuild) map[string]packdecl.PrebuiltBuild {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]packdecl.PrebuiltBuild, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
				Prebuilts: clonePrebuilts(c.Prebuilt),
			})
		}
	}
	return out
}

// packTreeDigestSkip is what a pack-tree digest leaves out: the source directory's own `.git`,
// which a `file://` pack checked out in place carries and which reaches no build (BG-D3), and the
// pack store's completion marker, which a fetched pack's checkout keeps at its root and a launch's
// staging copies with it, so a `from: "."` source holds it. The build's staging copies the same
// set, so the build is handed exactly the digested bytes.
var packTreeDigestSkip = map[string]bool{".git": true, packsrc.TreeCompleteMarker: true}

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
