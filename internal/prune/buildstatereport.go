package prune

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// renderBuildStateLinks is `yolo prune`'s section for the build-state classes the
// launch's housekeeping slot also runs (buildstate.go): retired prefix out-links, aged
// image-copier links and aged package-profile roots. Orphaned prefix roots keep their own
// section. Counts only: removing a link frees nothing until a later store-output pass or
// nix GC, so nothing here joins the reclaimed-bytes summary.
func renderBuildStateLinks(p *printer, rt string, live runtime.LiveSet, buildDir string, apply bool, opts Options) {
	p.line("")
	p.line("[bold]Retired and aged build links[/bold]  [dim](roots nothing reads any more)[/dim]")
	report := func(what string, links []string) {
		if len(links) > 0 {
			p.line(fmt.Sprintf("  %s: %s %s", verb(apply, "would remove", "removed"), fmtComma(len(links)), what))
		}
	}
	removed := 0
	copiers := PruneImageCopierLinks(buildDir, BuildLinkRetention, apply, opts.Now(), nil)
	report("image-copier link(s) dangling or unused for "+BuildLinkRetention.String(), copiers)
	removed += len(copiers)

	sources, srcKnown, why := LivePrefixSources(rt, live, prefixBinMountDest, opts.Exec)
	if srcKnown {
		outLinks := PruneRetiredPrefixOutLinks(buildDir, sources, srcKnown, apply, opts.Now(), nil)
		report("retired jail-prefix out-link(s)", outLinks)
		removed += len(outLinks)
	} else {
		p.line(fmt.Sprintf("  [dim]retired jail-prefix out-links skipped — %s; declining to sweep[/dim]", why))
	}
	profiles, profKnown, profWhy := LiveStoreProfiles(rt, live, opts.Exec)
	if profKnown {
		roots := PrunePackageRoots(joinPath(buildDir, "package-roots"), profiles, profKnown,
			BuildLinkRetention, apply, opts.Now(), nil)
		report("package-profile root(s) no running jail uses, unused for "+BuildLinkRetention.String(), roots)
		removed += len(roots)
	} else {
		p.line(fmt.Sprintf("  [dim]package-profile roots skipped — %s; declining to sweep[/dim]", profWhy))
	}
	if removed == 0 && srcKnown && profKnown {
		p.line("  [dim]none[/dim]")
	}
}
