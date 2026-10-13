package cli

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestHostPreparationTimingThroughLaunchAndApply(t *testing.T) {
	for _, verb := range []string{"launch", "apply"} {
		for _, enabled := range []bool{true, false} {
			name := verb + " disabled"
			if enabled {
				name = verb + " enabled"
			}
			t.Run(name, func(t *testing.T) {
				fx := newTreeFixture(t, `"f.txt"`)
				fx.listTreeForAgent(t)
				fx.writeHostConfig(t, treeHostOwn+`,"host_apply_on_launch":true,"agent_updates":{"agentpack":"next-launch"}`)
				stubSpawn(t)
				t.Setenv(paths.TimingEnv, "")
				t.Setenv(paths.VerboseEnv, "")
				if !advanceHostTrees(io.Discard, false, "tool", nil) || len(fx.builds) != 1 {
					t.Fatal("failed to prepare admitted tree")
				}
				stubBins(t, "tool")
				if enabled {
					t.Setenv(paths.TimingEnv, "1")
				}
				var out syncBuffer
				var rc int
				if verb == "launch" {
					captureHostExec(t)
					rc = hostExec(nil, []string{"tool"}, io.Discard, &out, nil)
				} else {
					rc = hostApply([]string{"--assert"}, io.Discard, &out, false, strings.NewReader(""))
				}
				if rc != 0 || len(fx.builds) != 1 {
					t.Fatalf("unchanged consumption rc=%d builds=%d: %s", rc, len(fx.builds), out.String())
				}
				if enabled {
					log := hostPerfFile(t)
					assertSpansEnded(t, log, "preparation.registration", "preparation.admitted_read")
				} else if _, err := os.Stat(run.HostNotchPerfLogPath()); !os.IsNotExist(err) {
					t.Fatalf("disabled host act created timing log: %v", err)
				}
			})
		}
	}
}

func TestHostPreparationTimingObserveDoesNotClaimWork(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"agent_updates":{"agentpack":"next-launch"}`)
	t.Setenv(paths.TimingEnv, "1")
	t.Setenv(paths.VerboseEnv, "")
	var out syncBuffer
	if rc := hostApply(nil, io.Discard, &out, false, nil); rc != 0 {
		t.Fatalf("observe rc=%d: %s", rc, out.String())
	}
	if len(fx.builds) != 0 || strings.Contains(hostPerfFile(t), "preparation.registration") || strings.Contains(hostPerfFile(t), "preparation.admitted_read") {
		t.Fatal("observe claimed preparation work")
	}
}

func TestHostProgramAdmittedReadTimingAtPublicLaunchAndApply(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the admitted program qualifier deliberately supports Linux captures only")
	}
	for _, verb := range []string{"launch", "apply"} {
		t.Run(verb, func(t *testing.T) {
			fx := patchedFloorFixture(t)
			t.Setenv(paths.TimingEnv, "")
			t.Setenv(paths.VerboseEnv, "")
			if rc, _, out := fx.hostLaunch(t); rc != 0 {
				t.Fatalf("initial launch: %d %s", rc, out)
			}
			fx.writeUserConfig(t, `,"host_management":"own","agent_updates":{"forkpack":"next-launch"}`)
			t.Setenv(paths.TimingEnv, "1")
			before := len(fx.builds)
			if err := os.RemoveAll(paths.HostFloorDir()); err != nil {
				t.Fatal(err)
			}
			var rc int
			var out syncBuffer
			if verb == "launch" {
				rc, _, _ = fx.hostLaunch(t)
			} else {
				rc = hostApply([]string{"--assert"}, io.Discard, &out, false, strings.NewReader(""))
			}
			if rc != 0 || len(fx.builds) != before {
				t.Fatalf("unchanged %s: %d builds=%d %s", verb, rc, len(fx.builds), out.String())
			}
			assertSpansEnded(t, hostPerfFile(t), "preparation.admitted_read")
		})
	}
}

func TestHostPreparationTimingReportsActualRegistrationFailure(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.listTreeForAgent(t)
	fx.writeHostConfig(t, treeHostOwn+`,"agent_updates":{"agentpack":"next-launch"}`)
	previous := startBackgroundAdvance
	startBackgroundAdvance = func([]string, string) error { return errors.New("synthetic host registration failure") }
	t.Cleanup(func() { startBackgroundAdvance = previous })
	t.Setenv(paths.TimingEnv, "1")
	t.Setenv(paths.VerboseEnv, "")
	var out syncBuffer
	if rc := hostApply([]string{"--assert"}, io.Discard, &out, false, strings.NewReader("")); rc != 1 {
		t.Fatalf("failed callback did not refuse: %d %s", rc, out.String())
	}
	log := hostPerfFile(t)
	assertSpansEnded(t, log, "preparation.registration")
	if !strings.Contains(log, "selected host trees: failed: synthetic host registration failure") || strings.Contains(log, "preparation.admitted_read") || len(fx.builds) != 0 {
		t.Fatalf("failed registration claimed later work: %s", log)
	}
}

func TestHostPreparationTimingEmptySelectionDoesNotClaimWork(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	fx.writeHostConfig(t, treeHostOwn)
	writeFile(t, paths.UserConfigPath(), `{"packs":[],"host_management":"own"}`)
	t.Setenv(paths.TimingEnv, "1")
	t.Setenv(paths.VerboseEnv, "")
	var out syncBuffer
	if rc := hostApply([]string{"--assert"}, io.Discard, &out, false, strings.NewReader("")); rc != 0 {
		t.Fatalf("empty apply: %d %s", rc, out.String())
	}
	if len(fx.builds) != 0 || strings.Contains(hostPerfFile(t), "preparation.registration") || strings.Contains(hostPerfFile(t), "preparation.admitted_read") {
		t.Fatal("empty apply claimed preparation")
	}
}
