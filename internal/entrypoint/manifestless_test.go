package entrypoint

// manifestless_test.go pins the JAIL-BOOT notch of RC-D13's C6: a pack folder with only `home/`
// and `overlays/` directories and no pack.json or pack.toml. The boot renders its overlay into
// the surface another pack owns (ConfigurePackSurfaces → packoverlay.Collect), and the loader
// hands the launch the `files` contribution its home/ file becomes, the one the jail's :ro bind
// mount delivers (the mount itself is the launcher's, internal/cli/run).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestManifestlessHomeAndOverlayFolderAtEveryNotch(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"home/.config/mlnote/hello.txt": "manifest-less home file\n",
		"overlays/acme-settings.jsonc":  "{ // overlay\n \"mlOverlayKey\": \"from-overlay\" }\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ml, problems := packload.LoadDir(root, "ml")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}

	// The home/ file is a `files` contribution the launch binds at its own home path.
	var homeFile *packdecl.Contribution
	for _, c := range ml.Decl.Contributions() {
		if c.Kind == packdecl.KindFiles {
			c := c
			homeFile = &c
		}
	}
	if homeFile == nil {
		t.Fatalf("a manifest-less pack's home/ file became no `files` contribution: %+v", ml.Decl.Contributions())
	}

	e, _ := overlayRenderEnv(t)
	bootJail(t, e, overlayOwnerPack(t, ""), ml)
	got := readRenderedJSON(t, e.Home, ".acme/settings.json")
	if got["mlOverlayKey"] != "from-overlay" {
		t.Errorf("the manifest-less pack's overlays/acme-settings.jsonc did not reach the owner's surface at boot:\n%#v", got)
	}
	if got["theme"] != "system" {
		t.Errorf("the overlay clobbered a key it never named: theme=%v, want system", got["theme"])
	}
}
