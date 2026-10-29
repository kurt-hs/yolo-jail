package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
)

// A HOST LAUNCH'S PATCH REFUSAL COLORS AGAINST ITS TERMINAL EVEN WITH ITS LOG OPEN: the host
// launch log tees stderr through a writer, but the user still sees the terminal styles. Its log
// keeps the same refusal without ANSI. Red if hostExec colors the tee instead of its destination.
func TestHostLaunchPatchFailureColorSurvivesTheLaunchLogTee(t *testing.T) {
	for _, tc := range []struct {
		name, noColor string
		terminal      bool
		wantANSI      bool
	}{
		{name: "terminal", terminal: true, wantANSI: true},
		{name: "NO_COLOR", noColor: "1", terminal: true},
		{name: "pipe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			fx := newTreeFixture(t, `"f.txt"`)
			fx.listTreeForAgent(t)
			fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":false`)
			stubBins(t, "tool")
			var applyOut, applyErr bytes.Buffer
			if rc := hostApplyRefreshAndRender(&applyOut, &applyErr, false, true, nil, "", ""); rc != 0 || !isDir(fx.link()) {
				t.Fatalf("initial host apply: rc=%d\n%s\n%s", rc, applyOut.String(), applyErr.String())
			}
			fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
			fx.setNow(fx.now.Add(2 * time.Hour))
			if advanceHostTrees(io.Discard, false, "", nil) {
				t.Fatal("the advance that records the patch failure did not stop")
			}
			got := captureHostExec(t)

			previousProbe := fileIsTerminal
			fileIsTerminal = func(f *os.File) bool {
				if tc.terminal {
					return true
				}
				return previousProbe(f)
			}
			t.Cleanup(func() { fileIsTerminal = previousProbe })

			var rc int
			shown := captureStderr(t, func() {
				rc = hostMain([]string{"--", "tool"}, io.Discard, os.Stderr, false, nil)
			})
			if rc == 0 || got.execed {
				t.Fatalf("recorded patch failure: rc=%d execed=%v\n%s", rc, got.execed, shown)
			}
			if strings.Contains(shown, "patch series does not apply") == false {
				t.Fatalf("host launch did not print the patch refusal:\n%s", shown)
			}
			if hasANSI := strings.Contains(shown, "\x1b["); hasANSI != tc.wantANSI {
				t.Errorf("terminal=%v NO_COLOR=%q: stderr carries ANSI=%v, want %v:\n%s",
					tc.terminal, tc.noColor, hasANSI, tc.wantANSI, shown)
			}
			if tc.wantANSI {
				for _, styled := range []string{
					"\x1b[1m\x1b[31mERROR: extension treepack/tool-ext: patch series does not apply",
					"\x1b[1mRepair:\x1b[0m",
					"\x1b[1m\x1b[36mYOLO_ALLOW_PATCH_FAILURES=treepack/tool-ext yolo host -- tool\x1b[0m",
				} {
					if !strings.Contains(shown, styled) {
						t.Errorf("terminal output lacks styled patch-failure text %q:\n%s", styled, shown)
					}
				}
			}
			log, err := os.ReadFile(run.HostLaunchLogPath())
			if err != nil {
				t.Fatalf("host launch log was not opened: %v", err)
			}
			if !strings.Contains(string(log), "patch series does not apply") {
				t.Errorf("host launch log lost the refusal:\n%s", log)
			}
			if strings.Contains(string(log), "\x1b[") {
				t.Errorf("host launch log contains ANSI:\n%s", log)
			}
		})
	}
}
