package entrypoint

import (
	"encoding/json"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// sandboxTmpdirEnvShellFn adapts only the program's declared temp-variable followers, immediately
// before the launcher execs it. The native sandbox's final TMPDIR is the source; non-empty explicit
// program settings keep precedence, after the same native root validation as the direct run plan.
const sandboxTmpdirEnvShellFn = `# --- declared program temp variables follow the final sandbox TMPDIR ---
SANDBOX_TMPDIR_CONTEXT=__YOLO_SANDBOX_TMPDIR_CONTEXT__
SANDBOX_TMPDIR_NATIVE=__YOLO_SANDBOX_TMPDIR_NATIVE__
SANDBOX_TMPDIR_ENV=(__YOLO_SANDBOX_TMPDIR_ENV__)
SANDBOX_TMPDIR_NAMES_JSON=__YOLO_SANDBOX_TMPDIR_NAMES_JSON__
_yolo_follow_sandbox_tmpdir() {
    [ "${#SANDBOX_TMPDIR_ENV[@]}" -gt 0 ] || return 0
    if [ "$SANDBOX_TMPDIR_NATIVE" = "1" ] && [ -z "$SANDBOX_TMPDIR_CONTEXT" ]; then
        echo "  ⚠ native temp-validation metadata is missing; restart this workspace with yolo" >&2
        return 1
    fi
    [ -n "$SANDBOX_TMPDIR_CONTEXT" ] || return 0
    YOLO_INTERNAL_SANDBOX_TMPDIR_CONTEXT="$SANDBOX_TMPDIR_CONTEXT" \
        YOLO_INTERNAL_SANDBOX_TMPDIR_NAMES="$SANDBOX_TMPDIR_NAMES_JSON" \
        yolo internal validate-sandbox-tmpdir || return $?
    [ -n "${TMPDIR:-}" ] || return 0
    local name
    for name in "${SANDBOX_TMPDIR_ENV[@]}"; do
        [ -n "${!name:-}" ] || export "$name=$TMPDIR"
    done
}
_yolo_follow_sandbox_tmpdir || exit $?
`

func sandboxTmpdirEnvSplices(inst *packdecl.Install, context string, native bool) []string {
	names, _ := json.Marshal(inst.SandboxTmpdirEnv)
	return []string{
		"__YOLO_SANDBOX_TMPDIR_CONTEXT__", shquote.Quote(context),
		"__YOLO_SANDBOX_TMPDIR_NATIVE__", shquote.Quote(boolFlag(native)),
		"__YOLO_SANDBOX_TMPDIR_ENV__", shquote.Join(inst.SandboxTmpdirEnv),
		"__YOLO_SANDBOX_TMPDIR_NAMES_JSON__", shquote.Quote(string(names)),
	}
}
