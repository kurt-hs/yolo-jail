package run

import (
	"slices"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

const graphicsGPUEnabledEnv = "YOLO_GPU_ENABLED"

func TestAssembleRunCmdHandsRequestedGPUStateToContainerBoot(t *testing.T) {
	sec := jsonx.NewOrderedMap()
	sec.Set("blocked_tools", []any{})
	home := t.TempDir()
	t.Setenv("HOME", home)
	emptyLoopholeDirs(t)
	o := goldenOptions("/ws", home)
	requested := jsonx.NewOrderedMap()
	requested.Set("enabled", true)
	in := relocationInput(t, "podman", "/ws/.yolo/home", nil)
	in.cfg.Set("gpu", requested)
	in.packs = claudePackFixture(t)
	got := o.assembleRunCmd(in)
	if !slices.Contains(got, graphicsGPUEnabledEnv+"=1") {
		t.Fatalf("assembled command does not explicitly pass requested GPU config: %v", got)
	}
	if !slices.Contains(knownLaunchYOLOEnvNames(nil, nil), graphicsGPUEnabledEnv) {
		t.Fatalf("generated GPU env %s is not recognized on nested launches", graphicsGPUEnabledEnv)
	}

	// Disabled config must beat a same-named value inherited by the host client.
	o.Getenv = func(key string) string {
		if key == graphicsGPUEnabledEnv {
			return "1"
		}
		return ""
	}
	requested.Set("enabled", false)
	got = o.assembleRunCmd(in)
	if !slices.Contains(got, graphicsGPUEnabledEnv+"=0") {
		t.Fatalf("disabled GPU did not explicitly clear inherited state: %v", got)
	}
}
