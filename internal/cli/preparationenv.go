package cli

import (
	"os"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// preparationEnvironment is the detached scheduler's complete inherited environment. Source
// transport is the only credential-bearing exception; provider credentials and launch hatches do
// not cross into this host worker.
func preparationEnvironment(environ []string, providerEnvNames []string) []string {
	providers := make(map[string]bool, len(providerEnvNames))
	for _, name := range providerEnvNames {
		providers[name] = true
	}
	allowed := map[string]bool{
		"HOME": true, "PATH": true, "TMPDIR": true, "TMP": true, "TEMP": true,
		"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LC_CTYPE": true, "LC_MESSAGES": true,
		"TERM": true, "TZ": true, "USER": true, "LOGNAME": true,
		"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "NO_COLOR": true,
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
		"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true,
		"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "CURL_CA_BUNDLE": true, "NODE_EXTRA_CA_CERTS": true,
		"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true, "GIT_SSH_COMMAND": true, "GIT_ASKPASS": true,
		"SSH_ASKPASS": true, "YOLO_REPO_ROOT": true, "YOLO_RUNTIME": true,
	}
	values := make(map[string]string)
	for _, item := range environ {
		name, value, ok := strings.Cut(item, "=")
		if !ok || !allowed[name] || providers[name] || strings.HasPrefix(name, "YOLO_ALLOW_") || strings.HasPrefix(name, "YOLO_TEST_") {
			continue
		}
		values[name] = value
	}
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, name := range keys {
		out = append(out, name+"="+values[name])
	}
	return out
}

func preparationProviderEnvNames() []string {
	var names []string
	selection := selectConfiguredHostPacks()
	packs := append([]*packload.Pack(nil), selection.packs...)
	packs = append(packs, packload.Embedded()...)
	seen := map[string]bool{}
	for _, p := range packs {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, provider := range p.Decl.Providers() {
			for _, name := range provider.APIKeyEnvName {
				if !seen[name] {
					names, seen[name] = append(names, name), true
				}
			}
			for _, name := range provider.RegionEnvName {
				if !seen[name] {
					names, seen[name] = append(names, name), true
				}
			}
		}
	}
	return names
}

func currentPreparationEnvironment() []string {
	return preparationEnvironment(os.Environ(), preparationProviderEnvNames())
}
