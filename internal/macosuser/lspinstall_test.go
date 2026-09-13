package macosuser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
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
		"/opt/yolo-jail/bin/yolo", "", "", HostContext{}, jsonx.NewOrderedMap(), nil, nil)
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
		[]string{"claude"}, "/opt/yolo-jail/bin/yolo", "", "", HostContext{}, jsonx.NewOrderedMap(), nil, nil)

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

// THE CONSUMER SIDE OF THE SAME WIRE, asked of THIS BACKEND'S OWN SCRIPT.
//
// ⚠ WHAT THIS IS NOT FOR, because the obvious reading of it is already covered and saying
// so is cheaper than the next reader re-deriving it. Renaming a variable inside the
// SHARED `bootstrapTemplate` (internal/entrypoint/shell.go) is caught without this test:
// MEASURED 2026-09-13 by mutating each arm in an isolated copy of the tree, the npm
// rename fails `TestLSPInstallsLeaveReceipts` and
// `TestLSPSentinelBytesAreUnchangedByTheReceiptHook`, and the go rename those two plus
// `TestLSPGoReceiptOmitsAnUnreadableVersion` and
// `TestLSPReceiptsAreNotWrittenAfterAFailedInstall` — all in internal/entrypoint, all
// driving the script with fakes. The producer half is covered too, by PlanInvariants and
// by the checks above.
//
// WHAT NOTHING ELSE COVERS is a DARWIN-ONLY divergence: the script the macos-user stage
// execs losing the install loop while the container's keeps it. Those receipt tests build
// a CONTAINER-shaped Env, so a `SkipMCPPresets`-style seam that empties an arm on this
// backend alone leaves every one of them green. That is not a hypothetical seam — it is
// the one this very file's header describes, and `SkipMCPPresets` is already exactly it
// for the MCP arm, three lines away in the same generator. MEASURED the same way: making
// the LSP npm loop read a dead variable when `SkipMCPPresets` is set fails THIS test and
// no other test in the tree.
//
// Which is why `SkipMCPPresets: true` in the helper below is load-bearing rather than
// scene-setting: it is what makes the generated script this backend's rather than the
// container's.
//
// It is deliberately a NAME check and not a value check. What the loop does with the list
// is the hardware oracle's question
// (integration/TestMacosUserDeclaredToolsArrive/lsp_servers); what is answerable from
// Linux is whether the two halves are still spelling the same variable at each other.
//
// Both ends are DERIVED. The names come out of the plan's own env file rather than from
// the literals above, so deleting the composition empties the set and fails here too
// instead of passing against a constant that no longer describes anything; and the script
// comes out of the real generator, fed the real bootstrap env this plan bakes.
func TestTheStagesScriptReadsTheInstallListsThePlanComposes(t *testing.T) {
	plan := lspPlan(t)

	var names []string
	for _, k := range SandboxEnvFileKeys(plan.EnvFileContent) {
		if strings.HasPrefix(k, "YOLO_LSP_") && strings.HasSuffix(k, "_INSTALL") {
			names = append(names, k)
		}
	}
	if len(names) != 2 {
		t.Fatalf("the session env file exports %d LSP install list(s), not the pair this "+
			"backend composes (%v). Either BuildRunPlan stopped composing them — in which "+
			"case the stage installs nothing and the checks above say why — or a THIRD "+
			"list was added and nothing here knows to look for it in the script.\n%s",
			len(names), names, plan.EnvFileContent)
	}

	// Keeps the temp sidecar below honest: the script this test generates is only evidence
	// about the stage if the stage resolves its script through the same function.
	if want := entrypoint.DarwinBootstrapScriptPath(paths.WorkspaceHomeState(plan.Workspace)); plan.ProvisionScriptPath != want {
		t.Fatalf("the stage execs %s, not the generator's path %s; the script generated "+
			"below is not the one that runs", plan.ProvisionScriptPath, want)
	}

	script := darwinStageScript(t, plan)
	for _, name := range names {
		if !shellReadsVar(script, name) {
			t.Errorf("the generated bootstrap script never reads $%s, which this launch's "+
				"session env file exports. The stage would source the file, exec the "+
				"script, and its install loop would iterate an empty list — the stage "+
				"exits 0 having installed no LSP server while the agent's config names "+
				"them. The loop is in internal/entrypoint/shell.go's bootstrapTemplate; "+
				"the producer is BuildRunPlan.", name)
		}
	}
}

