package entrypoint

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// SelectedServerPreparations contains installation declarations only: the program each held
// pack `mcp` server's `bin` names.
func SelectedServerPreparations(e *Env, packs []*packload.Pack, mode ProgramPreparationMode) []ProgramPreparation {
	var out []ProgramPreparation
	for _, p := range packs {
		installs, _ := p.HonoredInstalls()
		wanted := packMCPServerPrograms(e, packs)
		for _, inst := range installs {
			for _, w := range wanted {
				if inst.Bin == w.Bin && inst.Package == w.Package {
					out = append(out, ProgramPreparation{Bin: inst.Bin, Pack: p.Name, Install: inst, Mode: mode})
				}
			}
		}
	}
	return out
}
