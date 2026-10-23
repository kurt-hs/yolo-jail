package cli

// packtreeprebuilt.go is a pack-tree program's PREBUILT ARTIFACT (docs/design/pack-buildable-programs.md
// §7, BG-D7): where the pack ships no source directory, or the user opted that program into it
// (`programs.overrides.<bin>: {"prebuilt": true}`, BG-D18) — never in place of a build that cannot
// run (OQ-BG2; packload.PackTreeResolve decides) — the program's `prebuilt` entry for the
// platform is fetched through the pack-binary download cache (packbin.Fetcher.Ensure: https at every
// hop, a size bound, the sha256 checked after the last byte, admission only by verified rename),
// verified AGAIN as it is copied into the store's staging, and admitted by the host with no jail
// under the build act's lock, as a no-build copy is (BG-D5):
//
//	~/.local/lib/yolo-programs/<bin>/<bin>   the artifact, mode 0755
//	~/.local/bin/<bin>                       -> ../lib/yolo-programs/<bin>/<bin>
//
// Its key is the build act's formula with the artifact's sha256 as the revision and a recipe tagged
// as a prebuilt's (packdecl.PackTreePrebuiltRecipe), so an admitted artifact is a store hit on every
// later launch, offline included, and never answers for a build of the same program.
//
// NOTHING UNVERIFIED RUNS: no byte of the download is executed or admitted before both digest
// checks pass, and a mismatch or a failed fetch stores nothing and refuses the program, naming the
// URL, both digests for a mismatch, and the next step (the readiness act's refusal and hatch, as a
// failed build's, OQ-BG1's leaning).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packbin"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// packTreePrebuiltToolchain is what a prebuilt artifact's receipt records as its toolchain: no build
// ran here, and the artifact's URL is where its bytes came from.
func packTreePrebuiltToolchain(pb packdecl.PrebuiltBuild) string {
	return "none: prebuilt artifact " + pb.URL + ", sha256 verified"
}

// packTreePrebuiltStaging is the store staging id of a prebuilt artifact's proto-entry (b's id).
func packTreePrebuiltStaging(b forkBuild) string { return "prebuilt-" + b.id() }

// prebuiltNextStep is the next step a failed prebuilt names: for a digest mismatch, the pack's own
// fix, since retrying fetches the same wrong bytes; for anything else, the network and a retry.
func prebuiltNextStep(f packload.Fork, err error) string {
	var integrity *packbin.IntegrityError
	if errors.As(err, &integrity) {
		return "the pack's pinned sha256 does not match what its URL serves, so nothing was run or stored — " +
			"tell pack " + f.Pack + "'s author to fix its `prebuilt`, then run `yolo pack update`"
	}
	return "nothing was stored — check that the URL is reachable from this machine, then launch again " +
		"(`yolo pack install` fetches it ahead of a launch)"
}

// packTreePrebuiltError is a prebuilt that could not be fetched, verified or stored: the URL, the
// cause (an IntegrityError names both digests) and the next step.
func packTreePrebuiltError(f packload.Fork, err error) error {
	return fmt.Errorf("pack %s's prebuilt artifact for %s (%s) could not be used: %w — %s",
		f.Pack, f.Bin, f.Prebuilt.URL, err, prebuiltNextStep(f, err))
}

// copyVerified copies src to dst with mode, hashing what it writes, and refuses (removing dst) when
// the bytes are not want: the second digest check, on the very bytes the store will hold.
func copyVerified(src, dst, want string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, cerr := io.Copy(io.MultiWriter(out, h), in)
	if err := out.Close(); cerr == nil {
		cerr = err
	}
	if cerr == nil {
		if got := hex.EncodeToString(h.Sum(nil)); got != want {
			cerr = &packbin.IntegrityError{URL: src, Want: want, Got: got}
		}
	}
	if cerr != nil {
		_ = os.Remove(dst)
		return cerr
	}
	return os.Chmod(dst, mode)
}

