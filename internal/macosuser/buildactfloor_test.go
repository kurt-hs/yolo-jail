package macosuser

// buildactfloor_test.go pins what the sealed macos-user build act runs a build's script WITH
// (pi-extensions.md PPX-D46): the darwin floor's bash, never macOS's /bin/bash 3.2. The maintainer's
// Mac met it: a patched extension whose build line ran `shopt -s globstar && rm -f
// packages/**/*.test.ts` built in a container (the image's bash 5) and failed on macos-user with
// `shopt: globstar: invalid shell option name`.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// driverScriptArgv is the installer argv the capture driver runs, cut from a build's driver argv:
// from the /usr/bin/env after capture-run's `--` on.
func driverScriptArgv(t *testing.T, argv []string) []string {
	t.Helper()
	i := slices.Index(argv, "capture-run")
	if i < 0 {
		t.Fatalf("no capture-run in the driver argv: %q", argv)
	}
	j := slices.Index(argv[i:], "--")
	if j < 0 || i+j+1 >= len(argv) {
		t.Fatalf("no installer after capture-run's --: %q", argv)
	}
	return argv[i+j+1:]
}

// THROUGH THE ACT: the floor RunForkBuildAct builds is the bash the driver runs the script with, for a
// fork's build and a patched extension's alike, and /bin/bash is no word of the installer argv.
func TestTheBuildActRunsItsScriptUnderTheDarwinFloorsBash(t *testing.T) {
	for _, tree := range []bool{false, true} {
		name := map[bool]string{false: "a fork's build", true: "a patched extension's build"}[tree]
		t.Run(name, func(t *testing.T) {
			c := &captureDeps{}
			d := c.deps()
			d.MaterializeDarwin = func(string, []any) (*Darwin, bool, error) {
				return &Darwin{PathPrefix: []string{testProfile + "/bin"}, ProfilePath: testProfile,
					System: "aarch64-darwin"}, true, nil
			}
			opts, tmp := forkBuildActOptions(t)
			if tree {
				opts.Build = "shopt -s globstar && rm -f packages/**/*.test.ts"
				opts.TreeDir = packdecl.TreeReservedDir("pi-archimedes")
			}
			var driver []string
			run := forkBuildActRun(t, d, opts)
			d.Run = func(argv []string) int {
				if slices.Contains(argv, "capture-run") {
					driver = argv
				}
				return run(argv)
			}
			if rc := RunForkBuildAct(d, opts, filepath.Join(tmp, "store", "out"),
				filepath.Join(tmp, "store", "toolchain"), false); rc != 0 {
				t.Fatalf("RunForkBuildAct = %d\n%s", rc, c.out.String())
			}
			inst := driverScriptArgv(t, driver)
			want := testProfile + "/bin/bash"
			if len(inst) < 4 || inst[0] != "/usr/bin/env" || inst[2] != want || inst[3] != "-c" {
				t.Errorf("the build's script is not run by the floor's bash %s: %q", want, inst)
			}
			if slices.Contains(inst, "/bin/bash") {
				t.Errorf("the build's script reaches macOS's /bin/bash 3.2: %q", inst)
			}
		})
	}
}

// A PLAN WHOSE DRIVER RUNS /bin/bash IS REFUSED: the invariant fails when the call site naming the
// floor's bash is swapped for macOS's, or when the plan names a bash that is not its floor's.
func TestABuildPlanRunningMacOSsBashIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(p *ForkBuildPlan)
	}{
		{"the driver swapped to /bin/bash", func(p *ForkBuildPlan) {
			p.DriverArgv = ForkBuildDriverArgv(p.StagedYolo, p.StagingHome, p.OutDir, p.SrcDir, p.ToolchainFile,
				"", p.Build, p.ProfilePath, p.EnvFile, p.DarwinPathPrefix, "/bin/bash")
		}},
		{"a bash that is not the floor's", func(p *ForkBuildPlan) {
			p.Bash = "/bin/bash"
			p.DriverArgv = ForkBuildDriverArgv(p.StagedYolo, p.StagingHome, p.OutDir, p.SrcDir, p.ToolchainFile,
				"", p.Build, p.ProfilePath, p.EnvFile, p.DarwinPathPrefix, p.Bash)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := BuildForkBuildPlan(testForkBuildOptions())
			tc.mut(&plan)
			if problems := strings.Join(ForkBuildPlanInvariants(plan), "\n"); !strings.Contains(problems,
				"darwin floor's bash") {
				t.Errorf("the plan is not refused for its bash:\n%s", problems)
			}
		})
	}
}

// THE SCRIPT, RUN AS THE DRIVER RUNS IT: a floor whose bin/bash is a bash 4+ (this machine's, standing
// in for the floor's bashInteractive), the plan's own installer argv executed verbatim, and a build
// line using globstar removes what it names before the tree is copied.
func TestAGlobstarBuildLineRunsUnderTheFloorsBash(t *testing.T) {
	real, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	if _, err := os.Stat("/usr/bin/env"); err != nil {
		t.Skip("no /usr/bin/env on this machine")
	}
	if out, err := exec.Command(real, "-c", "shopt -s globstar").CombinedOutput(); err != nil {
		t.Skipf("this machine's bash has no globstar either: %s", out)
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	floor := filepath.Join(base, "floor")
	if err := os.MkdirAll(filepath.Join(floor, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(floor, "bin", "bash")); err != nil {
		t.Fatal(err)
	}
	opts := testTreeBuildOptions("shopt -s globstar && rm -f packages/**/*.test.ts")
	opts.CaptureRoot = filepath.Join(base, "captures")
	opts.Darwin = &Darwin{PathPrefix: []string{floor + "/bin"}, ProfilePath: floor}
	plan := BuildForkBuildPlan(opts)
	if problems := ForkBuildPlanInvariants(plan); len(problems) > 0 {
		t.Fatalf("the plan is not viable: %v", problems)
	}
	deep := filepath.Join(plan.SrcDir, "packages", "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"x.test.ts", "x.ts"} {
		if err := os.WriteFile(filepath.Join(deep, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	inst := driverScriptArgv(t, plan.DriverArgv)
	cmd := exec.Command(inst[0], inst[1:]...)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the build's script failed under the floor's bash: %v\n%s", err, out)
	}
	tree := filepath.Join(home, plan.TreeDir, "packages", "a", "b")
	if _, err := os.Stat(filepath.Join(tree, "x.test.ts")); err == nil {
		t.Error("the globstar build line removed nothing")
	}
	if _, err := os.Stat(filepath.Join(tree, "x.ts")); err != nil {
		t.Errorf("the tree lost what the build line kept: %v", err)
	}
}
