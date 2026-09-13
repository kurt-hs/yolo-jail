package run

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// THE TWO BACKENDS MUST RESOLVE ONE CONFIG INTO ONE INSTALL SET.
//
// This is the only package that can ask: `internal/macosuser` cannot import this one (the
// dependency runs the other way — see run/flock.go's AcquireWorkspaceLockFor), so a test
// living over there can compare the macos-user plan only against a literal. Here both
// spellings are in scope, and the recipe table they share now lives under them both in
// `internal/config` (config/lsp.go) rather than in this package's unexported state — which
// is what made the container the only backend that could compute the set at all, and is
// how macos-user shipped rendering `lsp_servers` into an agent's config while installing
// none of them.
//
// What it pins is the RESOLUTION, on both sides, from one config value. It does not reach
// the podman argv's own construction site (assembleInput is built inline in Run's
// pipeline); the golden in assemble_test.go pins the `-e` words themselves against a
// hand-built input.
func TestBothBackendsResolveTheSameLSPInstallSet(t *testing.T) {
	servers := jsonx.NewOrderedMap()
	servers.Set("python", jsonx.NewOrderedMap())
	servers.Set("go", jsonx.NewOrderedMap())
	cfg := jsonx.NewOrderedMap()
	cfg.Set("lsp_servers", servers)

	// The container's values: the two functions Run feeds straight into the
	// `-e YOLO_LSP_*_INSTALL=` words.
	wantNPM, wantGo := lspNPMOf(cfg), lspGoOf(cfg)
	if wantNPM == "" || wantGo == "" {
		t.Fatalf("the container path resolved nothing from a config declaring python and "+
			"go: npm=%q go=%q", wantNPM, wantGo)
	}

	plan := macosuser.BuildRunPlan("/Users/Shared/proj", cfg, []string{"claude"},
		[]string{"claude"}, "/opt/yolo-jail/bin/yolo", "", "", jsonx.NewOrderedMap(), nil, nil)

	// The bootstrap env — where the macos-user generators read the declared set.
	bootstrap := strings.Join(plan.BootstrapArgv, " ")
	for _, want := range []string{"YOLO_LSP_NPM_INSTALL=" + wantNPM, "YOLO_LSP_GO_INSTALL=" + wantGo} {
		if !strings.Contains(bootstrap, want) {
			t.Errorf("the macos-user bootstrap env does not carry %s, which is what the "+
				"container's podman argv emits for this config:\n%s", want, bootstrap)
		}
	}

	// The session env file — where the confined provisioning stage, the process that runs
	// the install, reads it.
	for _, pair := range [][2]string{
		{"YOLO_LSP_NPM_INSTALL", wantNPM},
		{"YOLO_LSP_GO_INSTALL", wantGo},
	} {
		if !macosuser.SandboxEnvFileSets(plan.EnvFileContent, pair[0], pair[1]) {
			t.Errorf("the macos-user session env file does not export %s=%s, so its stage "+
				"would install a different set than the container does:\n%s",
				pair[0], pair[1], plan.EnvFileContent)
		}
	}
}
