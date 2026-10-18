package run

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
			swapSeam(t, &injectCtxMountSeam, func(*Options, string, string, configCtxMount, time.Time) error {
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
	swapSeam(t, &injectCtxMountSeam, func(*Options, string, string, configCtxMount, time.Time) error {
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

// A SOURCE CHANGED AT THE SAME DESTINATION is a removal and an addition (diffCtxMounts), and
// the removal runs first (M1): the destination is the mount's identity in the jail, so an add
// run first finds it taken and the mount is lost. The jail ends with the new source.
func TestConfigApplyMovesAChangedSourceAtTheSameDestination(t *testing.T) {
	oldSrc, newSrc := t.TempDir(), t.TempDir()
	o, buf := applyFixture(t, `{"mounts":["`+oldSrc+`:/ctx/a"]}`, `{"mounts":["`+newSrc+`:/ctx/a"]}`,
		applyJail{running: true})
	rec := liveMounts(t)
	*rec = []liveCtxMount{{Source: oldSrc, Dest: "/ctx/a"}}
	mounted := map[string]string{"/ctx/a": oldSrc}
	swapSeam(t, &injectCtxMountSeam, func(_ *Options, _, _ string, m configCtxMount, _ time.Time) error {
		if _, taken := mounted[m.dest]; taken {
			return errors.New(m.dest + " is, or is under, a mount already; remove that first")
		}
		mounted[m.dest] = m.source
		return nil
	})
	swapSeam(t, &removeCtxMountSeam, func(_ *Options, _, _ string, e liveCtxMount, _ time.Time) error {
		delete(mounted, e.Dest)
		return nil
	})
	answer(o, "y")
	if rc := o.configApply(false); rc != 0 {
		t.Fatalf("rc = %d, want 0\n%s", rc, buf.String())
	}
	if mounted["/ctx/a"] != newSrc {
		t.Errorf("/ctx/a holds %q, want the new source %q\n%s", mounted["/ctx/a"], newSrc, buf.String())
	}
	if len(*rec) != 1 || (*rec)[0].Source != newSrc {
		t.Errorf("the host's mount record is %v, want the new source alone", *rec)
	}
}

// EVERY CALL A MOUNT STEP MAKES IS BOUNDED BY THE STEP'S DEADLINE (M2): nothing is left running
// past it in an abandoned goroutine, where it could mount after the record was written without
// the mount. The real driver runs against a fake podman whose verb never finishes.
func TestConfigApplyBoundsEveryCallOfAMountStep(t *testing.T) {
	src := t.TempDir()
	o, buf := applyFixture(t, `{}`, `{"mounts":["`+src+`"]}`, applyJail{running: true})
	liveMounts(t)
	o.PathExists = func(string) bool { return false }
	answeringPodman(o, rootlessInfo)
	swapSeam(t, &applyStepDeadline, 200*time.Millisecond)
	inner := o.Exec
	var stepTimeouts []time.Duration
	o.Exec = func(argv []string, dir string, env []string, timeout time.Duration) ExecResult {
		switch {
		case slices.Contains(argv, ctxAttachVerbName):
			stepTimeouts = append(stepTimeouts, timeout)
			return ExecResult{Ran: true, Timeout: true}
		case len(argv) > 3 && argv[1] == "inspect" && argv[3] == "{{.State.Pid}}":
			stepTimeouts = append(stepTimeouts, timeout)
			return ExecResult{Ran: true, Stdout: "4242\n"}
		}
		return inner(argv, dir, env, timeout)
	}
	answer(o, "y")
	if rc := o.configApply(false); rc != 3 {
		t.Fatalf("rc = %d, want 3\n%s", rc, buf.String())
	}
	if len(stepTimeouts) == 0 {
		t.Fatalf("the step ran nothing:\n%s", buf.String())
	}
	for _, d := range stepTimeouts {
		if d <= 0 || d > 200*time.Millisecond {
			t.Errorf("a step call ran under %v, past the step's 200ms deadline", d)
		}
	}
	if out := buf.String(); !strings.Contains(out, "at next restart") || !strings.Contains(out, "did not finish within") {
		t.Errorf("want at next restart naming the deadline:\n%s", out)
	}
}

// THE JAIL'S PID IS ASKED AGAIN AFTER THE VERB: the injector opens /proc/<pid>/ns/mnt and cannot
// know whose namespace that is, so a pid that changed while the step ran (the jail stopped, its
// pid reused) is an error, and the step is never recorded as the jail's.
func TestInjectCtxMountRefusesAPidThatChangedUnderTheStep(t *testing.T) {
	f := newCtxDriveFixture(t)
	f.o.acceptPodmanFacts(rootlessInfo)
	inner := f.o.Exec
	inspects := 0
	f.o.Exec = func(argv []string, dir string, env []string, timeout time.Duration) ExecResult {
		if len(argv) > 3 && argv[1] == "inspect" && argv[3] == "{{.State.Pid}}" {
			inspects++
			if inspects > 1 {
				return ExecResult{Ran: true, Stdout: "5151\n"}
			}
		}
		return inner(argv, dir, env, timeout)
	}
	err := f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "pid 4242, now 5151") {
		t.Errorf("err %v; want the changed pid named", err)
	}
	if argv, _ := f.verbCall(); argv == nil {
		t.Error("the verb never ran")
	}
}
