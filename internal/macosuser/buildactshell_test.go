package macosuser

// buildactshell_test.go pins what the sealed macos-user build act runs a build's script WITH
// (pi-extensions.md PPX-D46): the platform's own shell, macOS's /bin/bash. The maintainer ruled
// (2026-10-17) that a build for a platform runs on that platform's base system and a recipe must work
// there; yolo does not swap a base tool for a GNU or newer copy out of its darwin floor.

import (
	"os"
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

// THROUGH THE ACT: the driver RunForkBuildAct runs (BuildForkBuildPlan's argv) hands the build's
// script to macOS's /bin/bash, for a fork's build and a patched extension's alike, and takes no shell
// out of the darwin floor even when the floor is built.
func TestTheBuildActRunsItsScriptUnderThePlatformsOwnBash(t *testing.T) {
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
				opts.TreeDir = packdecl.TreeReservedDir("pi-ext")
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
			if len(inst) < 4 || inst[0] != "/usr/bin/env" || inst[2] != "/bin/bash" || inst[3] != "-c" {
				t.Fatalf("the build's script is not run by the platform's /bin/bash -c: %q", inst)
			}
			for _, w := range inst[:4] {
				if strings.HasPrefix(w, testProfile) {
					t.Errorf("the build's shell comes from the darwin floor (%s), not the base system: %q", w, inst)
				}
			}
		})
	}
}

// A BUILD LINE THAT RAN AND FAILED LEAVES ITS TOOLCHAIN RECORD where the host act reads it
// (PPX-D47): the script writes it first, so it is how internal/cli tells a failed build line from a
// sandbox that never reached it. Swept with the staging tree, the maintainer's failed build was
// said as "its build jail refused to start on the host". A driver that stopped before the script
// left none, and none is moved.
func TestAFailedBuildLineKeepsItsToolchainRecord(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record bool
	}{
		{"the build line ran and failed", true},
		{"the sandbox stopped before the script", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &captureDeps{}
			d := c.deps()
			d.MaterializeDarwin = func(string, []any) (*Darwin, bool, error) {
				return &Darwin{PathPrefix: []string{testProfile + "/bin"}, ProfilePath: testProfile}, true, nil
			}
			opts, tmp := forkBuildActOptions(t)
			plan := BuildForkBuildPlan(opts)
			run := d.Run
			d.Run = func(argv []string) int {
				if slices.Contains(argv, "capture-run") {
					run(argv)
					if tc.record {
						if err := os.WriteFile(plan.ToolchainFile, []byte("yolo 1.2.3 macOS 26.1"), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					return 1
				}
				return run(argv)
			}
			if err := os.MkdirAll(plan.StagingRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(tmp, "store", "toolchain")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if rc := RunForkBuildAct(d, opts, filepath.Join(tmp, "store", "out"), dest, false); rc == 0 {
				t.Fatalf("a failed build line's act exited 0\n%s", c.out.String())
			}
			_, err := os.Stat(dest)
			if moved := err == nil; moved != tc.record {
				t.Errorf("toolchain record beside the store = %v, want %v\n%s", moved, tc.record, c.out.String())
			}
		})
	}
}
