package entrypoint

import (
	"encoding/json"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// nativeCompatibilityShell emits a version refusal only for the self mode carried by the
// host-resolved native launch context. Container launchers and account mode keep their existing
// behavior; malformed or absent metadata cannot claim to be self mode.
func nativeCompatibilityShell(compatibility *packdecl.VersionCompatibility, context string, native bool) string {
	if compatibility == nil || !native || !nativeSelfMode(context) {
		return "COMPATIBILITY_ENABLED=0\n" + nativeCompatibilityFunctions
	}
	return "COMPATIBILITY_ENABLED=1\n" +
		"COMPATIBILITY_REFUSE_AT_OR_ABOVE=" + shquote.Quote(compatibility.RefuseAtOrAbove) + "\n" +
		"COMPATIBILITY_MESSAGE=" + shquote.Quote(compatibility.Message) + "\n" + nativeCompatibilityFunctions
}

// The context is emitted from the resolved macos-user session plan, not read from an agent's
// environment. Mode 1 is macosuser.ModeSelf; ModeAccount is the zero value.
func nativeSelfMode(context string) bool {
	var metadata struct {
		Mode int `json:"mode"`
	}
	return json.Unmarshal([]byte(context), &metadata) == nil && metadata.Mode == 1
}

const nativeCompatibilityFunctions = `
_yolo_version_at_least() {
    local IFS=. actual_major actual_minor actual_patch threshold_major threshold_minor threshold_patch
    read -r actual_major actual_minor actual_patch <<< "$1"
    read -r threshold_major threshold_minor threshold_patch <<< "$2"
    if (( 10#$actual_major > 10#$threshold_major )); then return 0; fi
    if (( 10#$actual_major < 10#$threshold_major )); then return 1; fi
    if (( 10#$actual_minor > 10#$threshold_minor )); then return 0; fi
    if (( 10#$actual_minor < 10#$threshold_minor )); then return 1; fi
    (( 10#$actual_patch >= 10#$threshold_patch ))
}

_yolo_compatibility_check() {
    [ "$COMPATIBILITY_ENABLED" = "1" ] || return 0
    [ "${_YOLO_PROBE:-}" = "1" ] && return 0
    [ -x "$REAL_BIN" ] || return 0
    local output actual
    if ! output=$("$REAL_BIN" "${PROBE_ARGS[@]}" 2>&1); then
        echo "yolo: ERROR: cannot verify $BIN's version for this macos-user self launch; use a container backend (podman or container), or repair its declared version probe." >&2
        return 1
    fi
    if [[ "$output" =~ ([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
        actual="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
    else
        echo "yolo: ERROR: cannot parse $BIN's declared version probe for this macos-user self launch; use a container backend (podman or container), or repair its declared version probe." >&2
        return 1
    fi
    if [ "${#BASH_REMATCH[1]}" -gt 9 ] || [ "${#BASH_REMATCH[2]}" -gt 9 ] || [ "${#BASH_REMATCH[3]}" -gt 9 ]; then
        echo "yolo: ERROR: $BIN's version is outside the supported numeric comparison bound; refusing this macos-user self launch. Use a container backend (podman or container)." >&2
        return 1
    fi
    if _yolo_version_at_least "$actual" "$COMPATIBILITY_REFUSE_AT_OR_ABOVE"; then
        printf 'yolo: ERROR: %s\n' "$COMPATIBILITY_MESSAGE" >&2
        return 1
    fi
    return 0
}
`