// admitPackTreePrebuilt fetches and verifies b's artifact, lays out its two paths as a proto-entry in
// the store's staging, and admits it under b's key. Called with b's build lock held.
func admitPackTreePrebuilt(ctx context.Context, store *capture.Store, b forkBuild) (*capture.Entry, *capture.Manifest, error) {
	f := b.Fork
	pb := *f.Prebuilt
	cached, _, err := packBinaryFetcher().Ensure(ctx, packbin.Want{Name: f.Bin, URL: pb.URL, SHA256: pb.SHA256})
	if err != nil {
		return nil, nil, err
	}
	staging, err := store.Stage(packTreePrebuiltStaging(b))
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(staging)
	out := filepath.Join(staging, captureOutLeaf)
	tree := capture.TreeDir(out)
	dir := filepath.Join(tree, filepath.FromSlash(packdecl.PackTreeProgramDir(f.Bin)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	run := f.ProgramRun()
	if err := copyVerified(cached, filepath.Join(dir, filepath.FromSlash(run)), pb.SHA256, 0o755); err != nil {
		return nil, nil, err
	}
	link := filepath.Join(tree, filepath.FromSlash(packdecl.PackTreeProgramLink(f.Bin)))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.Symlink(packdecl.PackTreeLinkTarget(f.Bin, run), link); err != nil {
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

// admitPackTreePrebuiltUnderLock is buildForkUnderLock's act for a prebuilt, once the lock is held
// and the hit check missed: the artifact admitted (admitPackTreePrebuilt) and its `build` receipt
// beside the entry, recording the URL and, as the revision, the sha256. It prints nothing: the
// launch's disclosure line (run.PrebuiltLine) and `yolo pack build`'s result line say it.
func admitPackTreePrebuiltUnderLock(b forkBuild, mode buildMode, store *capture.Store) (*capture.Entry, error) {
	f := b.Fork
	ctx := mode.admitCtx
	if ctx == nil {
		ctx = context.Background()
	}
	mode.run.phase("fetching its prebuilt artifact; no build, so no build jail")
	entry, m, err := admitPackTreePrebuilt(ctx, store, b)
	if err != nil {
		return nil, packTreePrebuiltError(f, err)
	}
	receipt := entrypoint.BuildReceipt{
		Bin: f.Bin, Source: b.buildSource(), Key: entry.Key, Digest: capture.DigestHash(entry.Digest),
		Bytes: m.TotalBytes(), Path: entry.Root, Platform: m.Platform, Revision: b.Commit,
		Recipe: b.recipe(), Toolchain: packTreePrebuiltToolchain(*f.Prebuilt),
		Act: entrypoint.ReceiptActRecord, Time: time.Now(),
	}
	if err := entrypoint.AppendReceiptLine(capture.ReceiptsPath(entry.Root), receipt.Line()); err != nil {
		return nil, fmt.Errorf("writing the receipt: %w", err)
	}
	if mode.run != nil {
		mode.run.admitted(entry.Key, len(m.Entries), m.TotalBytes())
	}
	return entry, nil
}

// fetchPrebuiltForLaunch is one prebuilt's delivery at a jail launch, the pool's key it: the fetch
// and admission through the build act (buildFork), which takes the build's lock and checks for a
// hit under it — or why there is none, as a warning on the pool's stream and the reason the
// readiness act refuses the program with. It takes no build slot, since no jail runs.
func fetchPrebuiltForLaunch(b forkBuild, rt string, it *poolItem, color bool) entrypoint.ForkDelivery {
	f := b.Fork
	pr := richtext.Printer{W: it.stream(), Color: color}
	if it.stopped() {
		return entrypoint.ForkDelivery{Reason: f.Bin + "'s prebuilt artifact from pack " + f.Pack + " was not " +
			"fetched: a Ctrl-C ended this launch's wait for its builds — the next launch fetches it"}
	}
	ctx := it.context()
	entry, err := buildFork(b, buildMode{lock: pidlock.Mode{Wait: true, Bound: forkBuildWaitBound, Cancel: ctx.Done()},
		runtime: rt, admitCtx: ctx}, io.Discard, it.stream(), color)
	if err != nil {
		pr.Printf("[yellow]%s[/yellow]", richtext.Escape("Warning: "+err.Error()))
		return entrypoint.ForkDelivery{Reason: err.Error()}
	}
	return entrypoint.ForkDelivery{Key: entry.Key}
}
