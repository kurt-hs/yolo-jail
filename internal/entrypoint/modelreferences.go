package entrypoint

// modelreferences.go is the boot's half of MM-D45 (docs/design/model-lists-and-pickers.md): once
// the pack surfaces are rendered, each MODEL REFERENCE a program's pack declares
// (packdecl.ModelReferences, where the term is coined) is read back out of the file the render
// WROTE and resolved against the program's model registry.
//
// Why the boot and not only `yolo check`: a jail cannot run the host render, and the file holds
// what no declaration does — the computed layer the derive re-asserts each boot.
//
// Warnings only, each once per boot: nothing here refuses a launch, and nothing is written.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/modelcatalog"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// reportModelReferences warns about every declared model reference the rendered surfaces hold
// that matches nothing in its program's registry.
func reportModelReferences(e *Env, packs []*packload.Pack) {
	autonomy := e.renderTarget().Profile().AgentAutonomy
	profiles := packload.ProfileTable(e.LoadUseProfiles())
	var providers = e.LoadProviders()
	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, bin := range p.InstallBins() {
			refs := p.Decl.ProgramModelReferences(bin)
			if refs == nil {
				continue
			}
			s, doc, ok := renderedSurface(e, p, refs.Surface, autonomy)
			if !ok {
				continue
			}
			governs := ""
			if name := profiles[bin]; name != "" {
				governs = fmt.Sprintf(", which profile %q derives each boot", name)
			}
			values := packload.ModelReferenceValuesIn(doc, refs, s.Path+" ")
			if len(values) == 0 {
				continue
			}
			reg := packload.NewModelRegistry(packs, providers, bin, bootCatalog(e, p, bin))
			findings, unasked := packload.EvaluateModelReferences(reg, values)
			for _, f := range findings {
				nearest := ""
				if len(f.Nearest) > 0 {
					nearest = " (nearest: " + strings.Join(f.Nearest, ", ") + ")"
				}
				e.warnOnce(fmt.Sprintf("%s: %s names %q, which matches no model in %s's registry%s, so a child "+
					"agent started on it fails at dispatch. `yolo check` on the host names the declaration that "+
					"wrote it%s", bin, f.Where, f.Value, bin, nearest, governs))
			}
			if unasked > 0 {
				e.noteOnce(fmt.Sprintf("%s: %d model reference(s) in %s not checked: %s's catalog is not "+
					"installed yet", bin, unasked, s.Path, bin))
			}
		}
	}
}

// renderedSurface is p's surface id as this boot rendered it, decoded; false when p declares no
// such surface or the file is absent or does not decode.
func renderedSurface(e *Env, p *packload.Pack, id string, autonomy bool) (manifest.Surface, map[string]any, bool) {
	key, err := manifest.ParseSurfaceID(id)
	if err != nil {
		return manifest.Surface{}, nil, false
	}
	surfaces, _ := p.SurfacesFor(autonomy)
	for _, s := range surfaces {
		if s.Key() != key {
			continue
		}
		data, err := os.ReadFile(e.renderTarget().SurfacePath(s))
		if err != nil {
			return s, nil, false
		}
		doc := agentcfg.DecodeSurfaceObject(s.Codec, data)
		return s, doc, doc != nil
	}
	return manifest.Surface{}, nil, false
}

// bootCatalog is the ids bin's installed catalog names, read where this jail's launcher installs
// it (the npm prefix), nil when it is not installed or names none.
func bootCatalog(e *Env, p *packload.Pack, bin string) map[string]bool {
	installs, _ := p.HonoredInstalls()
	for _, inst := range installs {
		if inst.Bin != bin || inst.Kind != "npm" || len(inst.ModelCatalog) == 0 {
			continue
		}
		name, _ := packdecl.SplitNpmSpec(inst.Package)
		dir := filepath.Join(e.NpmPrefix, "lib", "node_modules", filepath.FromSlash(name))
		if !modelcatalog.Installed(dir) {
			return nil
		}
		if cat := modelcatalog.Read(dir, inst.ModelCatalog); cat.Files > 0 {
			return cat.IDs
		}
	}
	return nil
}
