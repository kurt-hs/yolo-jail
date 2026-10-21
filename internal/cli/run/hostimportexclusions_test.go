package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestRunCarriesSelectedHostImportPolicyIntoMacosUserBootstrap(t *testing.T) {
	home := packHome(t)
	configDir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.jsonc"), []byte(
		`{"packs":["claude"],"host_import_exclusions":{"claude/settings":["/hooks"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	const source = `{"hooks":[{"command":"host"}],"theme":"host"}`
	sourcePath := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, t.TempDir(), "macos-user", &stdout, &stderr, nil)
	var bootPolicy map[string][]string
	var planPolicy map[string][]string
	var planDelivered []string
	o.MacosUserRun = func(cfg *jsonx.OrderedMap, workspace string, agents, agentArgv []string,
		selfExe, hostPackRoot string, overlay macosuser.HomeOverlay, hostCtx macosuser.HostContext,
		_ bool, _ *jsonx.OrderedMap, _ []packload.BlockedTool, _ macosuser.JailDaemons) int {
		bootPolicy = hostCtx.HostImportExclusions
		if !containsString(hostCtx.Delivered, "/ctx/host-claude/settings.json") {
			t.Errorf("host context did not stage claude/settings: %v", hostCtx.Delivered)
		}
		plan := macosuser.BuildRunPlan(workspace, cfg, agents, agentArgv, selfExe, hostPackRoot,
			overlay, hostCtx, nil, nil, nil)
		for _, arg := range plan.BootstrapArgv {
			if value, ok := strings.CutPrefix(arg, packload.HostLayerEnvVar+"="); ok {
				wire, parsed := entrypoint.ParseHostLayerWire(value)
				if !parsed {
					t.Errorf("invalid bootstrap host-layer wire %q", value)
					continue
				}
				planPolicy = wire.HostImportExclusions
				planDelivered = wire.Delivered
			}
		}
		return 0
	}
	if rc := Run(*o); rc != 0 {
		t.Fatalf("Run() = %d\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	for name, got := range map[string]map[string][]string{"HostContext": bootPolicy, "bootstrap": planPolicy} {
		if pointers := got["claude/settings"]; len(pointers) != 1 || pointers[0] != "/hooks" {
			t.Errorf("%s policy = %v, want the normalized launch policy", name, got)
		}
	}
	if !containsString(planDelivered, "/ctx/host-claude/settings.json") {
		t.Errorf("macos-user host-layer wire dropped actual staged delivery: %v", planDelivered)
	}
	if got, err := os.ReadFile(sourcePath); err != nil || string(got) != source {
		t.Errorf("macos-user launch changed the host source: bytes=%q err=%v", got, err)
	}
}

func TestHostLayerEnvCarriesSelectedHostImportPolicy(t *testing.T) {
	policy := map[string][]string{
		"claude/settings": {"/hooks", "/statusLine"},
		"pi/settings":     {"/theme"},
	}
	selected := selectedHostImportExclusions(policy, claudePackFixture(t))
	in := &assembleInput{
		hostLayersDelivered:  []string{"/ctx/host-claude/settings.json"},
		hostImportExclusions: selected,
	}
	pairs := (&Options{}).hostLayerEnv(in)
	if len(pairs) != 2 || pairs[0] != "-e" || !strings.HasPrefix(pairs[1], packload.HostLayerEnvVar+"=") {
		t.Fatalf("host layer env = %q", pairs)
	}
	wire, ok := entrypoint.ParseHostLayerWire(strings.TrimPrefix(pairs[1], packload.HostLayerEnvVar+"="))
	if !ok {
		t.Fatalf("could not parse emitted host-layer wire: %s", pairs[1])
	}
	if got := wire.HostImportExclusions["claude/settings"]; len(got) != 2 || got[0] != "/hooks" || got[1] != "/statusLine" {
		t.Errorf("wire policy = %v, want selected normalized policy %v", got, policy["claude/settings"])
	}
	if _, ok := wire.HostImportExclusions["pi/settings"]; ok {
		t.Errorf("wire leaked a dormant unselected policy: %v", wire.HostImportExclusions)
	}
}
