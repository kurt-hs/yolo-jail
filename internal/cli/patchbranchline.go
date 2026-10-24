package cli

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// patchBranchProvenance is a local checkout's patch branch provenance line (packsrc PatchBranchResolved.Line,
// docs/reference/patched-forks.md §3.5): "<label>: patch branch <ref>  <copy> <commit>  <relation>
// <freshness>", read from the check record, so the launch, `yolo pack update`, `yolo pack series check` and
// `yolo pack status` say the same thing. loud is a diverged branch or a remote that could not be fetched. ""
// for a patch branch at a URL or none.
func patchBranchProvenance(label string, r *packsrc.PatchBranchResolved) (line string, loud bool) {
	text := r.Line(patchedNow())
	if text == "" {
		return "", false
	}
	return label + ": patch branch " + text, r.Diverged() || r.NotVerified()
}

// printPatchBranchProvenance prints patchBranchProvenance, dim or as a warning.
func printPatchBranchProvenance(pr richtext.Printer, label string, r *packsrc.PatchBranchResolved) {
	line, loud := patchBranchProvenance(label, r)
	switch {
	case line == "":
	case loud:
		pr.Printf("[yellow]⚠ %s[/yellow]", richtext.Escape(line))
	default:
		pr.Printf("[dim]%s[/dim]", richtext.Escape(line))
	}
}
