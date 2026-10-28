package config

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/modelcatalog"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

type explicitOfflineCatalog struct {
	bin   string
	globs []string
	dir   string
}

func explicitProviderCatalog(ctx *editValidationContext, config *jsonx.OrderedMap) (map[string]map[string]packdecl.ProviderModel, error) {
	catalog := map[string]map[string]packdecl.ProviderModel{}
	authored, _ := config.Get(providersKey)
	authoredProviders, _ := asMap(authored)
	declarations, err := packload.ComposeProviderDeclarations(authoredProviders, ctx.packs)
	if err != nil {
		return nil, fmt.Errorf("compose provider declarations for offline catalog: %w", err)
	}

	var packPrograms []hostfloor.PackPrograms
	for _, pack := range ctx.packs {
		installs, _ := pack.HonoredInstalls()
		packPrograms = append(packPrograms, hostfloor.PackPrograms{Pack: pack.Name, Installs: installs})
	}
	programs := hostfloor.Programs(packPrograms)
	programByBin := map[string]hostfloor.Program{}
	for _, program := range programs {
		programByBin[program.Bin()] = program
	}
	wire := hostFloorWire(ctx.rawUser)
	floor := &hostfloor.Floor{
		Dir: paths.HostFloorDir(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		NodeFloor: hostfloor.HighestNodeFloor(programs),
		Include: func(pack string) bool {
			allowed, _ := PackPolicyDecision(wire, pack)
			return allowed
		},
		Outranked: hostfloor.Outranker(func(bin string) []string {
			return provisionerOrder(ctx.rawUser, ProvisionerEnvHost, bin)
		}, func(bin string) (string, error) { return exec.LookPath(bin) }),
	}

	var sources []explicitOfflineCatalog
	for _, pack := range ctx.packs {
		installs, _ := pack.HonoredInstalls()
		for _, install := range installs {
			if install.Kind != "npm" || len(install.ModelCatalog) == 0 {
				continue
			}
			source := explicitOfflineCatalog{bin: install.Bin, globs: append([]string(nil), install.ModelCatalog...)}
			program, ok := programByBin[install.Bin]
			if !ok {
				sources = append(sources, source)
				continue
			}
			status := floor.Status(program)
			if status.Disposition != hostfloor.Provisioned || status.Record == nil {
				sources = append(sources, source)
				continue
			}
			source.dir = status.Record.NpmPackageDir(program.Install.Package)
			sources = append(sources, source)
		}
	}

	factsByID := map[string]packdecl.ProviderModel{}
	for _, source := range sources {
		if source.dir == "" {
			continue
		}
		if !modelcatalog.Installed(source.dir) {
			continue
		}
		read, err := modelcatalog.ReadCanonicalFacts(source.dir, source.globs)
		if err != nil {
			return nil, fmt.Errorf("offline provider model catalog for %s (%s): %w; repair the installed package or re-provision it with `yolo host -- %s`", source.bin, source.dir, err, source.bin)
		}
		if read.Files == 0 {
			continue
		}
		for id, facts := range read.Facts {
			if _, exists := factsByID[id]; !exists {
				factsByID[id] = facts
			}
		}
	}

	for _, declaration := range declarations {
		if len(factsByID) == 0 {
			break
		}
		providerFacts := make(map[string]packdecl.ProviderModel, len(factsByID))
		for id, facts := range factsByID {
			providerFacts[id] = facts
		}
		catalog[declaration.Name] = providerFacts
	}
	return catalog, nil
}
