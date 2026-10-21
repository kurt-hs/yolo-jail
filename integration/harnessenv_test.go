package integration

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMatchedHarnessUsesAdjacentBundleAndScrubsObsoleteSelector(t *testing.T) {
	launcher := filepath.Join("/tmp", "dev generation", "bin", "yolo")
	wantBundle := filepath.Join("/tmp", "dev generation", "share", "yolo-jail")
	if got := matchedBundleFlakeRoot(launcher); got != wantBundle {
		t.Fatalf("matchedBundleFlakeRoot(%q) = %q, want %q", launcher, got, wantBundle)
	}
	buildOutput := "build-go: yolo -> dist-go/linux-amd64/yolo\n" +
		"stage-source-bundle: staged 100M bundle\n" + launcher + "\n"
	if got := matchedLauncherFromBuildOutput(buildOutput); got != launcher {
		t.Fatalf("matchedLauncherFromBuildOutput() = %q, want final absolute launcher path %q", got, launcher)
	}

	env := []string{"PATH=/bin", "YOLO_REPO_ROOT=/other/checkout", "HOME=/home/test"}
	got := envWithoutRepoRoot(env)
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/home/test"}) {
		t.Fatalf("envWithoutRepoRoot() = %v, want selector removed without changing other child environment", got)
	}
	if !slices.Contains(env, "YOLO_REPO_ROOT=/other/checkout") {
		t.Fatal("child sanitation mutated the caller's environment slice")
	}
}

// TestEveryLaunchTheSuiteMakesHasTheAutomaticReapersOff: TestMain turns yolo's automatic reapers
// off for every launch this suite makes (applySuiteEnv), and a test that exercises one turns them
// back on for its own launch (withAutoReapers). The launches are a stand-in yolo that prints what
// it was handed, run through runCommand, the helper every run* helper funnels through.
//
// The first assertion is vacuous where the calling shell already sets the variable, as it holds
// there anyway; CI's shell does not.
func TestEveryLaunchTheSuiteMakesHasTheAutomaticReapersOff(t *testing.T) {
	if got := os.Getenv(autoReapersOffEnv); got != "1" {
		t.Errorf("this test process has %s=%q, want 1: TestMain no longer turns the automatic "+
			"reapers off, so a launch here reaps images and store outputs through whatever "+
			"runtime and nix daemon the machine has, while other tests use them", autoReapersOffEnv, got)
	}
	dir := resolvedTempDir(t)
	fake := filepath.Join(dir, "fake-yolo")
	script := "#!/bin/sh\necho \"REAPERS_OFF=${" + autoReapersOffEnv + "-unset}.\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := yoloBin
	yoloBin = fake
	t.Cleanup(func() { yoloBin = saved })

	if r := runCommand(t, dir, []string{"run", "--", "true"}); !strings.Contains(r.stdout, "REAPERS_OFF=1.") {
		t.Errorf("a launch did not have the automatic reapers off: %q", r.stdout)
	}
	if r := runCommand(t, dir, []string{"run", "--", "true"}, withAutoReapers()); !strings.Contains(r.stdout, "REAPERS_OFF=.") {
		t.Errorf("withAutoReapers did not turn the automatic reapers back on: %q", r.stdout)
	}
}
