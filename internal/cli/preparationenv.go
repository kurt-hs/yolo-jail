package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// preparationEnvironment bounds the detached host's inputs. Host source transport keeps its
// authorized helpers; this is not the sealed build environment, which has its own boundary.
// Declarations take precedence over the allowlist even when a credential uses a transport name.
func preparationEnvironment(in []string, providers *jsonx.OrderedMap, packs []*packload.Pack) []string {
	excluded := map[string]bool{}
	if providers != nil {
		for _, name := range providers.Keys() {
			v, _ := providers.Get(name)
			entry, _ := v.(*jsonx.OrderedMap)
			for _, n := range packload.CredentialEnvNames(entry) {
				excluded[n] = true
			}
		}
	}
	for _, p := range packs {
		if p.Decl == nil {
			continue
		}
		for _, provider := range p.Decl.Providers() {
			for _, n := range provider.APIKeyEnvName {
				excluded[n] = true
			}
		}
		for bin := range p.Decl.ProviderEnvByBin {
			for _, v := range p.Decl.ProviderEnv(bin) {
				excluded[v.Name] = true
			}
		}
	}
	out := make([]string, 0, len(in))
	for _, kv := range packsrc.CleanGitEnv(in) {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || excluded[name] {
			continue
		}
		switch name {
		// Identity, storage and tool lookup; never workspace or launch-specific inputs.
		case "HOME", "USER", "LOGNAME", "PATH", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR",
			"TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "TZ":
		// TLS and proxy settings are source transport, not agent provider credentials.
		case "SSL_CERT_FILE", "SSL_CERT_DIR", "NIX_SSL_CERT_FILE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
			"http_proxy", "https_proxy", "all_proxy", "no_proxy":
		// Git's ordinary host config/helpers and SSH transport remain authorized. Repository-local
		// GIT_CONFIG_* injections and interactive askpass/UI variables are deliberately absent.
		case "SSH_AUTH_SOCK", "SSH_AGENT_PID", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM":
		// Existing source/runtime controls needed by the same sealed-build launcher. No YOLO_ALLOW_*
		// hatches, jail markers, service tokens or arbitrary YOLO_* prefix is inherited.
		case "YOLO_REPO_ROOT", "YOLO_NIX_HOST_DAEMON", "YOLO_NIX_HOST_STORE_LINUX", "NIX_REMOTE", "NIX_DAEMON_SOCKET_PATH", "NIX_STORE_DIR",
			"CONTAINER_HOST", "CONTAINER_CONNECTION", "PODMAN_CONNECTIONS_CONF", "CONTAINERS_CONF", "CONTAINERS_STORAGE_CONF":
		default:
			continue
		}
		out = append(out, kv)
	}
	// These are worker-owned controls, not inherited agent variables.
	return append(out, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "SSH_ASKPASS_REQUIRE=never")
}

// loadPreparationEnvironment reads user-authorized declarations before process creation. An
// unreadable boundary must not silently degrade to an inherited or only partly filtered env.
func loadPreparationEnvironment() ([]string, error) {
	user, err := config.UserScopeConfig(true, func(string) {})
	if err != nil {
		return nil, preparationEnvironmentError(err)
	}
	problems, _ := config.ValidateProviderSection(user)
	if len(problems) > 0 {
		return nil, preparationEnvironmentError(fmt.Errorf("%s", strings.Join(problems, "; ")))
	}
	var providers *jsonx.OrderedMap
	if v, ok := user.Get("providers"); ok {
		providers, _ = v.(*jsonx.OrderedMap)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sel := selectHostPacks(func(e config.PackEntry) (*packload.Pack, error) {
		return resolveConfiguredPackWithSpec(e, config.ResolvePackSpec{Ctx: ctx, NoWait: true, Detached: true})
	}, config.UserScopeSelection())
	if problems := sel.problems(); len(problems) > 0 {
		var reasons []string
		for _, p := range problems {
			reasons = append(reasons, p.Name+": "+p.Reason)
		}
		return nil, preparationEnvironmentError(fmt.Errorf("%s", strings.Join(reasons, "; ")))
	}
	// Shipped declarations exclude known provider inputs even when the provider is not selected;
	// configured declarations add user-defined names without a tool-name denylist.
	packs := append(append([]*packload.Pack(nil), packload.Embedded()...), sel.packs...)
	if problems := packload.EmbeddedProblems(); len(problems) > 0 {
		return nil, preparationEnvironmentError(fmt.Errorf("%s", strings.Join(problems, "; ")))
	}
	return preparationEnvironment(os.Environ(), providers, packs), nil
}

func preparationEnvironmentError(err error) error {
	return fmt.Errorf("cannot bound detached preparation environment: %w — run `yolo check --no-build` and repair the user pack/provider declarations before retrying", err)
}
