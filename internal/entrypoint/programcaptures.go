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

func (e *Env) programCaptureShell(bin, declared string) string {
	identity := e.ProgramCaptureCompatibility(bin, declared)
	if identity == "" {
		return ""
	}
	return programCaptureInstallShell(bin, declared, identity, capturesDir(e), receiptsFile(e), e.ProgramCaptureDeliveries()[bin])
}

func programCaptureInstallShell(bin, declared, identity, store, receipts string, delivery ProgramCaptureDelivery) string {
	if identity == "" {
		return ""
	}
	argv := []string{"yolo", "internal", "capture-materialize", "--store=" + store, "--bin=" + bin, "--declared=" + declared, "--compatibility=" + identity, "--receipts=" + receipts}
	if delivery.Runtime != "" {
		argv = append(argv, "--runtime="+delivery.Runtime, "--node-floor="+delivery.NodeFloor)
	}
	nodeArg := ""
	if delivery.NodeFloor != "" {
		// This token is joined with the same generation-time interpreter as exec.
		nodeArg = " --node-path=" + execPrefixToken()
	}
	validation := ""
	if delivery.Runtime != "" {
		validation = "if ! " + shquote.Join(append(append([]string{}, argv...), "--validate-only")) + nodeArg + "; then\n    exit 1\nfi\n"
	}
	// Only metadata in the private home changes at materialization; the machine entry
	// stays immutable, and each identity owns an independent payload directory.
	marker := "${HOME}/.cache/yolo-program-captures/" + bin
	return "\n" + validation + `
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
