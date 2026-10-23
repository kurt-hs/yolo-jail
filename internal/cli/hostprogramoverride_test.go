package cli

import (
	"bytes"
	"strings"
	"testing"
)

// `yolo host` DOES NOT APPLY A DEV OVERRIDE YET (docs/design/pack-buildable-programs.md §5, the
// plan's Task 9), and says so rather than ignoring it: `yolo host --` and `yolo host env` name each
// overridden program a selected pack declares, in the macos-user notice's words. An override
// naming no selected program says nothing. Red if the line leaves selectionLines.
func TestYoloHostSaysItDoesNotApplyAProgramOverride(t *testing.T) {
	const line = "Warning: `programs.overrides` is not applied by `yolo host` yet — claude. " +
		"Each of these runs its pack's own copy; run a checkout from a podman jail."
	ws := t.TempDir()
	hostGateHome(t, `{"packs": ["claude"], "programs": {"overrides": {"claude": {"path": "`+ws+`", "run": "cli.js"}}}}`, nil)
	rc, reached, errw := hostExecRun(t, "claude")
	if rc != 0 || !reached || !strings.Contains(errw, "yolo host: "+line) {
		t.Errorf("yolo host -- claude with an override: rc=%d reached=%v, want the line %q\n%s", rc, reached, line, errw)
	}
	var out, envErr bytes.Buffer
	if rc := hostMain([]string{"env", "--agent", "claude"}, &out, &envErr, false, nil); rc != 0 ||
		!strings.Contains(envErr.String(), line) {
		t.Errorf("yolo host env with an override: rc=%d, want the line\n%s", rc, envErr.String())
	}

	hostGateHome(t, `{"packs": ["claude"], "programs": {"overrides": {"nosuchbin": {"path": "`+ws+`", "run": "x"}}}}`, nil)
	if _, _, errw := hostExecRun(t, "claude"); strings.Contains(errw, "programs.overrides") {
		t.Errorf("an override naming no selected program was named:\n%s", errw)
	}
}
