package cli

// packbinaries.go is `yolo pack install`'s DOWNLOAD STEP: every build of a declared loophole
// binary (internal/loopholedecl's binaries.go) the selected packs need on this machine, fetched,
// verified against the sha256 its manifest pins, and cached by digest with its exec bit set
// (internal/packbin; docs/reference/pack-binaries.md BP-D1).
//
// THIS IS THE ONLY FETCH OF A LOOPHOLE'S BINARY. A launch reads the cache and never downloads one
// (§3.1: "at `pack install`, never at launch"), and says `yolo pack install` when a build it needs
// is missing (loopholes.BinaryInertNotes). A pack-tree program's prebuilt artifact is the exception
// (packtreeprebuilt.go, BG-D7): a launch fetches it too, and this step prefetches it. `yolo pack update` runs install first, so it fetches too.
// Nothing else fills the cache: yolo's own build seeds no pack program, since no official pack
// declares one (docs/reference/pack-binaries.md OQ-BP8).
//
// EVERY SELECTED PACK, embedded ones included, through the one selection function the host verbs
// share (selectConfiguredHostPacks), so the walk is the same whatever a pack's origin. An
// official pack declares no binary, which a short-suite census holds, so in practice only a
// configured or fetched pack has anything to fetch.
//
// WHATEVER THE LOOPHOLE'S SWITCH SAYS. Installing a pack installs what it ships; a loophole
// switched on later then runs without another network step, which is what install is for.

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packbin"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// packBinaryFetcher is the cache `yolo pack install` fills. A seam so a test can serve the
// builds from a local TLS server; production fetches into paths.PackBinariesDir.
var packBinaryFetcher = func() packbin.Fetcher {
	return packbin.Fetcher{Dir: paths.PackBinariesDir()}
}

// installPackBinaries is the download step over the user's selection, on this machine.
func installPackBinaries(pr richtext.Printer, errw io.Writer) int {
	set := selectConfiguredHostPacks()
	return fetchPackBinaries(set.packs, packBinaryFetcher(), runtime.GOOS, runtime.GOARCH, pr, errw)
}

// fetchPackBinaries ensures every build packs need on a goos/goarch machine is in f's cache, and
// reports one line per build: fetched, already cached, re-fetched, or missing for this platform.
// It returns 1 when any build could not be fetched or verified, 0 otherwise. A build two
// loopholes pin with the same digest and name is fetched once.
func fetchPackBinaries(packs []*packload.Pack, f packbin.Fetcher, goos, goarch string,
	pr richtext.Printer, errw io.Writer) int {
	rc := 0
	done := map[string]bool{}
	for _, p := range packs {
		mods, _, _ := p.LoopholeModules()
		for _, mod := range mods {
			if mod.Decl == nil {
				continue // an unreadable manifest is reported where the pack is loaded
			}
			// A loophole whose `platforms` leaves this machine out runs nothing here, so it needs
			// none of its builds — not even one declared for this machine's platform, which a
			// build list and a `platforms` list may both name. The launch reports it on the
			// platform axis; install says why it fetched nothing.
			if len(mod.Decl.BinariesNeeded(goos, goarch)) > 0 && !mod.Decl.SupportsPlatform(goos, goarch) {
				pr.Printf("[dim]%s: loophole %s does not run on %s/%s (its `platforms`), so none "+
					"of its binaries is fetched[/dim]", p.Name, mod.Name, goos, goarch)
				continue
			}
			for _, n := range mod.Decl.BinariesNeeded(goos, goarch) {
				label := fmt.Sprintf("%s: loophole %s: binary %s for %s", p.Name, mod.Name,
					n.Binary, n.Platform)
				if done[label] {
					continue
				}
				done[label] = true
				if !n.HasBuild {
					pr.Printf("[yellow]%s: no build is declared, so the loophole does nothing "+
						"on this machine[/yellow]", label)
					continue
				}
				key := n.Build.SHA256 + "\x00" + n.Binary
				if done[key] {
					continue
				}
				done[key] = true
				_, outcome, err := f.Ensure(context.Background(), packbin.Want{
					Name: n.Binary, URL: n.Build.URL, SHA256: n.Build.SHA256})
				if err != nil {
					fmt.Fprintf(errw, "yolo pack install: %s: %v\n", label, err)
					rc = 1
					continue
				}
				switch outcome {
				case packbin.Cached:
					pr.Printf("[dim]%s: already fetched (sha256 %s)[/dim]", label, shortSHA(n.Build.SHA256))
				case packbin.Replaced:
					pr.Printf("[yellow]%s: the cached copy no longer matched its sha256, so it was "+
						"fetched again and verified (sha256 %s)[/yellow]", label, shortSHA(n.Build.SHA256))
				default:
					pr.Printf("[green]%s: fetched and verified (sha256 %s)[/green]", label,
						shortSHA(n.Build.SHA256))
				}
			}
		}
	}
	if fetchPackTreePrebuilts(packs, f, goos, goarch, pr, errw) != 0 {
		rc = 1
	}
	return rc
}

// fetchPackTreePrebuilts is the same download step for every PREBUILT artifact a launch on this
// machine would take for a selected pack-tree program (docs/design/pack-buildable-programs.md §7,
// BG-D7): resolved as the launch resolves it (packload.PackTreeResolve) for a container jail's
// linux/<goarch>, and on a Mac for the macos-user sandbox's darwin/<goarch> too. Only a program whose
// pack ships no source directory, or one the user opted into its prebuilt
// (`programs.overrides.<bin>: {"prebuilt": true}`, BG-D18), fetches anything; a present source is
// built, never answered with the artifact (P5, OQ-BG2). The launch then finds the artifact in the
// cache, offline included.
func fetchPackTreePrebuilts(packs []*packload.Pack, f packbin.Fetcher, goos, goarch string,
	pr richtext.Printer, errw io.Writer) int {
	rc := 0
	platforms := []string{"linux/" + goarch}
	if goos == "darwin" { // parity: HonoredBy — the macos-user sandbox runs darwin programs, so a Mac prefetches the darwin artifact a launch there would take
		platforms = append(platforms, "darwin/"+goarch)
	}
	optIn := config.ProgramPrebuiltOptIns(nil)
	for _, prog := range packload.PackTreePrograms(packs) {
		for _, platform := range platforms {
			pin := packload.PackTreeResolve(prog, platform, optIn[prog.Bin])
			if !pin.Fork.IsPrebuilt() {
				continue
			}
			pb := *pin.Fork.Prebuilt
			label := fmt.Sprintf("%s: program %s: prebuilt artifact for %s", prog.Pack, prog.Bin, platform)
			_, outcome, err := f.Ensure(context.Background(), packbin.Want{Name: prog.Bin, URL: pb.URL, SHA256: pb.SHA256})
			if err != nil {
				fmt.Fprintf(errw, "yolo pack install: %s: %v\n", label, err)
				rc = 1
				continue
			}
			switch outcome {
			case packbin.Cached:
				pr.Printf("[dim]%s: already fetched (sha256 %s)[/dim]", label, shortSHA(pb.SHA256))
			default:
				pr.Printf("[green]%s: fetched and verified (sha256 %s)[/green]", label, shortSHA(pb.SHA256))
			}
		}
	}
	return rc
}
