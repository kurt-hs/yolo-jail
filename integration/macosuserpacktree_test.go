package integration

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
