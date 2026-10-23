package macosuser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A CROSSING INTO THE SANDBOX ACCOUNT STARTS IN `/`, never in the caller's cwd. sudo keeps the
// caller's, which is usually under the invoking user's home or per-user temp dir, where
// _yolojail cannot traverse: macos-user CI's capture driver printed `shell-init: error retrieving
// current directory: getcwd: cannot access parent directories: Operation not permitted`. Every
// real runner builds its command through commandFor, so this pins each crossing the backend
// makes, and that a host command keeps the caller's cwd.
func TestCommandForStartsACrossingInADirectoryTheSandboxCanRead(t *testing.T) {
	crossings := map[string][]string{
		"capture driver": CaptureDriverArgv(RunAs{}, "/s/bin/yolo", "/c/home", "/c/out", "jq", "/s/p.sb", "", nil),
		"grant probe":    RunAsGrantProbeArgv(),
		"set-home":       RunAsSandboxArgv([]string{"--set-home"}, "/bin/sh", "-c", "true"),
	}
	for name, argv := range crossings {
		if got := commandFor(argv).Dir; got != "/" {
			t.Errorf("%s: a crossing starts in %q, want / (argv %v)", name, got, argv)
		}
	}
	for _, argv := range [][]string{
		{"/bin/cp", "-R", "a", "b"},
		{sudoBin, "-n", "/usr/bin/true"},
		// `--user=` after env is the program's argument, not sudo's.
		{sudoBin, "-n", envBin, "--user=" + SandboxUser},
	} {
		if got := commandFor(argv).Dir; got != "" {
			t.Errorf("%v: a host command starts in %q, want the caller's cwd", argv, got)
		}
	}
}

// Does it fail if a call site is deleted? Each real runner's crossing goes through commandFor:
// a runner spelling exec.Command itself would let a crossing inherit the caller's cwd again.
func TestRealRunnersBuildCommandsThroughCommandFor(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(".", "real.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, fn := range []string{"func runOutputsTo(", "func startBackgroundReal(", "func runAsGrantReal("} {
		i := strings.Index(body, fn)
		if i < 0 {
			t.Fatalf("real.go has no %s", fn)
		}
		rest := body[i:]
		if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
			rest = rest[:j+1]
		}
		if !strings.Contains(rest, "commandFor(") || strings.Contains(rest, "exec.Command(") {
			t.Errorf("%s does not build its command through commandFor", fn)
		}
	}
}

// A RUN-AS-YOU CROSSING STARTS IN `/` as well, the account mode's guarantee kept in the other
// mode: the crossing runs under the deny-by-default profile, which opens `/` and the granted roots
// and not wherever `yolo` was typed. Each self-mode crossing the plan carries is checked, built
// through the real builders, so a self argv that stopped starting with /usr/bin/env, or a
// commandFor that stopped recognizing it, fails here.
func TestCommandForStartsASelfCrossingInRoot(t *testing.T) {
	plan := selfRunPlan(t, "/Users/you/code/alpha")
	for name, argv := range map[string][]string{
		"launch": plan.LaunchArgv, "bootstrap": plan.BootstrapArgv,
		"context preflight": RunAs{Mode: ModeSelf}.Argv(false, testBin, "-r", "/Users/Shared/data"),
	} {
		if got := commandFor(argv).Dir; got != "/" {
			t.Errorf("%s: a self-mode crossing starts in %q, want / (argv %v)", name, got, argv)
		}
	}
}
