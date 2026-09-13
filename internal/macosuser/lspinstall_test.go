package macosuser

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// THE LINUX HALF OF RUNBOOK ITEM 9's `lsp_servers` SUBTEST.
//
// The hardware oracle is integration/TestMacosUserDeclaredToolsArrive/lsp_servers, which
// asks the only question that finally settles it: is `pyright-langserver` in the sandbox's
// npm prefix after a real launch? Nothing here can answer that. What IS answerable from
// Linux — and what was actually missing until 2026-09-13 — is whether the launch ever
// TELLS anything to install it: macos-user set `YOLO_LSP_SERVERS`, the table that renders
// an agent's LSP config, and neither of the two variables the installer reads. The stage
// execed the generated script, its loop iterated an empty list, and the stage exited 0
// while the agent's config named servers that were not on disk.
//
// ⚠ BOTH ENV LISTS, OR IT IS A HALF-FIX — which is why every assertion below comes in a
// pair. The readers are in different processes: the generated script's install loop
// (entrypoint/shell.go) runs in the CONFINED PROVISIONING STAGE, whose environment is the
// session env file; the catalog orphan finders (entrypoint/catalog.go) and the evergreen
// refresh set (entrypoint/serverrefresh.go) read the BOOTSTRAP env. Wiring one leaves a
// launch that looks green and still installs nothing.

// The frozen recipe outputs for the two servers exercised here — one npm arm, one go arm.
// Spelled as literals rather than re-derived from config.LSPInstalls, because a test that
// calls the same resolver as the code under test asserts only that a function is
// deterministic. `pyright` is the package whose `pyright-langserver` binary the hardware
// subtest looks for.
const (
	wantLSPNPMInstall = "pyright"
	wantLSPGoInstall  = "golang.org/x/tools/gopls@latest"
)

// lspPlan builds a plan for a workspace declaring the python and go LSP servers — the
// config shape the runbook uses, through the REAL plan builder rather than through a
// hand-assembled env.
func lspPlan(t *testing.T) RunPlan {
	t.Helper()
	servers := jsonx.NewOrderedMap()
	servers.Set("python", jsonx.NewOrderedMap())
	servers.Set("go", jsonx.NewOrderedMap())
	cfg := jsonx.NewOrderedMap()
	cfg.Set("lsp_servers", servers)
	return BuildRunPlan("/Users/Shared/proj", cfg, []string{"claude"}, []string{"claude"},
		"/opt/yolo-jail/bin/yolo", "", "", jsonx.NewOrderedMap(), nil, nil)
}

func TestDeclaredLSPServersReachBothEnvironments(t *testing.T) {
	plan := lspPlan(t)

	// HALF ONE — the bootstrap env, baked onto the self-exec argv. Its readers decide what
	// this launch DECLARED: an orphan finder that cannot see the list reports the servers
	// yolo just installed as unowned, and the refresh set skips them.
	for _, want := range []string{
		"YOLO_LSP_NPM_INSTALL=" + wantLSPNPMInstall,
		"YOLO_LSP_GO_INSTALL=" + wantLSPGoInstall,
	} {
		if !containsArg(plan.BootstrapArgv, want) {
			t.Errorf("the bootstrap env does not carry %s:\n%v", want, plan.BootstrapArgv)
		}
	}

	// HALF TWO — the session env file, which is the PROVISIONING STAGE's whole environment
	// and therefore the only way the generated script's install loop sees a list at all.
	for _, pair := range [][2]string{
		{"YOLO_LSP_NPM_INSTALL", wantLSPNPMInstall},
		{"YOLO_LSP_GO_INSTALL", wantLSPGoInstall},
	} {
		if !SandboxEnvFileSets(plan.EnvFileContent, pair[0], pair[1]) {
			t.Errorf("the session env file does not export %s=%s; the stage would exec the "+
				"generated script, find an empty install list and exit 0 having installed "+
				"nothing:\n%s", pair[0], pair[1], plan.EnvFileContent)
		}
	}

	// The file only reaches the stage if the stage READS it, and a config declaring
	// lsp_servers must produce a stage at all — the two links between half two and the
	// install actually happening.
	if len(plan.ProvisionArgv) == 0 {
		t.Fatal("a config declaring lsp_servers produced no provisioning stage; nothing " +
			"would run the generated script")
	}
	if !SandboxArgvReadsEnvFile(plan.EnvFile, plan.ProvisionArgv) {
		t.Errorf("the provisioning stage never reads the session env file (%s), so the "+
			"install lists in it are unreachable:\n%v", plan.EnvFile, plan.ProvisionArgv)
	}

	// THE CHANNEL IS THE FILE, NOT THE ARGV, and that is a rule rather than a detail: the
	// sandboxed argvs are under a closed allowlist so a new COMPOSED variable fails by
	// existing (SandboxArgvEnvProblems), which is what keeps the next secret off a
	// world-readable command line. These two are composed from the user's config.
	for _, argv := range [][]string{plan.ProvisionArgv, plan.LaunchArgv} {
		for _, word := range argv {
			if strings.HasPrefix(word, "YOLO_LSP_NPM_INSTALL=") ||
				strings.HasPrefix(word, "YOLO_LSP_GO_INSTALL=") {
				t.Errorf("a composed LSP install list rides a sandboxed argv as %q; it "+
					"belongs in the session env file", word)
			}
		}
	}

	if problems := PlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("the unmutated plan is not viable: %v", problems)
	}
}

