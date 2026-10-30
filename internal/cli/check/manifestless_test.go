package check

// manifestless_test.go pins the CHECK notch of RC-D13's C6: `yolo check` accepts a pack folder
// with only `home/` and `overlays/` directories and no pack.json or pack.toml, staging both and
// reporting it clean. The loader discovers them (packload/conventions.go); this proves the
// Packs section consumes the discovery, so a bad `home/` destination fails here rather than at
// the launch.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func writeManifestlessFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManifestlessHomeAndOverlayFolderAtEveryNotch(t *testing.T) {
	good := t.TempDir()
	writeManifestlessFile(t, filepath.Join(good, "home", ".config", "mlnote", "hello.txt"), "hello\n")
	writeManifestlessFile(t, filepath.Join(good, "overlays", "claude-settings.jsonc"), "{ \"k\": 1 }\n")
	packsFixture(t, `{"packs":[{"name":"ml","source":"file://`+good+`"}]}`)

	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{}).sectionPacks(r, jsonx.NewOrderedMap())
	if r.failed != 0 || !strings.Contains(buf.String(), "ml: 2 file(s) stage") {
		t.Fatalf("a manifest-less home/ + overlays/ folder was not accepted and staged (failed=%d):\n%s", r.failed, buf.String())
	}

	// The same folder with a home/ file that lands on the jail's PATH is refused at check, by the
	// manifest path rules the loader applies to a discovered home file.
	bad := t.TempDir()
	writeManifestlessFile(t, filepath.Join(bad, "home", ".local", "bin", "run"), "#!/bin/sh\n")
	packsFixture(t, `{"packs":[{"name":"mlbad","source":"file://`+bad+`"}]}`)
	buf.Reset()
	r = &reporter{w: &buf}
	(&Options{}).sectionPacks(r, jsonx.NewOrderedMap())
	if r.failed == 0 || !strings.Contains(buf.String(), "jail's PATH") {
		t.Fatalf("a manifest-less pack's home/ destination on the PATH was not refused at check (failed=%d):\n%s", r.failed, buf.String())
	}

	// A malformed overlays/ (a directory where only direct files are allowed) is fatal at check.
	badOverlay := t.TempDir()
	writeManifestlessFile(t, filepath.Join(badOverlay, "overlays", "nested", "claude-settings.jsonc"), "{}\n")
	packsFixture(t, `{"packs":[{"name":"mlbadoverlay","source":"file://`+badOverlay+`"}]}`)
	buf.Reset()
	r = &reporter{w: &buf}
	(&Options{}).sectionPacks(r, jsonx.NewOrderedMap())
	if r.failed == 0 || !strings.Contains(buf.String(), "overlay files must be direct children") {
		t.Fatalf("a malformed manifest-less overlays/ was not refused at check (failed=%d):\n%s", r.failed, buf.String())
	}
}
