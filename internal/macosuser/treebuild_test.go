package macosuser

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// --- a PATCHED EXTENSION's build (docs/reference/pi-extensions.md §7.1) ----------------------

func testTreeBuildOptions(build string) ForkBuildOptions {
	opts := testForkBuildOptions()
	opts.Build = build
	opts.TreeDir = packdecl.TreeReservedDir("pi-ext")
	return opts
}

// The driver of a tree build runs the TREE script, through the fork build's crossing: the build line
// in a subshell on lines of its own in the checkout, then the fixed copy of the checkout, links kept
// as links, into ~/<TreeDir>.
func TestATreeBuildPlanRunsTheBuildThenCopiesTheCheckout(t *testing.T) {
	plan := BuildForkBuildPlan(testTreeBuildOptions("npm ci # trailing comment"))
	if problems := ForkBuildPlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("a freshly built tree build plan is not viable: %v", problems)
	}
	if plan.TreeDir != packdecl.TreeReservedDir("pi-ext") {
		t.Errorf("plan.TreeDir = %q", plan.TreeDir)
	}
	argv := strings.Join(plan.DriverArgv, " ")
	src := shQuote(plan.SrcDir)
	for _, want := range []string{
		"/usr/bin/sandbox-exec -f " + plan.ProfilePath, "internal capture-run", "--home=" + plan.StagingHome,
		"/usr/bin/env YOLO_BYPASS_SHIMS=1 /nix/store/abc-profile/bin/bash -c",
		"cd " + src + " && (\nnpm ci # trailing comment\n)",
		` && /bin/mkdir -p "$HOME"/` + shQuote(packdecl.TreeReservedRoot),
		` && /bin/cp -pPR ` + src + ` "$HOME"/` + shQuote(packdecl.TreeReservedDir("pi-ext")),
		`printf ' node %s npm %s' "$(node --version 2>/dev/null)" "$(npm --version 2>/dev/null)"`,
		"printf '%s' 'yolo 1.2.3, darwin floor /nix/store/abc-profile'",
		`NPM_CONFIG_PREFIX="$HOME/.npm-global"`,
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the tree driver argv lacks %q:\n%s", want, argv)
		}
	}
	if strings.Contains(argv, "/bin/node") || strings.Contains(argv, "export PATH=/bin") {
		t.Errorf("the tree script reaches for the container's /bin/node, which macOS does not have:\n%s", argv)
	}
}

// A tree with no build line runs no build and is still copied, and its plan is viable.
func TestATreeWithNoBuildLineIsStillCopied(t *testing.T) {
	plan := BuildForkBuildPlan(testTreeBuildOptions(""))
	if problems := ForkBuildPlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("a tree build plan with no build line is not viable: %v", problems)
	}
	argv := strings.Join(plan.DriverArgv, " ")
	if strings.Contains(argv, "&& (") {
		t.Errorf("a tree with no build line runs a subshell:\n%s", argv)
	}
	if want := "cd " + shQuote(plan.SrcDir) + ` && /bin/mkdir -p "$HOME"/`; !strings.Contains(argv, want) {
		t.Errorf("the tree with no build line is not copied straight from its checkout (want %q):\n%s", want, argv)
	}
}

// THE CALL SITE IS PINNED: a tree plan whose driver lost its copy step, or runs the fork script, or
// whose TreeDir would land outside the staging home, is refused.
func TestTreeBuildPlanInvariantsRefuseEachMissingPiece(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(p *ForkBuildPlan)
		want string
	}{
		{"the copy step removed", func(p *ForkBuildPlan) {
			for i, a := range p.DriverArgv {
				p.DriverArgv[i] = strings.Replace(a, treeCopyStep(p.SrcDir, p.TreeDir), "", 1)
			}
		}, "never copies the checkout"},
		{"the fork script instead", func(p *ForkBuildPlan) {
			p.DriverArgv = ForkBuildDriverArgv(p.StagedYolo, p.StagingHome, p.OutDir, p.SrcDir, p.ToolchainFile,
				"", p.Build, p.ProfilePath, p.EnvFile, p.DarwinPathPrefix, p.Bash)
		}, "does not run the tree build"},
		{"an absolute tree dir", func(p *ForkBuildPlan) { p.TreeDir = "/" + p.TreeDir }, "not a plain home-relative"},
		{"an escaping tree dir", func(p *ForkBuildPlan) { p.TreeDir = "../outside" }, "not a plain home-relative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := BuildForkBuildPlan(testTreeBuildOptions("npm ci"))
			tc.mut(&plan)
			problems := strings.Join(ForkBuildPlanInvariants(plan), "\n")
			if !strings.Contains(problems, tc.want) {
				t.Errorf("problems lack %q:\n%s", tc.want, problems)
			}
		})
	}
}

