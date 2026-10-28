package entrypoint

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// nativeCompatibilityShell emits a version refusal only for the self mode carried by the
// host-resolved native launch context. Container launchers and explicit account mode keep their
// existing behavior; invalid native metadata cannot silently disable a declared compatibility gate.
func nativeCompatibilityShell(compatibility *packdecl.VersionCompatibility, context string, native bool) string {
	if compatibility == nil || !native {
		return "COMPATIBILITY_CONTEXT_VALID=1\nCOMPATIBILITY_ENABLED=0\n" + nativeCompatibilityFunctions
	}
	valid, self := nativeCompatibilityMode(context)
	if !valid {
		return "COMPATIBILITY_CONTEXT_VALID=0\nCOMPATIBILITY_ENABLED=0\n" +
			"COMPATIBILITY_MESSAGE=" + shquote.Quote("yolo: ERROR: invalid macos-user launch context; refusing this program because its native compatibility rule cannot be evaluated. Use a valid macos_user_mode (self or account), or use a container backend (podman or container).") + "\n" + nativeCompatibilityFunctions
	}
	if !self {
		return "COMPATIBILITY_CONTEXT_VALID=1\nCOMPATIBILITY_ENABLED=0\n" + nativeCompatibilityFunctions
	}
	return "COMPATIBILITY_CONTEXT_VALID=1\nCOMPATIBILITY_ENABLED=1\n" +
		"COMPATIBILITY_REFUSE_AT_OR_ABOVE=" + shquote.Quote(compatibility.RefuseAtOrAbove) + "\n" +
		"COMPATIBILITY_MESSAGE=" + shquote.Quote(compatibility.Message) + "\n" + nativeCompatibilityFunctions
}

// The context is emitted from the resolved macos-user session plan, not read from an agent's
// environment. A mode is valid only when it is explicitly encoded as account (0) or self (1).
func nativeCompatibilityMode(context string) (valid, self bool) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal([]byte(context), &metadata); err != nil || metadata == nil {
		return false, false
	}
	mode, ok := metadata["mode"]
	if !ok {
		return false, false
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(mode)))
	if err != nil {
		return false, false
	}
	switch value {
	case 0:
		return true, false
	case 1:
		return true, true
	default:
		return false, false
	}
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
    [ "${_YOLO_PROBE:-}" = "1" ] && return 0
    if [ "$COMPATIBILITY_CONTEXT_VALID" != "1" ]; then
        printf '%s\n' "$COMPATIBILITY_MESSAGE" >&2
        return 1
    fi
    [ "$COMPATIBILITY_ENABLED" = "1" ] || return 0
    [ -x "$REAL_BIN" ] || return 0
    local output actual probe_arg probe_ok=0
    for probe_arg in "${PROBE_ARGS[@]}"; do
        if output=$("$REAL_BIN" "$probe_arg" 2>&1); then
            probe_ok=1
            break
        fi
    done
    if [ "$probe_ok" != "1" ]; then
        echo "yolo: ERROR: cannot verify $BIN's version for this macos-user self launch; use a container backend (podman or container), or repair its declared version probe." >&2
        return 1
    fi
    if [[ "$output" =~ (^|[^[:alnum:].]|[vV])([0-9]+)\.([0-9]+)\.([0-9]+)($|[^[:alnum:].]) ]]; then
        actual="${BASH_REMATCH[2]}.${BASH_REMATCH[3]}.${BASH_REMATCH[4]}"
    else
        echo "yolo: ERROR: cannot parse $BIN's declared version probe for this macos-user self launch; use a container backend (podman or container), or repair its declared version probe." >&2
        return 1
    fi
    if [ "${#BASH_REMATCH[2]}" -gt 9 ] || [ "${#BASH_REMATCH[3]}" -gt 9 ] || [ "${#BASH_REMATCH[4]}" -gt 9 ]; then
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
