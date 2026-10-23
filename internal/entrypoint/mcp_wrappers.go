package entrypoint

import "path/filepath"

// GenerateMCPWrappers writes the executable wrapper scripts mcp-wrappers/node and
// mcp-wrappers/npx, each chmod'd |= S_IEXEC. The chrome-devtools-mcp-wrapper it also wrote,
// which nothing spawned, went with `mcp_presets` (2026-10-23): the browser's wrapper is the
// chrome-devtools pack's own.
func GenerateMCPWrappers(e *Env) error {
	if err := writeExecutable(filepath.Join(e.McpWrappersBin(), "node"), nodeWrapper); err != nil {
		return err
	}
	return writeExecutable(filepath.Join(e.McpWrappersBin(), "npx"), npxWrapper)
}

// nodeWrapper is the mcp-wrappers/node body. No LD_LIBRARY_PATH export — nix-ld
// covers the FHS mise node env-free and this wrapper execs the RPATH-self-contained
// nix /bin/node, so the scrubbed-child-env case the old export guarded against is covered
// structurally (docs/reference/mise-node-dynamic-linking.md, step 7). FONTCONFIG_* stay:
// they are font config for a server that drives a browser, unrelated to the loader.
const nodeWrapper = `#!/bin/bash
export FONTCONFIG_FILE="${FONTCONFIG_FILE:-/etc/fonts/fonts.conf}"
export FONTCONFIG_PATH="${FONTCONFIG_PATH:-/etc/fonts}"
exec /bin/node "$@"
`

// npxWrapper is the mcp-wrappers/npx body. No LD_LIBRARY_PATH export — same
// rationale as nodeWrapper (nix-ld + RPATH-self-contained nix /bin/npx).
const npxWrapper = `#!/bin/bash
export FONTCONFIG_FILE="${FONTCONFIG_FILE:-/etc/fonts/fonts.conf}"
export FONTCONFIG_PATH="${FONTCONFIG_PATH:-/etc/fonts}"
exec /bin/npx "$@"
`
