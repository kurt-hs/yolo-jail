package cli

import (
	"io"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// hostrecoveryscope.go is `yolo host`'s half of WHAT A SET RECOVERY HATCH REACHES
// (docs/reference/patched-forks.md §8.5, PBR-D3; the launch's is internal/cli/run's recoveryscope.go):
// once per invocation, each scoped hatch that is set (run.NoterHatches: not the two that say their own) warns of the names covering none of the
// patched builds the host selects — its patched forks and the patched extensions it delivers —
// in patchrecovery.UnmatchedLine's one shape. Never a refusal.

// hostRecoverySubjects are the patched builds packs select at the host, as a hatch's names match.
func hostRecoverySubjects(packs []*packload.Pack) []patchrecovery.Subject {
	var subs []patchrecovery.Subject
	for _, p := range floorPrograms(packs) {
		if p.Install.IsPatchedFork() {
			subs = append(subs, patchrecovery.Subject{Key: p.Install.ForkedBy + "/" + p.Bin(), Pack: p.Install.ForkedBy,
				Name: p.Bin()})
		}
	}
	for _, f := range packload.PatchedTrees(packs) {
		if f.DeliveredAtHost() {
			subs = append(subs, patchrecovery.Subject{Key: f.Key(), Pack: f.Pack, Name: f.Bin})
		}
	}
	return subs
}

// noteHostRecoveryHatches prints the unmatched-name warnings on errw, color through its gate.
func noteHostRecoveryHatches(errw io.Writer, packs []*packload.Pack) {
	if config.InJail() {
		return
	}
	subs := hostRecoverySubjects(packs)
	color := colorForWriter(errw)
	for _, env := range run.NoterHatches() {
		s := patchrecovery.Read(os.Getenv, env)
		if line := patchrecovery.UnmatchedLine(env, s.Unmatched(subs), subs); line != "" {
			_, _ = io.WriteString(errw, richtext.Render("[yellow]"+richtext.Escape("yolo host: "+line)+"[/yellow]", color)+"\n")
		}
	}
}
