package integration

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// TestMacosUserANormalExitLeavesNoStagedPackTree is the guest pack tree's cleanup measured on a
// Mac (docs/reference/macos-user-provisioning.md, "The guest pack tree"): a launch that stages a
// pack tree and whose agent exits normally removes the tree on its way out, as the host user,
// and says nothing about it. Before the holder proof every launch kept its tree under
// <staging root>/packs and printed a paragraph about it.
func TestMacosUserANormalExitLeavesNoStagedPackTree(t *testing.T) {
	requireMacosUser(t)
	packHome(t, `{"packs": ["claude"]}`)
	ws := macosUserWorkspace(t, `{}`)
	packs := filepath.Join(macosuser.StateDir(), "packs")
	before := dirNames(t, packs)

	r := runMacosUser(t, ws, `/bin/sh -c 'id -un'`)
	if r.rc != 0 || !strings.Contains(r.stdout, macosuser.SandboxUser) {
		t.Fatalf("the launch did not run the command as %s (rc %d):\n%s", macosuser.SandboxUser, r.rc, r.combined())
	}
	for _, said := range []string{"Kept guest pack tree", "Retaining guest pack tree", "Could not remove guest pack tree"} {
		if strings.Contains(r.combined(), said) {
			t.Errorf("a normal exit said %q:\n%s", said, r.combined())
		}
	}
	for _, name := range dirNames(t, packs) {
		if !slices.Contains(before, name) {
			t.Errorf("the launch left its staged pack tree %s behind", filepath.Join(packs, name))
		}
	}
}

// TestMacosUserATrueSessionKeepsNoPackTree is `yolo -- true`, the shortest session there is: it
// says nothing about its guest pack tree and leaves none behind.
func TestMacosUserATrueSessionKeepsNoPackTree(t *testing.T) {
	requireMacosUser(t)
	packHome(t, `{"packs": ["claude"]}`)
	ws := macosUserWorkspace(t, `{}`)
	packs := filepath.Join(macosuser.StateDir(), "packs")
	before := dirNames(t, packs)

	r := runMacosUser(t, ws, `true`)
	if r.rc != 0 {
		t.Fatalf("`yolo -- true` failed (rc %d):\n%s", r.rc, r.combined())
	}
	for _, said := range []string{"Kept guest pack tree", "Could not remove guest pack tree"} {
		if strings.Contains(r.combined(), said) {
			t.Errorf("`yolo -- true` said %q:\n%s", said, r.combined())
		}
	}
	// Removed by the launch, or by the reaper it handed the tree to if something the session
	// started was still exiting: either way gone shortly after.
	if left := awaitNewTreesGone(packs, before, 30*time.Second); len(left) > 0 {
		t.Errorf("`yolo -- true` left its staged pack tree behind: %v\n%s", left, r.combined())
	}
}

// TestMacosUserASessionsLingeringProcessDoesNotKeepItsPackTree is the maintainer's report on
// `yolo -- pi --version`: a session that leaves a sandbox-account process running past the
// launch's few-second wait used to keep its guest pack tree for `yolo prune --apply`. The launch
// now hands the tree to the pack-tree reaper, which removes it once that process exits.
func TestMacosUserASessionsLingeringProcessDoesNotKeepItsPackTree(t *testing.T) {
	requireMacosUser(t)
	packHome(t, `{"packs": ["claude"]}`)
	ws := macosUserWorkspace(t, `{}`)
	packs := filepath.Join(macosuser.StateDir(), "packs")
	before := dirNames(t, packs)

	// Orphaned onto launchd, outliving the session by several seconds, holding no pipe of it.
	r := runMacosUser(t, ws, `( /bin/sleep 8 </dev/null >/dev/null 2>&1 & ); true`)
	if r.rc != 0 {
		t.Fatalf("the session failed (rc %d):\n%s", r.rc, r.combined())
	}
	if strings.Contains(r.combined(), "Kept guest pack tree") {
		t.Errorf("a lingering process kept the session's pack tree:\n%s", r.combined())
	}
	if !strings.Contains(r.combined(), "removed in the background") {
		t.Errorf("the launch did not say it handed its tree to the reaper:\n%s", r.combined())
	}
	if left := awaitNewTreesGone(packs, before, 60*time.Second); len(left) > 0 {
		log, _ := os.ReadFile(macosuser.PackTreeReapLog())
		t.Errorf("the reaper left the pack tree behind after the process exited: %v\nreaper log:\n%s\n%s",
			left, log, r.combined())
	}
}

// awaitNewTreesGone waits up to bound for every entry of packs not in before to be gone, and
// returns those still there.
func awaitNewTreesGone(packs string, before []string, bound time.Duration) []string {
	deadline := time.Now().Add(bound)
	for {
		var left []string
		entries, _ := os.ReadDir(packs)
		for _, e := range entries {
			if !slices.Contains(before, e.Name()) {
				left = append(left, filepath.Join(packs, e.Name()))
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// dirNames lists dir's entries; an absent dir has none.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("listing %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
