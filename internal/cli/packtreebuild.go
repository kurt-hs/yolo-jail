package cli

// packtreebuild.go is the build act's PACK-TREE source (docs/design/pack-buildable-programs.md §6,
// BG-D2 to BG-D5): a program a pack delivers `via: "pack"` is built by the fork route's own act
// (buildFork), with the launch's staged copy of the pack's `from` directory as its source in place
// of a fork's git checkout. Everything else — the per-build lock, the hit after the lock, the sealed
// jail, admission into the capture store and the `build` receipt — is the act's, unchanged.
//
// What is the pack-tree program's own, and lives here:
//
//   - the source staging: `from` copied into the build workspace minus `.git`, exactly the bytes
//     its digest (the build's revision) read;
//   - the PLACEMENT SCRIPT the build jail runs in place of a fork's bare build line: the source
//     copied into ~/.local/lib/yolo-programs/<bin>/, `build` run there, the exec bit set on `run`,
//     and ~/.local/bin/<bin> linked to it (BG-D4, BG-D6);
//   - the trim before admission: the entry holds those two paths and nothing else the build wrote
//     into the home, each dropped path named in the build's log;
//   - the copy with NO BUILD LINE, which the host lays out and admits itself, with no jail (BG-D5).

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// packTreePlacementScript is the command line a pack-tree program's build jail runs, after
// forkBuildJailArgv's `cd` into the staged source: it copies that source into the program's own
// directory in the build home, runs build there, checks that run is a file, sets its exec bit, and
// links ~/.local/bin/<bin> to it by a relative target (packdecl.PackTreeLinkTarget). HOME is the
// build jail's home, the capture home, so every path the build embeds is one the store's relocation
// records. The build line sits on lines of its own inside the braces, so a trailing `# comment`,
// which a build line may carry, cannot comment out the placement after it; packdecl refuses a
// newline in it, so it cannot close the braces early.
func packTreePlacementScript(bin, run, build string) string {
	q := shquote.Quote
	return `d="$HOME"/` + q(packdecl.PackTreeProgramDir(bin)) +
		` && mkdir -p "$d" && cp -a ` + q(path.Join(containerWorkspace, forkSourceLeaf)) + `/. "$d"/` +
		` && cd "$d" && {` + "\n" + build + "\n" + `}` +
		` && test -f "$d"/` + q(run) + ` && chmod +x "$d"/` + q(run) +
		` && mkdir -p "$HOME"/.local/bin && ln -sfn ` + q(packdecl.PackTreeLinkTarget(bin, run)) +
		` "$HOME"/` + q(packdecl.PackTreeProgramLink(bin))
}

// copySourceTreeSkipping is copySourceTree leaving out skip, a set of slash-separated paths
// relative to src (and their subtrees) — packload.PackTreeDigestSkip for a pack-tree source, so the
// build is handed exactly the bytes its digest read.
func copySourceTreeSkipping(src, dst string, skip map[string]bool) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if skip[filepath.ToSlash(rel)] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".yolo-pack-complete" {
			return nil
		}
		return copySourceEntry(p, filepath.Join(dst, rel), d)
	})
}

