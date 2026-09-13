package config

import (
	"slices"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// lsp.go translates the `lsp_servers` config key into the two newline-joined install
// lists the jail-side installer reads — `YOLO_LSP_NPM_INSTALL` and `YOLO_LSP_GO_INSTALL`.
//
// IT LIVES IN `config` BECAUSE BOTH BACKENDS COMPOSE IT, and until 2026-09-13 only one
// could: the recipe table was unexported state of `internal/cli/run`, which imports
// `internal/macosuser` and so can never be imported back. macos-user therefore set
// `YOLO_LSP_SERVERS` — the table that RENDERS an agent's config — and neither install
// list, so its provisioning stage execed the generated script, found an empty list, and
// exited 0 having installed nothing while the agent's config named servers that were not
// there. This is the same shape as `MergeMiseTools` next door: a pure function of the
// merged config that both the podman `-e` lines and the native bootstrap env resolve
// through, so the two cannot compute different answers from one config.

// lspInstallRecipe maps a configured LSP server name to the npm + go packages
// the bootstrap should ensure installed. Frozen contract (recipe values must not
// drift — the bootstrap installer depends on them).
type lspInstallRecipe struct {
	npm []string
	go_ []string
}

var lspInstallRecipes = map[string]lspInstallRecipe{
	"python":     {npm: []string{"pyright"}},
	"typescript": {npm: []string{"typescript-language-server", "typescript"}},
	"go":         {go_: []string{"golang.org/x/tools/gopls@latest"}},
}

// LSPInstalls resolves a merged config's `lsp_servers` keys into the newline-joined npm
// and go install lists. A config with no `lsp_servers` returns two empty strings, which
// is what every reader treats as "install nothing".
func LSPInstalls(config *jsonx.OrderedMap) (npm, goPkgs string) {
	return ResolveLSPInstalls(lspServerNames(config))
}

// lspServerNames returns the `lsp_servers` keys in config load order — the order
// ResolveLSPInstalls preserves into both lists.
func lspServerNames(config *jsonx.OrderedMap) []string {
	if config == nil {
		return nil
	}
	m, ok := asMap(getMapOrEmpty(config, "lsp_servers"))
	if !ok || m == nil {
		return nil
	}
	return m.Keys()
}

// ResolveLSPInstalls translates a configured lsp_servers set into newline-joined
// npm + go install lists (parser-free for the bash side). An empty set returns
// two empty strings. Server names outside the recipe table contribute nothing.
// Dedup preserves first-seen order.
//
// serverNames is the set of configured LSP server names, in config load order;
// pass them in that order.
func ResolveLSPInstalls(serverNames []string) (npm, goPkgs string) {
	if len(serverNames) == 0 {
		return "", ""
	}
	var npmList, goList []string
	for _, name := range serverNames {
		recipe, ok := lspInstallRecipes[name]
		if !ok {
			continue
		}
		for _, pkg := range recipe.npm {
			if !slices.Contains(npmList, pkg) {
				npmList = append(npmList, pkg)
			}
		}
		for _, pkg := range recipe.go_ {
			if !slices.Contains(goList, pkg) {
				goList = append(goList, pkg)
			}
		}
	}
	// The mcp-language-server BRIDGE IS GONE with the gemini agent (A1): gemini was
	// its only consumer — it wrapped every configured LSP as an MCP server keyed
	// "<name>-lsp" (the old buildGeminiMCPServers). Every surviving agent consumes
	// LSP servers natively (copilot via its own lsp-config.json surface; claude via
	// its own tools), so appending the bridge would install a Go binary nothing
	// invokes.
	return strings.Join(npmList, "\n"), strings.Join(goList, "\n")
}
