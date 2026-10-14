package entrypoint

import (
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// ProgramCapturesEnv is a launch's immutable exact ordinary-program decisions.
// Admission may move independently; a running launch keeps this vector unchanged.
const ProgramCapturesEnv = "YOLO_PROGRAM_CAPTURES"

type ProgramCaptureDelivery struct {
	Declared      string `json:"declared"`
	Compatibility string `json:"compatibility"`
	Platform      string `json:"platform"`
	Runtime       string `json:"runtime,omitempty"`
	NodeFloor     string `json:"node_floor,omitempty"`
}

func MarshalProgramCaptures(deliveries map[string]ProgramCaptureDelivery) string {
	b, err := json.Marshal(deliveries)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (e *Env) ProgramCaptureDeliveries() map[string]ProgramCaptureDelivery {
	var out map[string]ProgramCaptureDelivery
	if json.Unmarshal([]byte(e.Getenv(ProgramCapturesEnv)), &out) != nil {
		return nil
	}
	return out
}

func (e *Env) ProgramCaptureCompatibility(bin, declared string) string {
	d := e.ProgramCaptureDeliveries()[bin]
	if d.Declared != declared || d.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return ""
	}
	b, err := hex.DecodeString(d.Compatibility)
	if err != nil || len(b) != 32 {
		return ""
	}
	return d.Compatibility
}

func (e *Env) programCaptureShell(bin, declared, token string) string {
	identity := e.ProgramCaptureCompatibility(bin, declared)
	if identity == "" {
		return ""
	}
	return programCaptureInstallShell(bin, declared, identity, capturesDir(e), receiptsFile(e), e.ProgramCaptureDeliveries()[bin], token)
}

func programCaptureInstallShell(bin, declared, identity, store, receipts string, delivery ProgramCaptureDelivery, token string) string {
	if identity == "" {
		return ""
	}
	argv := []string{"yolo", "internal", "capture-materialize", "--store=" + store, "--bin=" + bin, "--declared=" + declared, "--compatibility=" + identity, "--receipts=" + receipts}
	if delivery.Runtime != "" {
		argv = append(argv, "--runtime="+delivery.Runtime, "--node-floor="+delivery.NodeFloor)
	}
	nodeArg, nodeSelection, nodeBinding := "", "", ""
	if delivery.Runtime != "" {
		// The renderer shares this token with every exec, including pending-floor
		// regeneration. A no-floor consumer binds its actual PATH-selected Node,
		// not the shim's bytes or a separately chosen image interpreter.
		nodeArg = " --node-path=" + token
		if delivery.NodeFloor == "" {
			// Bind env-node shebangs without interpreting a native executable
			// shipped through npm (opencode is one). Selection still happens on
			// the consumer's original PATH; only this launcher's children inherit
			// the selected physical interpreter ahead of its selector shim.
			nodeSelection = "_CAPTURE_NODE=$(node -p 'process.execPath') || { echo 'prepared Node is unavailable — retry a fresh launch' >&2; exit 1; }\n"
			// The CLI proves this directory's executable node is the same file
			// before PATH is changed, including on warm launcher re-entry.
			nodeBinding = "export PATH=\"${_CAPTURE_NODE%/*}:$PATH\"\n"
			nodeArg = " --node-path=\"$_CAPTURE_NODE\""
		}
	}
	validation := ""
	if delivery.Runtime != "" {
		validation = "if ! " + shquote.Join(append(append([]string{}, argv...), "--validate-only")) + nodeArg + "; then\n    exit 1\nfi\n"
	}
	// Only metadata in the private home changes at materialization; the machine entry
	// stays immutable, and each identity owns an independent payload directory.
	marker := "${HOME}/.cache/yolo-program-captures/" + bin
	return "\n" + nodeSelection + validation + nodeBinding + `
_CAPTURE_ID=` + shquote.Quote(identity) + `
_CAPTURE_MARKER="` + marker + `"
if [ "$(cat "$_CAPTURE_MARKER" 2>/dev/null || true)" != "$_CAPTURE_ID" ] || [ ! -x "$REAL_BIN" ]; then
    if ! ` + shquote.Join(argv) + nodeArg + `; then
        echo ` + shquote.Quote("  "+bin+": exact prepared artifacts could not be delivered — restart with a fresh launch to retry preparation") + ` >&2
        exit 1
    fi
    mkdir -p "$(dirname "$_CAPTURE_MARKER")"
    printf '%s\n' "$_CAPTURE_ID" > "$_CAPTURE_MARKER"
fi
# A prepared generation never runs an in-place registry/vendor update.
UPDATES_ENABLED=0
PINNED=1
printf '%s\n' "$SPEC" > "$SPEC_FILE"
if [ "${YOLO_PACK_UPDATE:-}" = "1" ]; then exit 0; fi
`
}

// ProgramCaptureForSpec selects an installed MCP package's already-decided exact
// artifacts; raw npx commands and undeclared table entries never enter this vector.
func (e *Env) ProgramCaptureForSpec(spec string) (string, string) {
	for bin, d := range e.ProgramCaptureDeliveries() {
		if d.Declared == spec {
			if id := e.ProgramCaptureCompatibility(bin, spec); id != "" {
				return bin, id
			}
		}
	}
	return "", ""
}

func (e *Env) programCapturePresetChecks() string {
	var lines []string
	for _, spec := range strings.Fields(mcpPresetNpmPackages(e)) {
		bin, id := e.ProgramCaptureForSpec(spec)
		if id == "" {
			continue
		}
		argv := []string{"yolo", "internal", "capture-materialize", "--store=" + capturesDir(e), "--bin=" + bin, "--declared=" + spec, "--compatibility=" + id, "--receipts=" + receiptsFile(e)}
		d := e.ProgramCaptureDeliveries()[bin]
		if d.Runtime != "" {
			argv = append(argv, "--runtime="+d.Runtime, "--node-floor="+d.NodeFloor)
		}
		lines = append(lines, shquote.Join(argv)+" || exit 1")
	}
	return strings.Join(lines, "\n")
}

func mcpPresetLegacyNpmPackages(e *Env) string {
	var specs []string
	for _, spec := range strings.Fields(mcpPresetNpmPackages(e)) {
		if _, id := e.ProgramCaptureForSpec(spec); id == "" {
			specs = append(specs, spec)
		}
	}
	return strings.Join(specs, " ")
}

// ProgramCaptureMarker names only this home's materialized decision, never admission authority.
func ProgramCaptureMarker(home, bin string) string {
	return filepath.Join(home, ".cache/yolo-program-captures", bin)
}
