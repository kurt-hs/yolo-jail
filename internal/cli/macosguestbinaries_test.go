package cli

// macosguestbinaries_test.go pins that macos-user consumes only the complete Darwin helper
// set paired with the selected flake bundle and refuses missing helpers before guest staging.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

func TestMacosLaunchDepsWireTheGuestBinariesAndTheSupervisorStarter(t *testing.T) {
	deps := macosLaunchDeps(nil, nil)
	if deps.GuestBinaries == nil {
		t.Error("a macos-user launch has no guest-binary resolver")
	}
	if deps.StartBackground == nil {
		t.Error("a macos-user launch has no background starter for the jail-daemon supervisor")
	}
}

// writeGuestDir stages names into root's prebuilt guest dir, as a bundle would.
func writeGuestDir(t *testing.T, root string, names []string) string {
	t.Helper()
	dir := macosuser.PrebuiltGuestBinDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func asCheckout(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/mschulkind-oss/yolo-jail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveGuestBinariesPrefersTheBundlesPrebuiltDir(t *testing.T) {
	root := t.TempDir()
	dir := writeGuestDir(t, root, macosuser.GuestBinaries)
	got, err := resolveGuestBinaries(root)
	if err != nil || got != dir {
		t.Errorf("resolveGuestBinaries = %q, %v; want the prebuilt %q", got, err, dir)
	}
}

func TestResolveGuestBinariesRefusesWhenTheSourceShipsNone(t *testing.T) {
	root := t.TempDir()
	asCheckout(t, root)
	_, err := resolveGuestBinaries(root)
	assertGuestBundleRefusal(t, err, nil)
}

func TestResolveGuestBinariesRefusesACheckoutWithAStrayPartialDir(t *testing.T) {
	root := t.TempDir()
	asCheckout(t, root)
	writeGuestDir(t, root, []string{macosuser.JaildName})
	_, err := resolveGuestBinaries(root)
	assertGuestBundleRefusal(t, err, []string{macosuser.JaildName})
}

func TestResolveGuestBinariesRefusesABundleShortOfAMember(t *testing.T) {
	cases := map[string][]string{"empty": nil}
	for _, missing := range macosuser.GuestBinaries {
		var have []string
		for _, name := range macosuser.GuestBinaries {
			if name != missing {
				have = append(have, name)
			}
		}
		cases["without "+missing] = have
	}
	for name, have := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeGuestDir(t, root, have)
			_, err := resolveGuestBinaries(root)
			missing := make([]string, 0)
			for _, binary := range macosuser.GuestBinaries {
				if !slices.Contains(have, binary) {
					missing = append(missing, binary)
				}
			}
			assertGuestBundleRefusal(t, err, have)
			if err != nil {
				for _, binary := range missing {
					if !strings.Contains(err.Error(), binary) {
						t.Errorf("refusal does not name missing %s: %v", binary, err)
					}
				}
			}
		})
	}
	if !slices.Contains(macosuser.GuestBinaries, "yolo-serial") || !slices.Contains(macosuser.GuestBinaries, "yolo-ps") {
		t.Errorf("the guest set %v lacks a loophole client this test is for", macosuser.GuestBinaries)
	}
}

func assertGuestBundleRefusal(t *testing.T, err error, have []string) {
	t.Helper()
	if err == nil {
		t.Fatal("an incomplete guest helper set resolved")
	}
	for _, binary := range macosuser.GuestBinaries {
		if !slices.Contains(have, binary) && !strings.Contains(err.Error(), binary) {
			t.Errorf("refusal does not name missing %s: %v", binary, err)
		}
	}
	for _, want := range []string{"just build-dev-bundle", "printed", "launcher", "by path", "just install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal lacks actionable %q: %v", want, err)
		}
	}
}
