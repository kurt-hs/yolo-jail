package testconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestDefaultsUseTOMLAndLegacyIsExplicit(t *testing.T) {
	ws := Workspace(t, "packages = [\"just\"]\n")
	if filepath.Base(ws.Path) != paths.WorkspaceConfigTOMLName {
		t.Fatalf("workspace default path = %q", ws.Path)
	}
	legacy := WorkspaceFormat(t, configdoc.JSONC, `{"packages":["just"]}`)
	if filepath.Base(legacy.Path) != paths.WorkspaceConfigLegacyJSONCName {
		t.Fatalf("legacy path = %q", legacy.Path)
	}
	resolved, err := filepath.EvalSymlinks(ws.Root)
	if err != nil || resolved != ws.Root {
		t.Fatalf("fixture root %q not resolved: %q, %v", ws.Root, resolved, err)
	}
}

func TestUserAndManifestFixturesStayInsideTemporaryRoots(t *testing.T) {
	user := User(t, "packs = []\n")
	manifest := Manifest(t, "description = \"fixture\"\n")
	if filepath.Base(user.Path) != paths.UserConfigTOMLName || filepath.Base(manifest.Path) != paths.PackManifestTOMLName {
		t.Fatalf("unexpected paths: %q %q", user.Path, manifest.Path)
	}
	for _, fixture := range []Fixture{user, manifest} {
		resolved, err := filepath.EvalSymlinks(fixture.Root)
		if err != nil || resolved != fixture.Root {
			t.Fatalf("unresolved root %q: %q %v", fixture.Root, resolved, err)
		}
		if _, err := os.Stat(filepath.Join(fixture.Root, ".config", "yolo-jail", "config.jsonc")); err == nil {
			t.Fatal("fixture unexpectedly created the legacy user config")
		}
	}
}
