package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// heldTreeFailureFixture is a patched extension whose v1.0.0 good build is linked at the host and
// whose v1.2.0 does not take its series, the failure recorded.
func heldTreeFailureFixture(t *testing.T) *treeFixture {
	t.Helper()
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "")
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":false`)
	stubBins(t, "tool")
	var out, errw bytes.Buffer
	if rc := hostApplyRefreshAndRender(&out, &errw, false, true, nil, "", ""); rc != 0 || !isDir(fx.link()) {
		t.Fatalf("the first apply: rc=%d\n%s\n%s", rc, out.String(), errw.String())
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 11: "eleven"})
	fx.setNow(fx.now.Add(2 * time.Hour))
	if advanceHostTrees(io.Discard, false, "", nil) {
		t.Fatal("the advance that records the failure did not stop")
	}
	return fx
}

// A HOLD HOLDS FOR A PATCHED EXTENSION AT `yolo host -- <bin>` (§3.4, OQ-PBR5), with the launch's
// apply on and off: held, its recorded patch failure has no bearing while its good build serves, so
// the owner starts with no error block. Red with hostTreePatchFailureAllows not asking whether the
// extension is held.
func TestAHeldExtensionsRecordedFailureDoesNotStopTheHostLaunch(t *testing.T) {
	for name, apply := range map[string]string{"apply on launch off": `,"host_apply_on_launch":false`,
		"apply on launch on": `,"host_apply_on_launch":true`} {
		t.Run(name, func(t *testing.T) {
			fx := heldTreeFailureFixture(t)
			fx.writeHostConfig(t, treeHostOwn+apply+`,"agent_updates":{"treepack":false}`)
			got := captureHostExec(t)
			var errw bytes.Buffer
			rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil)
			if rc != 0 || !got.execed || strings.Contains(errw.String(), "ERROR: ") ||
				strings.Contains(errw.String(), "Start anyway:") {
				t.Errorf("the held extension's recorded failure stopped or marked the launch: rc=%d execed %v\n%s",
					rc, got.execed, errw.String())
			}
		})
	}
}

// THE APPLY-OFF EXTENSION REFUSAL at `yolo host -- <bin>` names its subject as an extension and lists
// every hatch that gets the launch running, from the one authority — the skip among them — with no
// prose under the list; and each hatch it lists does start the launch. Red with the stop's legacy
// one-hatch bypass.
func TestTheApplyOffExtensionRefusalListsEveryHatch(t *testing.T) {
	heldTreeFailureFixture(t)
	got := captureHostExec(t)
	var errw bytes.Buffer
	rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil)
	out := errw.String()
	if rc == 0 || got.execed {
		t.Fatalf("rc=%d execed %v, want a refusal\n%s", rc, got.execed, out)
	}
	for _, w := range []string{"ERROR: extension treepack/tool-ext: patch series does not apply to upstream v1.2.0",
		"\nStart anyway:\n  YOLO_ALLOW_PATCH_FAILURES=treepack/tool-ext yolo host -- tool ",
		"\n  YOLO_SKIP_EXTENSIONS=treepack/tool-ext yolo host -- tool "} {
		if !strings.Contains(out, w) {
			t.Errorf("the refusal lacks %q:\n%s", w, out)
		}
	}
	// No walk-back and no upstream build: this launch runs no advance, so neither would be built, and
	// YOLO_SKIP_PATCHES would start tool on the linked patched build its label says it does not run.
	if strings.Contains(out, "(the ERROR above)") || strings.Contains(out, "YOLO_ALLOW_OLDER_UPSTREAM") ||
		strings.Contains(out, "YOLO_SKIP_PATCHES") {
		t.Errorf("the refusal carries prose under its list, or a hatch this launch does not run:\n%s", out)
	}
	at := strings.Index(out, "Start anyway:")
	if at < 0 {
		t.Fatalf("no hatch list:\n%s", out)
	}
	for _, line := range strings.Split(out[at:], "\n")[1:] {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "YOLO_") {
			continue
		}
		env, value, _ := strings.Cut(strings.Fields(line)[0], "=")
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, value)
			got := captureHostExec(t)
			var errw bytes.Buffer
			if rc := hostExec(nil, []string{"tool"}, io.Discard, &errw, nil); rc != 0 || !got.execed {
				t.Errorf("the listed hatch %s=%s does not start the launch: rc=%d\n%s", env, value, rc, errw.String())
			}
		})
	}
}