// A FORK plan is not a tree plan: its driver is byte-for-byte the fork script's, and a fork plan
// handed a tree driver is refused.
func TestAForkPlanIsUnchangedByTheTreeSeam(t *testing.T) {
	opts := testForkBuildOptions()
	plan := BuildForkBuildPlan(opts)
	if plan.TreeDir != "" {
		t.Fatalf("a fork plan carries a tree dir %q", plan.TreeDir)
	}
	want := ForkBuildDriverArgv(plan.StagedYolo, plan.StagingHome, plan.OutDir, plan.SrcDir, plan.ToolchainFile,
		forkToolchainHead(opts), opts.Build, plan.ProfilePath, plan.EnvFile, plan.DarwinPathPrefix, plan.Bash)
	if !slices.Equal(plan.DriverArgv, want) {
		t.Errorf("the fork driver changed:\n%q\nwant\n%q", plan.DriverArgv, want)
	}
	plan.DriverArgv = TreeBuildDriverArgv(plan.StagedYolo, plan.StagingHome, plan.OutDir, plan.SrcDir,
		plan.ToolchainFile, "", opts.Build, packdecl.TreeReservedDir("x"), plan.ProfilePath, plan.EnvFile,
		plan.DarwinPathPrefix, plan.Bash)
	if problems := strings.Join(ForkBuildPlanInvariants(plan), "\n"); !strings.Contains(problems, "fork's build line") {
		t.Errorf("a fork plan running the tree script is not refused:\n%s", problems)
	}
}

// The dry run names the tree a tree build delivers.
func TestTheTreeBuildDryRunNamesTheTree(t *testing.T) {
	plan := BuildForkBuildPlan(testTreeBuildOptions("npm ci"))
	var buf bytes.Buffer
	PrintForkBuildPlan(&buf, plan, nil)
	if want := "~/" + packdecl.TreeReservedDir("pi-ext"); !strings.Contains(buf.String(), want) {
		t.Errorf("the dry run does not name %q:\n%s", want, buf.String())
	}
	buf.Reset()
	PrintForkBuildPlan(&buf, BuildForkBuildPlan(testForkBuildOptions()), nil)
	if strings.Contains(buf.String(), "tree:") {
		t.Errorf("a fork's dry run names a tree:\n%s", buf.String())
	}
}

// THE SCRIPT, RUN: a build line that `cd`s away and ends in a comment moves nothing of the copy,
// and the checkout lands at ~/<treeDir> with its relative links still links. Run with bash on this
// machine — the BSD-only pieces (sw_vers) fail soft into the record, which is written with `|| true`.
func TestTheTreeScriptCopiesTheCheckoutWithLinksKept(t *testing.T) {
	for _, bin := range []string{"/bin/bash", "/bin/cp", "/bin/mkdir"} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("%s is not on this machine", bin)
		}
	}
	base := t.TempDir()
	home, src := filepath.Join(base, "home"), filepath.Join(base, "src")
	for _, d := range []string{home, filepath.Join(src, "node_modules", "pkg"), filepath.Join(src, "node_modules", ".bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "node_modules", "pkg", "cli.js"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../pkg/cli.js", filepath.Join(src, "node_modules", ".bin", "cli")); err != nil {
		t.Fatal(err)
	}
	treeDir := packdecl.TreeReservedDir("pi ext")
	script := TreeBuildScript(src, filepath.Join(base, "toolchain"), "yolo test",
		"cd / && touch \"$OLDPWD/built\" # a trailing comment", treeDir)
	cmd := exec.Command("/bin/bash", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the tree script failed: %v\n%s", err, out)
	}
	tree := filepath.Join(home, treeDir)
	if _, err := os.Stat(filepath.Join(tree, "built")); err != nil {
		t.Errorf("the build line's output is not in the tree: %v", err)
	}
	link := filepath.Join(tree, "node_modules", ".bin", "cli")
	if got, err := os.Readlink(link); err != nil || got != "../pkg/cli.js" {
		t.Errorf("the link was not kept as a link: %q, %v", got, err)
	}
	rec, _ := os.ReadFile(filepath.Join(base, "toolchain"))
	if !strings.HasPrefix(string(rec), "yolo test macOS ") || !strings.Contains(string(rec), " node ") {
		t.Errorf("toolchain record = %q", rec)
	}
}

// THE PATCHED-EXTENSION DECISIONS REACH THE BOOTSTRAP (pi-extensions.md, PPX-D8): its file render
// takes the tree fallbacks and its launcher generation the gate from YOLO_PATCHED_TREES, so the
// launch's value is relayed onto the bootstrap's argv, and nothing is when the launch carried none.
func TestThePatchedTreeDecisionsReachTheBootstrap(t *testing.T) {
	const ws = "/Users/Shared/yolo/proj"
	wire := `{"pi:ext":{"skipped":true}}`
	plan := func(env *jsonx.OrderedMap) RunPlan {
		return BuildRunPlan(ws, jsonx.NewOrderedMap(), []string{"claude"}, []string{"/bin/zsh", "-l"},
			"/usr/local/bin/yolo", "", HomeOverlay{}, HostContext{}, env, nil, nil)
	}
	env := jsonx.NewOrderedMap()
	env.Set(entrypoint.PatchedTreesEnv, wire)
	if p := plan(env); !slices.Contains(p.BootstrapArgv, entrypoint.PatchedTreesEnv+"="+wire) {
		t.Errorf("the bootstrap is not told the patched-extension decisions: %q", p.BootstrapArgv)
	}
	for _, a := range plan(jsonx.NewOrderedMap()).BootstrapArgv {
		if strings.HasPrefix(a, entrypoint.PatchedTreesEnv+"=") {
			t.Errorf("a launch with no patched extension told the bootstrap %q", a)
		}
	}
}