// THE MUTATION HALVES. Each deletes ONE of the two crossings and asserts the plan stops
// being viable — the "does it fail if I delete the call site?" test AGENTS.md asks for,
// and the reason the invariant is worth carrying: both crossings are invisible to every
// other check in this package, and either one alone is a launch that installs nothing.
func TestPlanInvariantsRejectAHalfWiredLSPInstall(t *testing.T) {
	t.Run("bootstrap env dropped", func(t *testing.T) {
		plan := lspPlan(t)
		var kept []string
		for _, a := range plan.BootstrapArgv {
			if strings.HasPrefix(a, "YOLO_LSP_NPM_INSTALL=") ||
				strings.HasPrefix(a, "YOLO_LSP_GO_INSTALL=") {
				continue
			}
			kept = append(kept, a)
		}
		plan.BootstrapArgv = kept
		if problems := PlanInvariants(plan); len(problems) == 0 {
			t.Error("a plan whose bootstrap env names no LSP install list is reported as " +
				"viable; the orphan finders and the evergreen refresh would both be blind " +
				"to the launch's declared set")
		}
	})

	t.Run("session env file dropped", func(t *testing.T) {
		plan := lspPlan(t)
		// What deleting the sandboxEnv merge in BuildRunPlan produces: the bootstrap still
		// knows the set, and the process that installs it does not.
		plan.EnvFileContent = ""
		if problems := PlanInvariants(plan); len(problems) == 0 {
			t.Error("a plan whose session env file carries no LSP install list is reported " +
				"as viable; the provisioning stage would install nothing and exit 0")
		}
	})
}

// A workspace declaring NO LSP server must pay for none of this. The bootstrap still
// carries both names — the container emits both `-e` lines unconditionally, and one input
// shape for both backends is the rule the provider/profile wire tables are already under —
// while the env file stays empty, so a bare `yolo -- bash` still composes no environment
// and still writes no root-owned session file.
func TestNoDeclaredLSPServersComposesNothing(t *testing.T) {
	plan := BuildRunPlan("/Users/Shared/proj", jsonx.NewOrderedMap(), []string{"claude"},
		[]string{"claude"}, "/opt/yolo-jail/bin/yolo", "", "", jsonx.NewOrderedMap(), nil, nil)

	for _, want := range []string{"YOLO_LSP_NPM_INSTALL=", "YOLO_LSP_GO_INSTALL="} {
		if !containsArg(plan.BootstrapArgv, want) {
			t.Errorf("the bootstrap env does not carry %q; the container emits both lines on "+
				"every launch and the two backends' bootstraps must see one input shape:\n%v",
				want, plan.BootstrapArgv)
		}
	}
	if strings.Contains(plan.EnvFileContent, "YOLO_LSP_") {
		t.Errorf("a workspace declaring no LSP server composed an env file anyway:\n%s",
			plan.EnvFileContent)
	}
	if problems := PlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("a plan with no lsp_servers is not viable: %v", problems)
	}
}
