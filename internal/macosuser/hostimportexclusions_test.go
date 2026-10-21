package macosuser

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestBuildRunPlanCarriesHostImportPolicyOnHostLayerWire(t *testing.T) {
	policy := map[string][]string{"claude/settings": {"/hooks"}}
	hostCtx := HostContext{
		Tree:                 "/ctx/staged-tree",
		Delivered:            []string{"/ctx/host-claude/settings.json"},
		HostImportExclusions: policy,
	}
	plan := BuildRunPlan("/Users/Shared/project", jsonx.NewOrderedMap(), nil, nil,
		"/usr/local/bin/yolo", "", HomeOverlay{}, hostCtx, nil, nil, nil)
	vars := bootstrapVars(t, plan.BootstrapArgv)
	wire, ok := entrypoint.ParseHostLayerWire(vars[packload.HostLayerEnvVar])
	if !ok {
		t.Fatalf("bootstrap does not carry a parseable host-layer wire: %v", plan.BootstrapArgv)
	}
	if got := wire.HostImportExclusions["claude/settings"]; len(got) != 1 || got[0] != "/hooks" {
		t.Errorf("macos-user wire policy = %v, want %v", got, policy["claude/settings"])
	}
}
