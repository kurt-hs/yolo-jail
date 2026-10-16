package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestTheJailsYoloCarriesTheCheckoutsCommit pins the owner's 2026-10-16 ruling
// (docs/plans/downstream-asks.md OQ-DA2): every build's version string carries the git
// hash, nix builds included. A launch from a live checkout builds the jail's binaries with
// `nix build .#installPrefix`, and flake.nix's buildStamp stamps them from the flake's own
// git metadata; before that ruling those binaries printed `commit: unknown`.
//
// It asks the binary the jail RUNS (`/bin/yolo`, a link into the mounted prefix), so
// deleting the -ldflags from either compiling derivation, or the stamp itself, fails it.
// The first `--version` line is not asserted: the launcher sets YOLO_VERSION in the jail,
// and that line prints the launcher's version, not the binary's.
func TestTheJailsYoloCarriesTheCheckoutsCommit(t *testing.T) {
	requireJail(t)
	root := os.Getenv("YOLO_REPO_ROOT")
	if root == "" {
		root = repoRoot
	}
	head, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		t.Skipf("the flake source %s is not a git checkout, so it carries a stamp file "+
			"rather than git metadata: %v", root, err)
	}
	want := strings.TrimSpace(string(head))[:12]

	dir := writeProject(t, "{}")
	r := runYoloDirect(t, dir, "yolo", "--version", "--full")
	if r.rc != 0 {
		t.Fatalf("yolo --version --full in the jail: rc=%d\n%s", r.rc, r.combined())
	}
	if !strings.Contains(r.stdout, "\ncommit: "+want+"\n") {
		t.Errorf("the jail's yolo does not carry the checkout's commit %s "+
			"(flake.nix buildStamp, goStampLdflags):\n%s", want, r.stdout)
	}
	if !strings.Contains(r.stdout, "build stamp: ") || !strings.Contains(r.stdout, "+g"+want) {
		t.Errorf("the jail's yolo build stamp does not carry +g%s:\n%s", want, r.stdout)
	}
}
