package entrypoint

// overridelauncher.go is the jail's half of a DEV OVERRIDE (docs/design/pack-buildable-programs.md
// §5, BG-D8/BG-D9): `programs.overrides.<bin>` in the user's config points one program a selected
// pack declares at a host checkout, and the launcher generated for that bin runs the checkout's
// file in place of the pack's own delivery, whatever its `via`.
//
// # The host decides, the jail runs
//
// The override lives in the user config, which no jail reads, so the host resolves it at a fresh
// launch (run's programOverridePlans): it resolves the path's symlinks, refuses a path at the
// credential boundary, decides where the directory appears in the jail (the workspace's own path,
// or a read-only bind at /ctx/program-overrides/<bin>), and hands the jail, per bin, that directory
// and the file to run, or the reason it refused, in one env pair (ProgramOverridesEnv).
//
// # What the launcher never does
//
//   - Build or install. The checkout runs as found (OQ-BG3's leaning): a missing file is refused,
//     naming the pack's build line to run in that checkout.
//   - Fall through to the pack's own copy. An override that is refused is a refused program
//     (P5), never a silent return to the bytes the user chose not to run.
//   - Write into the override's directory. It is the developer's.
//
// In install-only mode (the readiness act) and update mode it checks that the file is there and
// executable, and stops; a refusal there refuses the launch through the readiness act's own
// refusal and hatch (OQ-BG1's leaning).

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// ProgramOverridesEnv names the host's per-bin dev-override decisions in the jail environment: a
// JSON object, bin → ProgramOverride. A host↔jail contract in ForkBuildsEnv's class.
const ProgramOverridesEnv = "YOLO_PROGRAM_OVERRIDES"

// ProgramOverrideCtxDir is the jail directory under which an override outside the workspace is
// bound read-only, one subdirectory per bin.
const ProgramOverrideCtxDir = "/ctx/program-overrides"

// ProgramOverride is the host's answer for one overridden program this launch.
type ProgramOverride struct {
	// Dir is the override's directory as the jail sees it; "" when Refused is set.
	Dir string `json:"dir,omitempty"`
	// Run is the file to run, relative to Dir (cleaned, no ".."); "" when Refused is set.
	Run string `json:"run,omitempty"`
	// Host is the directory's resolved host path, which every line names: "which bytes".
	Host string `json:"host,omitempty"`
	// Build is the declaring pack's build line, named by the refusal of a missing file; "" when the
	// pack declares none.
	Build string `json:"build,omitempty"`
	// Refused is why the host refused the override, naming the key to edit; "" when it applies.
	Refused string `json:"refused,omitempty"`
}

// ProgramOverridesWire is the env value for d, or "" for none.
func ProgramOverridesWire(d map[string]ProgramOverride) string {
	if len(d) == 0 {
		return ""
	}
	b, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseProgramOverrides reads a ProgramOverridesEnv value; "" is none.
func ParseProgramOverrides(raw string) (map[string]ProgramOverride, error) {
	if raw == "" {
		return nil, nil
	}
	var d map[string]ProgramOverride
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, err
	}
	return d, nil
}

// programOverrides reads the host's decisions from the environment. An unreadable value applies
// no override and says so: every program then runs its pack's own delivery, which is what the jail
// would do with no override at all, and the line names why the user's choice did not arrive.
func programOverrides(e *Env) map[string]ProgramOverride {
	d, err := ParseProgramOverrides(e.Getenv(ProgramOverridesEnv))
	if err != nil {
		e.warn(fmt.Sprintf("yolo-entrypoint: %s is not a dev-override decision this build reads (%v) — "+
			"no programs.overrides entry applies in this jail", ProgramOverridesEnv, err))
		return nil
	}
	return d
}

// RunPath is the host path of the file an applied override runs.
func (o ProgramOverride) RunPath() string {
	if o.Run == "" {
		return o.Host
	}
	return path.Join(o.Host, o.Run)
}

// overrideKey is the config key a user edits for bin's override, as every line spells it.
func overrideKey(bin string) string {
	return "programs.overrides." + bin + " in ~/.config/yolo-jail/config.jsonc"
}

// overrideLauncher renders bin's dev-override launcher. Every value is shquote'd into a bare
// position, the splice contract the other launchers keep. flags are the pack-declared launch
// flags for bin, injected as every other carrier injects them, so an override of an agent keeps the
// flags its pack runs it with.
func overrideLauncher(bin string, o ProgramOverride, flags *packload.LaunchInjection) string {
	prog := ""
	if o.Refused == "" {
		prog = path.Join(o.Dir, o.Run)
	}
	missing := "Cannot run it until that file exists: build it in that checkout"
	if strings.TrimSpace(o.Build) != "" {
		missing += " (the pack builds it with: " + o.Build + ")"
	}
	missing += ", or remove programs.overrides." + bin + " to run the pack's own copy."
	replacer := strings.NewReplacer(append([]string{
		"__YOLO_BIN__", shquote.Quote(bin),
		"__YOLO_PROG__", shquote.Quote(prog),
		"__YOLO_HOST_PROG__", shquote.Quote(o.RunPath()),
		"__YOLO_KEY__", shquote.Quote(overrideKey(bin)),
		"__YOLO_REFUSED__", shquote.Quote(o.Refused),
		"__YOLO_MISSING__", shquote.Quote(missing),
	}, launchFlagSplices(flags)...)...)
	return replacer.Replace(overrideLauncherTemplate)
}

// overrideLauncherTemplate is the launcher. It exits 127 on a refusal, the shell's "nothing to
// run" status, in every mode, so the readiness act records the refusal and an invocation fails
// rather than running something else.
const overrideLauncherTemplate = `#!/usr/bin/env bash
# yolo program launcher: a DEV OVERRIDE (programs.overrides in the user's config).
# It runs the checkout's file as found: no install, no build, and never the pack's own copy.
set -u
BIN=__YOLO_BIN__
PROG=__YOLO_PROG__
HOST_PROG=__YOLO_HOST_PROG__
KEY=__YOLO_KEY__
REFUSED=__YOLO_REFUSED__
MISSING=__YOLO_MISSING__
HAS_LAUNCH_FLAGS=__YOLO_HAS_LAUNCH_FLAGS__
LAUNCH_FLAGS=(__YOLO_LAUNCH_FLAGS__)
` + launchFlagsShellFn + `
if [ -n "$REFUSED" ]; then
    echo "yolo: $BIN: dev override refused: $REFUSED" >&2
    echo "  Cannot run $BIN from it: fix $KEY, or remove that entry to run the pack's own copy." >&2
    exit 127
fi
if [ ! -f "$PROG" ]; then
    echo "yolo: $BIN: dev override refused: $HOST_PROG does not exist ($KEY)." >&2
    echo "  $MISSING" >&2
    exit 127
fi
if [ ! -x "$PROG" ]; then
    echo "yolo: $BIN: dev override refused: $HOST_PROG is not executable ($KEY)." >&2
    echo "  Cannot run it until it is: chmod +x it in that checkout, or remove programs.overrides.$BIN to run the pack's own copy." >&2
    exit 127
fi
if [ "${` + InstallOnlyEnv + `:-}" = "1" ] || [ "${` + FreshRefreshEnv + `:-}" = "1" ]; then
    exit 0
fi
_yolo_launch_argv "$@"
exec "$PROG" ${YOLO_ARGV[@]+"${YOLO_ARGV[@]}"}
`
