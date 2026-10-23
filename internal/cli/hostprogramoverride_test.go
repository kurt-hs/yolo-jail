package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `yolo host` APPLIES A DEV OVERRIDE OF ANY PROGRAM (docs/design/pack-buildable-programs.md §5,
// BG-D8; the plan's Task 9), an npm agent's included: `yolo host --` and `yolo host env` say which file
// runs, in the jail's words, and the launch execs the floor's entry for it, which runs that file. An
// override naming no selected program says nothing. Red if the line leaves selectionLines.
func TestYoloHostAppliesAProgramOverrideOfAnNpmAgent(t *testing.T) {
	ws := t.TempDir()
	file := filepath.Join(ws, "cli.js")
	writeFile(t, file, "#!/bin/sh\necho dev\n")
	if err := os.Chmod(file, 0o755); err != nil {
		t.Fatal(err)
	}
	line := "claude: dev override — runs " + file + " (programs.overrides in ~/.config/yolo-jail/config.jsonc)"
	hostGateHome(t, `{"packs": ["claude"], "programs": {"overrides": {"claude": {"path": "`+ws+`", "run": "cli.js"}}}}`, nil)
	got := captureHostExec(t)
	var out, errw bytes.Buffer
	if rc := hostMain([]string{"--", "claude"}, &out, &errw, false, nil); rc != 0 || !got.execed ||
		!strings.Contains(errw.String(), "yolo host: "+line) || strings.Contains(errw.String(), "not applied") {
		t.Errorf("yolo host -- claude with an override: rc=%d execed=%v, want the line %q\n%s", rc, got.execed, line, errw.String())
	}
	if rec := floorRecord(t, "claude"); rec.Via != "override" || rec.Entry != file {
		t.Errorf("the floor's claude is %+v, want the override's file", rec)
	}
	var envOut, envErr bytes.Buffer
	if rc := hostMain([]string{"env", "--agent", "claude"}, &envOut, &envErr, false, nil); rc != 0 ||
		!strings.Contains(envErr.String(), line) {
		t.Errorf("yolo host env with an override: rc=%d, want the line\n%s", rc, envErr.String())
	}

	hostGateHome(t, `{"packs": ["claude"], "programs": {"overrides": {"nosuchbin": {"path": "`+ws+`", "run": "x"}}}}`, nil)
	if _, _, errw := hostExecRun(t, "claude"); strings.Contains(errw, "programs.overrides") {
		t.Errorf("an override naming no selected program was named:\n%s", errw)
	}
}
