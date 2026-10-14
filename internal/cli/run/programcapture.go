package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/provision"
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

// NodeFloorUnavailable is an observed provider result, not an installation/network failure.
// It travels only on failed preparation; it is never an artifact admission requirement.
type NodeFloorUnavailable struct {
	Owner, Bin, Floor, Available string
}

func (e *NodeFloorUnavailable) Error() string {
	return fmt.Sprintf("REFUSING to start this jail: pack %s, program %s requires Node >=%s; the observed mise release list has no satisfying version. Available: %s — lower the pack's Node floor or retry after its provider offers a satisfying release", e.Owner, e.Bin, e.Floor, e.Available)
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

func (o *Options) prepareOrdinaryPrograms(cfg *jsonx.OrderedMap, rt, repoRoot string, packs []*packload.Pack, channel *packChannel) int {
	if o.PreparePrograms == nil || o.captureJail() || o.roBindsUnsupported(rt) != "" {
		return 0
	}
	fingerprint, err := ProgramRuntimeFingerprint(repoRoot, cfg)
	if err != nil {
		if o.Update {
			o.pr(o.Stderr).print("ordinary program runtime could not be resolved — repair the flake source, then retry --update: " + err.Error())
			return 1
		}
		return 0
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
		var unavailable *NodeFloorUnavailable
		if errors.As(err, &unavailable) {
			fmt.Fprintln(o.Stderr, unavailable)
			return provision.RefusedStatus
		}
		fmt.Fprintln(o.Stderr, "selected ordinary preparation failed:", err, "— retry a fresh launch")
		return 1
	}
	o.programCaptures = deliveries
	return 0
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
	key = strings.ToUpper(key)
	switch key {
	case "PATH", "NODE_OPTIONS", "NODE_PATH", "NODE_EXTRA_CA_CERTS", "NODE_AUTH_TOKEN", "NPM_TOKEN",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "NIX_SSL_CERT_FILE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
		return true
	}
	return strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "NPM_CONFIG_")
}
