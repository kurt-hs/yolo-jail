package packdecl

import (
	"strings"
	"testing"
)

// A RELATIVE patch branch is valid in the manifest, which knows no directory: its climb is bounded where the
// pack's directory is known (packsrc.ParseFrom).
func TestARelativePatchBranchValidatesStatically(t *testing.T) {
	c := goodPatchBranchFork()
	c.PatchBranch = "../..?ref=carry/pi-subagents"
	if probs := validateContribution("contributes[0]", c); len(probs) > 0 {
		t.Errorf("a relative patch branch is refused:\n%s", strings.Join(probs, "\n"))
	}
	c.PatchBranch = "../..?ref=HEAD"
	if joined := strings.Join(validateContribution("contributes[0]", c), "\n"); !strings.Contains(joined, "HEAD") {
		t.Errorf("a relative patch branch at HEAD is accepted:\n%s", joined)
	}
	c.PatchBranch = "file:///srv/pi?ref=mine"
	if joined := strings.Join(validateContribution("contributes[0]", c), "\n"); !strings.Contains(joined, "git+file:///srv/pi?ref=mine") {
		t.Errorf("a file:// patch branch with a ?ref= does not name the git form:\n%s", joined)
	}
}