// stagePackTreeSource copies f's source directory, the launch's staged copy of its pack, into dst
// minus `.git`, and checks that the copy is the tree digest says it builds: a source that moved
// between the launch's digest and this copy would otherwise be admitted under the revision of
// bytes it does not hold.
func stagePackTreeSource(f packload.Fork, digest, dst string) error {
	src := f.SourceDir()
	if fi, err := os.Stat(src); err != nil {
		return fmt.Errorf("pack %s's source directory %s: %w", f.Pack, f.From, err)
	} else if !fi.IsDir() {
		return fmt.Errorf("pack %s's source %s is not a directory", f.Pack, f.From)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := copySourceTreeSkipping(src, dst, packload.PackTreeDigestSkip()); err != nil {
		return err
	}
	got, err := packload.PackTreeDigest(dst)
	if err != nil {
		return err
	}
	if got != digest {
		return fmt.Errorf("pack %s's %s changed while it was staged (digest %s, the launch read %s) — "+
			"launch again to build what it holds now", f.Pack, f.From, shortSHA(got), shortSHA(digest))
	}
	return nil
}

// packTreeKeeps reports whether a home-relative path belongs in a pack-tree program's entry: one of
// its two paths (packdecl.PackTreeProduces), something inside its directory, or an ancestor
// directory of either.
func packTreeKeeps(bin, p string) bool {
	for _, keep := range packdecl.PackTreeProduces(bin) {
		if p == keep || strings.HasPrefix(p, keep+"/") || strings.HasPrefix(keep, p+"/") {
			return true
		}
	}
	return false
}

// prunePackTreeOutput trims a pack-tree build's output, the proto-entry under outDir, to the
// program's two paths (BG-D4): every other path the build wrote into the home — a dependency
// cache under ~/go, a tool's state under ~/.local/share — is removed from the tree, and the
// manifest is rewritten to describe what is left. It returns the dropped paths, the topmost of each
// subtree, sorted, for the build's log. m is updated in place, keeping the surfaces the capture
// walked.
func prunePackTreeOutput(outDir string, m *capture.Manifest, bin string) ([]string, error) {
	var dropped []string
	for _, e := range m.Entries {
		if packTreeKeeps(bin, e.Path) {
			continue
		}
		covered := false
		for _, d := range dropped {
			if strings.HasPrefix(e.Path, d+"/") {
				covered = true
				break
			}
		}
		if !covered {
			dropped = append(dropped, e.Path)
		}
	}
	if len(dropped) == 0 {
		return nil, nil
	}
	tree := capture.TreeDir(outDir)
	for _, d := range dropped {
		if err := os.RemoveAll(filepath.Join(tree, filepath.FromSlash(d))); err != nil {
			return nil, fmt.Errorf("dropping %s from the build's output: %w", d, err)
		}
	}
	trimmed, err := capture.ManifestForTree(tree, m.Home, m.Platform)
	if err != nil {
		return nil, err
	}
	trimmed.Surfaces, trimmed.Excluded = m.Surfaces, m.Excluded
	if err := capture.WriteManifest(outDir, trimmed); err != nil {
		return nil, err
	}
	*m = *trimmed
	sort.Strings(dropped)
	return dropped, nil
}

// packTreeAdmitProblem is a pack-tree build's admit check, "" when its output may be admitted: the
// program's directory, its link at ~/.local/bin/<bin> with the relative target the layout names,
// and `run` inside the directory as a file or a link — then the link check every build gets
// (linksIntoTheBuild).
func packTreeAdmitProblem(m *capture.Manifest, f packload.Fork, workspace string) string {
	byPath := map[string]capture.ManifestEntry{}
	for _, e := range m.Entries {
		byPath[e.Path] = e
	}
	dir, link := packdecl.PackTreeProgramDir(f.Bin), packdecl.PackTreeProgramLink(f.Bin)
	if e, ok := byPath[dir]; !ok || e.Kind != capture.KindDir {
		return fmt.Sprintf("the build left no program directory at ~/%s", dir)
	}
	want := packdecl.PackTreeLinkTarget(f.Bin, f.Run)
	if e, ok := byPath[link]; !ok || e.Kind != capture.KindSymlink || e.Target != want {
		return fmt.Sprintf("the build left no link at ~/%s to %s", link, want)
	}
	if e, ok := byPath[dir+"/"+f.Run]; !ok || e.Kind == capture.KindDir {
		return fmt.Sprintf("the build exited 0 but produced no %s in %s's tree (its `run`)", f.Run, f.From)
	}
	return linksIntoTheBuild(m, workspace)
}

// packTreeCopyStaging is the store staging id of a no-build pack-tree program's copy (b's id).
func packTreeCopyStaging(b forkBuild) string { return "packtree-" + b.id() }

// admitPackTreeCopy is BG-D5: a pack-tree program with no `build` runs nothing, so the host lays
// out its two paths itself — the staged source copied into the program's directory with the exec
// bit on `run`, and the link — as a proto-entry in the store's staging, and admits it under b's
// key, with the same manifest shape a build's trimmed output has. Called with b's build lock held.
func admitPackTreeCopy(store *capture.Store, b forkBuild) (*capture.Entry, *capture.Manifest, error) {
	f := b.Fork
	staging, err := store.Stage(packTreeCopyStaging(b))
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(staging)
	out := filepath.Join(staging, captureOutLeaf)
	tree := capture.TreeDir(out)
	dir := filepath.Join(tree, filepath.FromSlash(packdecl.PackTreeProgramDir(f.Bin)))
	if err := stagePackTreeSource(f, b.Commit, dir); err != nil {
		return nil, nil, err
	}
	runPath := filepath.Join(dir, filepath.FromSlash(f.Run))
	fi, err := os.Stat(runPath)
	switch {
	case err != nil:
		return nil, nil, fmt.Errorf("pack %s's %s holds no %s (its `run`): %w", f.Pack, f.From, f.Run, err)
	case !fi.Mode().IsRegular():
		return nil, nil, fmt.Errorf("pack %s's %s/%s (its `run`) is not a file", f.Pack, f.From, f.Run)
	}
	if err := os.Chmod(runPath, fi.Mode().Perm()|0o111); err != nil {
		return nil, nil, err
	}
	link := filepath.Join(tree, filepath.FromSlash(packdecl.PackTreeProgramLink(f.Bin)))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.Symlink(packdecl.PackTreeLinkTarget(f.Bin, f.Run), link); err != nil {
		return nil, nil, err
	}
	m, err := capture.ManifestForTree(tree, jailHomeDir, b.Platform)
	if err != nil {
		return nil, nil, err
	}
	if why := packTreeAdmitProblem(m, f, containerWorkspace); why != "" {
		return nil, nil, fmt.Errorf("%s — nothing was stored", why)
	}
	if err := capture.WriteManifest(out, m); err != nil {
		return nil, nil, err
	}
	entry, err := store.AdmitEntry(out)
	if err != nil {
		return nil, nil, err
	}
	return entry, m, nil
}

// admitPackTreeCopyUnderLock is buildForkUnderLock's act for a pack-tree program with no `build`,
// once the lock is held and the hit check missed: the copy admitted (admitPackTreeCopy), its
// `build` receipt beside the entry, recording that no build ran, and the result said — on out for
// a caller with no report, as a built entry's is.
func admitPackTreeCopyUnderLock(b forkBuild, mode buildMode, store *capture.Store, pr richtext.Printer) (*capture.Entry, error) {
	f := b.Fork
	mode.run.phase("copying its pack's source; no build line, so no build jail")
	entry, m, err := admitPackTreeCopy(store, b)
	if err != nil {
		return nil, err
	}
	receipt := entrypoint.BuildReceipt{
		Bin: f.Bin, Source: b.buildSource(), Key: entry.Key, Digest: capture.DigestHash(entry.Digest),
		Bytes: m.TotalBytes(), Path: entry.Root, Platform: m.Platform, Revision: b.Commit,
		Recipe: b.recipe(), Toolchain: packTreeNoBuildToolchain,
		Act: entrypoint.ReceiptActRecord, Time: time.Now(),
	}
	if err := entrypoint.AppendReceiptLine(capture.ReceiptsPath(entry.Root), receipt.Line()); err != nil {
		return nil, fmt.Errorf("writing the build receipt: %w", err)
	}
	if mode.run != nil {
		mode.run.admitted(entry.Key, len(m.Entries), m.TotalBytes())
		return entry, nil
	}
	pr.Printf("[green]copied[/green] %s  [cyan]%s[/cyan]  %d paths, %s  [dim]%s; no build line, so nothing ran[/dim]",
		richtext.Escape(f.Label()), entry.Key, len(m.Entries), humanBytes(m.TotalBytes()), richtext.Escape(packTreeWhat(b)))
	return entry, nil
}

// copyPackTreeForLaunch is one no-build pack-tree program's delivery at a jail launch, the pool's
// key it: the host's copy through the build act (buildFork), which takes the build's lock and
// checks for a hit under it, then the result line — or why there is none. It takes no build slot,
// since no jail runs.
func copyPackTreeForLaunch(b forkBuild, rt string, it *poolItem, color bool) entrypoint.ForkDelivery {
	pr := richtext.Printer{W: it.stream(), Color: color}
	if it.stopped() {
		return entrypoint.ForkDelivery{Reason: b.Fork.Bin + " was not copied from pack " + b.Fork.Pack +
			"'s tree: a Ctrl-C ended this launch's wait for its builds — the next launch copies it"}
	}
	ctx := it.context()
	entry, err := buildFork(b, buildMode{lock: pidlock.Mode{Wait: true, Bound: forkBuildWaitBound, Cancel: ctx.Done()},
		runtime: rt, admitCtx: ctx}, it.stream(), it.stream(), color)
	if err != nil {
		pr.Printf("[yellow]%s[/yellow]", richtext.Escape(fmt.Sprintf("Warning: could not copy %s from %s (%v) — "+
			"nothing was stored, and this launch continues without %s. The next launch tries again.",
			b.Fork.Bin, packTreeWhat(b), err, b.Fork.Bin)))
		return entrypoint.ForkDelivery{Reason: "pack " + b.Fork.Pack + "'s copy of " + b.Fork.From +
			" could not be stored on the host (" + err.Error() + ")"}
	}
	return entrypoint.ForkDelivery{Key: entry.Key}
}

// packTreeNoBuildToolchain is what a no-build copy's receipt records as its toolchain: no build
// ran, so none was used (BG-D5's "the receipt records that no build ran").
const packTreeNoBuildToolchain = "none: no build line, the tree copied as shipped"

// packTreeWhat is a pack-tree build's subject for its start line and its log: the pack's source
// directory at its tree digest.
func packTreeWhat(b forkBuild) string {
	return "pack " + b.Fork.Pack + "'s " + b.Fork.From + " at tree digest " + shortSHA(b.Commit)
}
