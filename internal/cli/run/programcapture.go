package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// ProgramCaptureRequest is the fresh launch's selected ordinary installation vector.
// Env is used only for launcher/MCP selection; none of its server arguments or values
// is sent to a producer. The producer resolves execution authority from user selection.
type ProgramCaptureRequest struct {
	Packs                          []*packload.Pack
	Env                            *entrypoint.Env
	Runtime, Platform, Fingerprint string
	RepoRoot, Workspace            string
	Fresh                          bool
	Act                            *ActInterrupt
	Stderr                         io.Writer
	UnsupportedRuntime             string
}

func ProgramRuntimeFingerprint(repoRoot string, cfg *jsonx.OrderedMap) (string, error) {
	var inputs [][]byte
	for _, name := range []string{"flake.nix", "flake.lock"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			return "", err
		}
		inputs = append(inputs, b)
	}
	packages, _ := cfg.Get("packages")
	if packages == nil {
		packages = []any{}
	}
	b, err := json.Marshal(packages)
	if err != nil {
		return "", err
	}
	inputs = append(inputs, b)
	raw, _ := json.Marshal(inputs)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (o *Options) prepareOrdinaryPrograms(cfg *jsonx.OrderedMap, rt, repoRoot string, packs []*packload.Pack, channel *packChannel) bool {
	if o.PreparePrograms == nil || o.captureJail() || o.roBindsUnsupported(rt) != "" {
		return true
	}
	fingerprint, err := ProgramRuntimeFingerprint(repoRoot, cfg)
	if err != nil {
		if o.Update {
			o.pr(o.Stderr).print("ordinary program runtime could not be resolved — repair the flake source, then retry --update: " + err.Error())
			return false
		}
		return true
	}
	_, servers, presets := agentServerTables(cfg, false, packs)
	e := entrypoint.NewEnv(map[string]string{"YOLO_MISE_TOOLS": jsonDumpsOrEmptyObj(cfgMap(cfg, "mise_tools")), "YOLO_MCP_SERVERS": jsonDumpsOrEmptyObj(servers), "YOLO_MCP_PRESETS": jsonDumpsOrEmptyList(presets)})
	reason := OrdinaryRuntimeUnsupported(cfg, o.Getenv("YOLO_STORE_PACKAGES") == "1")
	if channel != nil {
		compositions := []packload.EnvComposition{channel.scope.SharedEnv()}
		for _, agent := range channel.scope.Agents() {
			compositions = append(compositions, channel.scope.EnvFor(agent))
		}
		for _, composition := range compositions {
			for _, env := range composition.Entries() {
				if !env.Unset && ordinaryRuntimeEnvKey(env.Key) {
					reason = "the composed " + env.Key + " changes the consumer runtime environment"
				}
			}
		}
	}
	deliveries, err := o.PreparePrograms(ProgramCaptureRequest{Packs: packs, Env: e, Runtime: rt, Platform: containerJailPlatform(), Fingerprint: fingerprint, RepoRoot: repoRoot, Workspace: o.Workspace, Fresh: o.Update, Act: o.actInterrupt(), Stderr: o.Stderr, UnsupportedRuntime: reason})
	if err != nil {
		fmt.Fprintln(o.Stderr, "selected ordinary preparation failed:", err, "— retry a fresh launch")
		return false
	}
	o.programCaptures = deliveries
	return true
}

// OrdinaryRuntimeUnsupported is the conservative reuse boundary, not a launch
// refusal. The base image marker does NOT identify package/loader variants; these
// installations stay on their authorized bounded legacy route instead of inventing
// equality or widening the credential-free seal.
func OrdinaryRuntimeUnsupported(cfg *jsonx.OrderedMap, storePackages bool) string {
	packages, ok := cfg.Get("packages")
	if ok && packages != nil {
		b, _ := jsonx.DumpsCompact(packages)
		if b != "[]" {
			return "packages: changes the runtime variant beyond the base image identity"
		}
	}
	if storePackages {
		return "store-delivered packages/loader libraries are not identified by the base image marker"
	}
	if tools := cfgMap(cfg, "mise_tools"); tools != nil {
		if _, ok := tools.Get("node"); ok {
			return "the configured Node toolchain is not the sealed producer's observed toolchain"
		}
	}
	if sources, _ := cfg.Get("env_sources"); sources != nil {
		if entries, ok := sources.([]any); ok {
			for _, entry := range entries {
				if _, file := entry.(string); file {
					return "file-backed env_sources has no sealed runtime identity; retain its authorized installation route"
				}
			}
		}
		if env := config.ResolveEnvSources("", cfg, nil); env != nil {
			for _, key := range env.Keys() {
				if ordinaryRuntimeEnvKey(key) {
					return "configured " + key + " can change the installation/runtime loader environment"
				}
			}
		}
	}
	return ""
}

func ordinaryRuntimeEnvKey(key string) bool {
	return key == "PATH" || key == "NODE_OPTIONS" || key == "NODE_PATH" || key == "NODE_EXTRA_CA_CERTS" || key == "NODE_AUTH_TOKEN" || key == "NPM_TOKEN" || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "NPM_CONFIG_")
}
