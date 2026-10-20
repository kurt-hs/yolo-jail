package run

import (
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/durable"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// knownLaunchYOLOEnvNames is diagnostic recognition only, not a registry of supported
// configuration. Keep the fixed names limited to host launch readers and the launch's
// entrypoint handoff; selected declarations add their own exact names below.
func knownLaunchYOLOEnvNames(packs []*packload.Pack, channel *packChannel) []string {
	known := map[string]bool{}
	add := func(name string) {
		if strings.HasPrefix(name, "YOLO_") {
			known[name] = true
		}
	}
	for _, name := range []string{
		"YOLO_RUNTIME", "YOLO_REPO_ROOT", "YOLO_SVC_ADVERTISE_HOST", "YOLO_EXTRA_PACKAGES",
		"YOLO_PACK_ROOT", "YOLO_CTX_ROOT", "YOLO_MCP_SERVERS", durable.EnvVar,
		"YOLO_NIX_HOST_DAEMON", nixHostStoreLinuxEnv, StorePackagesOptInEnv,
		"YOLO_NO_HOST_LOOPBACK", "YOLO_ALLOW_STALE_IMAGE", AllowSourceSkewEnv,
		"YOLO_ALLOW_LIVE_WORKSPACE", paths.AllowUnreachableServicesEnv,
		paths.AllowMissingProvidersEnv, paths.AllowMissingProgramsEnv,
		config.AllowUnmetCapabilitiesEnv,
		paths.NoProgramReadinessEnv, AllowAttachSkewEnv, patchrecovery.AllowPatchFailuresEnv,
		patchrecovery.AllowOlderUpstreamEnv, patchrecovery.UseCachedGoodEnv,
		patchrecovery.SkipPatchesEnv, patchrecovery.SkipExtensionsEnv,
		"YOLO_NO_AUTO_CAPTURE", "YOLO_NO_AUTO_IMAGE_REAP", "YOLO_NO_BANNER",
		"YOLO_NO_TMUX", herdrOptOutEnv, paths.JailBackendEnv, "YOLO_BLOCK_CONFIG",
		"YOLO_BYPASS_SHIMS", "YOLO_TIMING", "YOLO_VERBOSE", "YOLO_DEBUG", "YOLO_VERSION",
		"YOLO_TEST_REBUILD_IMAGE",
	} {
		add(name)
	}

	for _, alias := range []string{"DEFAULT", "FAST", "BALANCED", "FRONTIER"} {
		add(packload.ModelRoleEnv(strings.ToLower(alias)))
	}

	selectedProviders := map[string]bool{}
	if channel != nil && channel.scope != nil {
		for _, name := range channel.scope.SelectedProviders() {
			selectedProviders[name] = true
		}
		addComposition := func(entries []packload.EnvEntry) {
			for _, entry := range entries {
				add(entry.Key)
			}
		}
		addComposition(channel.scope.SharedEnv().Entries())
		for _, agent := range channel.scope.Agents() {
			addComposition(channel.scope.EnvFor(agent).Entries())
		}
	}
	if channel != nil {
		for name := range channel.callerTokens {
			add(name)
		}
	}
	if channel != nil && channel.userEnv != nil {
		for _, name := range channel.userEnv.Keys() {
			add(name)
		}
	}

	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for name := range p.Decl.EnvContributions() {
			add(name)
		}
		for _, gated := range p.Decl.GatedEnvContributions() {
			for name := range gated.Vars {
				add(name)
			}
		}
		for _, service := range p.Decl.Services() {
			slug := paths.ServiceEnvSlug(service.Name)
			if slug != "" {
				add(paths.ServiceEnvVarPrefix + slug + paths.ServiceEnvVarSuffix)
			}
		}
		for _, provider := range p.Decl.Providers() {
			if !selectedProviders[provider.Name] {
				continue
			}
			for _, name := range provider.APIKeyEnvName {
				add(name)
			}
			for _, name := range provider.RegionEnvName {
				add(name)
			}
		}
		for _, bin := range p.InstallBins() {
			if bin == "" {
				continue
			}
			suffix := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' {
					return r - ('a' - 'A')
				}
				if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
					return r
				}
				return '_'
			}, bin)
			for _, tail := range []string{"_FLAG", "_PATH", "_LOGIN"} {
				add("YOLO_AUTH_PRELAUNCH_" + suffix + tail)
			}
			for _, env := range p.Decl.ProviderEnv(bin) {
				add(env.Name)
			}
		}
	}

	out := make([]string, 0, len(known))
	for name := range known {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
