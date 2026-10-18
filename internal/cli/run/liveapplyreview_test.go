package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// liveapplyreview_test.go pins the review's findings on `yolo config apply`: the plan comes from
// the config the gate approved, a changed mount is removed before it is added again, each step
// is bounded by its own deadline, and a mount change the host cannot see still has a line.

// THE MOUNTS COME FROM THE CONFIG THE GATE APPROVED (B1). The apply's own load and the gate's
// read are two reads of an agent-writable file: one that names a mount when yolo loads it and
// something harmless when the gate reads it must not get the mount injected under an approval
// of the harmless one. The apply refuses, names the rerun, and injects nothing; an attach starts
// the session all the same.
func TestConfigApplyRefusesAConfigThatChangedBetweenItsReads(t *testing.T) {
	for _, attach := range []bool{false, true} {
		name := "command"
		if attach {
			name = "attach"
		}
		t.Run(name, func(t *testing.T) {
			evil := t.TempDir()
			o, buf := applyFixture(t, `{}`, `{"mounts":["`+evil+`"]}`, applyJail{running: true})
			liveMounts(t)
			injected := 0
			swapSeam(t, &injectCtxMountSeam, func(*Options, string, string, configCtxMount) error {
				injected++
				return nil
			})
			// The flip, between the early load and the gate's read.
			swapSeam(t, &readWorkspaceForGateSeam, func(ws string) (*config.WorkspaceRead, error) {
				writeWorkspaceConfig(t, ws, `{"packages":["jq"]}`)
				return config.ReadWorkspaceForGate(ws)
			})
			answer(o, "y")
			var rc int
			if attach {
				merged, ok := o.loadAndValidateConfig()
				if !ok {
					t.Fatal(buf.String())
				}
				o.applyAtAttach("yolo-ws", "podman", merged, nil)
			} else {
				rc = o.configApply(false)
			}
			out := buf.String()
			if injected != 0 {
				t.Fatalf("a mount the gate never showed was injected:\n%s", out)
			}
			if !attach && rc != 1 {
				t.Errorf("rc = %d, want 1\n%s", rc, out)
			}
			if !strings.Contains(out, "changed while yolo was reading it") ||
				!strings.Contains(out, "yolo config apply") {
				t.Errorf("no refusal naming the rerun:\n%s", out)
			}
			if _, drifted, _, _ := config.WorkspaceConfigDrift(o.Workspace); !drifted {
				t.Error("a refused apply folded a key")
			}
		})
	}
}

// An unchanged config passes the same check: the read the gate approves composes to the
// config the apply loaded, so the mount is injected.
func TestConfigApplyAppliesWhenBothReadsAgree(t *testing.T) {
	src := t.TempDir()
	o, buf := applyFixture(t, `{}`, `{"mounts":["`+src+`"]}`, applyJail{running: true})
	liveMounts(t)
	injected := 0
	swapSeam(t, &injectCtxMountSeam, func(*Options, string, string, configCtxMount) error {
		injected++
		return nil
	})
	answer(o, "y")
	if rc := o.configApply(false); rc != 0 || injected != 1 {
		t.Fatalf("rc = %d, injected %d; want 0 and 1\n%s", rc, injected, buf.String())
	}
}

// writeUserConfigOf writes cfg as the user-scope config under $HOME, so a test that hands the
// apply a merged config hands it the one the gate's read composes to.
func writeUserConfigOf(t *testing.T, cfg *jsonx.OrderedMap) {
	t.Helper()
	s, err := jsonx.DumpsSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
