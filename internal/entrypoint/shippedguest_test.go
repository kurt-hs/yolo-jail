package entrypoint

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// guestNixListRe and guestBashArrayRe capture the macos-user GUEST set (OQ-DP8): flake.nix's
// guestBinaries and stage-source-bundle.sh's GUEST_BINARIES.
var (
	guestNixListRe   = regexp.MustCompile(`(?m)^\s*guestBinaries\s*=\s*\[([^\]]*)\]\s*;`)
	guestBashArrayRe = regexp.MustCompile(`(?m)^GUEST_BINARIES=\(([^)]*)\)`)
)

// TestGuestSetAgreesAcrossFlakeAndBundleAndIsASubsetOfTheShipSet pins the darwin in-jail set a
// macos-user guest runs (bin/darwin-<arch>, docs/design/declaration-parity.md OQ-DP8) across the
// two shared binary lists: flake.nix's guestBinaries and the bundle script's GUEST_BINARIES. A
// name in one and not the other is a guest that a shipped bundle cannot run. Every guest name must
// also be a SHIPPED name — the guest runs the same program a container does, built for another
// OS — and `yolo` must never be in it: the sandbox self-execs the host's own darwin yolo, and a
// second copy in the guest dir would be two answers to "which yolo". The Go spelling,
// macosuser.GuestBinaries, is pinned to these in internal/macosuser (guestbundle_test.go), which
// this package cannot import.
func TestGuestSetAgreesAcrossFlakeAndBundleAndIsASubsetOfTheShipSet(t *testing.T) {
	root := repoRoot(t)
	flakeGuest := parseShipList(t, filepath.Join(root, "flake.nix"), guestNixListRe)
	bundleGuest := parseShipList(t, filepath.Join(root, "scripts", "stage-source-bundle.sh"), guestBashArrayRe)
	shipped := parseShipList(t, filepath.Join(root, "flake.nix"), nixListRe)
	for name := range flakeGuest {
		if !bundleGuest[name] {
			t.Errorf("%q is in flake.nix's guestBinaries but not in stage-source-bundle.sh's "+
				"GUEST_BINARIES — a shipped bundle's bin/darwin-<arch> would lack it", name)
		}
		if !shipped[name] {
			t.Errorf("%q is a guest binary but not a shipped one", name)
		}
	}
	for name := range bundleGuest {
		if !flakeGuest[name] {
			t.Errorf("%q is staged into bin/darwin-<arch> but not in flake.nix's guestBinaries, "+
				"so the flake's guest set and shipped bundle would disagree", name)
		}
	}
	if flakeGuest["yolo"] || bundleGuest["yolo"] {
		t.Error("`yolo` is in the guest set; the sandbox runs the host's own staged yolo")
	}
	if !flakeGuest["yolo-jaild"] {
		t.Error("yolo-jaild is not in the guest set, so no jail daemon can run on macos-user")
	}
}

func TestFlakeDoesNotExportAGuestBuildFallback(t *testing.T) {
	flake, err := os.ReadFile(filepath.Join(repoRoot(t), "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?m)^\s*packages\.guestPrefix\s*=`).Match(flake) {
		t.Fatal("flake.nix exports guestPrefix; macos-user must use the paired Darwin binaries in its selected bundle")
	}
}