// darwinStageScript generates the bootstrap script THIS PLAN's stage would exec, through
// the real generator (entrypoint.GenerateDarwinBootstrapScript) and out of the real
// bootstrap env the plan bakes onto its self-exec argv — which is the environment
// RunDarwinBootstrap builds its Env from on a Mac.
//
// The sidecar is redirected to a temp dir because the plan's is under /Users/Shared,
// which no test may write. That substitution is what the ProvisionScriptPath check in the
// caller guards.
func darwinStageScript(t *testing.T, plan RunPlan) string {
	t.Helper()
	// Resolved where the path is MINTED, per AGENTS.md's darwin PATH-RESOLUTION rule:
	// t.TempDir() is a /var/folders symlink on darwin and the generator does not resolve.
	sidecar, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temp sidecar: %v", err)
	}
	vars := map[string]string{}
	for _, a := range plan.BootstrapArgv {
		if k, v, ok := strings.Cut(a, "="); ok && isShellName(k) {
			vars[k] = v
		}
	}
	vars[entrypoint.DarwinHomeSidecarEnv] = sidecar

	// SkipMCPPresets is what RunDarwinBootstrap sets on this backend, and it changes what
	// the template renders — so a script generated without it is not this backend's.
	e := &entrypoint.Env{
		Home:           SandboxHome(),
		Workspace:      plan.Workspace,
		SkipMCPPresets: true,
		Vars:           vars,
	}
	if err := entrypoint.GenerateDarwinBootstrapScript(e); err != nil {
		t.Fatalf("generating the stage's bootstrap script: %v", err)
	}
	b, err := os.ReadFile(entrypoint.DarwinBootstrapScriptPath(sidecar))
	if err != nil {
		t.Fatalf("the generator wrote no script for the stage to exec: %v", err)
	}
	return string(b)
}

// shellReadsVar reports whether script dereferences the shell variable name, as `$name`
// or `${name…}`.
//
// The trailing-byte check is the whole of it: a plain substring search accepts
// `$YOLO_LSP_NPM_INSTALLED` for `YOLO_LSP_NPM_INSTALL`, and a near-miss spelling is
// exactly the regression this is here to catch rather than the one it should tolerate.
func shellReadsVar(script, name string) bool {
	for _, ref := range []string{"$" + name, "${" + name} {
		for i := 0; ; {
			j := strings.Index(script[i:], ref)
			if j < 0 {
				break
			}
			end := i + j + len(ref)
			if end >= len(script) || !isShellNameByte(script[end]) {
				return true
			}
			i = end
		}
	}
	return false
}

// isShellName reports whether s is a shell variable name, which is how an argv word is
// told from an environment assignment.
func isShellName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isShellNameByte(s[i]) {
			return false
		}
	}
	return true
}

func isShellNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Proves the check above is not vacuous — that it can tell the name it wants from the
// near-miss an incautious rename leaves behind. Without this, a shellReadsVar that
// returned true unconditionally would keep the test above green forever.
func TestShellReadsVarDistinguishesANearMiss(t *testing.T) {
	const name = "YOLO_LSP_NPM_INSTALL"
	for _, tc := range []struct {
		script string
		want   bool
	}{
		{`for pkg in $(printf '%s\n' "${YOLO_LSP_NPM_INSTALL:-}"); do`, true},
		{`echo $YOLO_LSP_NPM_INSTALL`, true},
		{`echo "${YOLO_LSP_NPM_INSTALL}"`, true},
		{`echo "${YOLO_LSP_NPM_INSTALLED:-}"`, false},
		{`echo $YOLO_LSP_NPM_INSTALL2`, false},
		{`# YOLO_LSP_NPM_INSTALL is only named in a comment`, false},
		{``, false},
	} {
		if got := shellReadsVar(tc.script, name); got != tc.want {
			t.Errorf("shellReadsVar(%q, %q) = %v, want %v", tc.script, name, got, tc.want)
		}
	}
}
