package entrypoint

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// SelectedServerPreparations contains installation declarations only. Preset binaries
// are read from the actual preset renderer, not a second registry or server argv snapshot.
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
	if e.SkipMCPPresets {
		return out
	}
	effective := e.LoadMCPServers()
	for _, preset := range e.LoadMCPPresetNames() {
		vars := make(map[string]string, len(e.Vars))
		for k, v := range e.Vars {
			vars[k] = v
		}
		names, _ := json.Marshal([]string{preset})
		vars["YOLO_MCP_PRESETS"] = string(names)
		vars["YOLO_MCP_SERVERS"] = ""
		canonicalEnv := *e
		canonicalEnv.Vars = vars
		spec := mcpPresetNpmPackages(&canonicalEnv)
		if spec == "" || strings.Contains(spec, " ") {
			continue
		}
		canonical := canonicalEnv.LoadMCPServers()
		c, _ := canonical.Get(preset)
		actual, _ := effective.Get(preset)
		cm, cok := c.(*jsonx.OrderedMap)
		am, aok := actual.(*jsonx.OrderedMap)
		if !cok || !aok {
			continue
		}
		command, _ := cm.Get("command")
		applied, _ := am.Get("command")
		commandString, cok := command.(string)
		appliedString, aok := applied.(string)
		if !cok || !aok || commandString != appliedString {
			continue
		}
		ca, _ := cm.Get("args")
		aa, _ := am.Get("args")
		args, cok := ca.([]any)
		appliedArgs, aok := aa.([]any)
		if !cok || !aok || len(args) == 0 || len(appliedArgs) == 0 {
			continue
		}
		entry, ok := args[0].(string)
		appliedEntry, appliedOK := appliedArgs[0].(string)
		if !ok || !appliedOK || entry != appliedEntry || filepath.Dir(entry) != e.NpmBin() {
			continue
		}
		bin := filepath.Base(entry)
		out = append(out, ProgramPreparation{Bin: bin, Pack: "mcp-preset:" + preset, Install: packdecl.Install{Kind: "npm", Bin: bin, Package: spec}, Mode: mode})
	}
	return out
}
