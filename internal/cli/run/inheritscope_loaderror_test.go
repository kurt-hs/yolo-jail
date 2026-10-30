package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

// A user config that cannot be loaded when the inherited scope is rendered reaches the user as a
// refusal that names the next step, never as an empty inner scope the launch proceeds with.
func TestInheritedScopeRefusesWhenTheConfigCannotBeLoaded(t *testing.T) {
	home, _ := inheritHome(t, "")
	// A TOML key of the wrong type is a load error even without strict mode, where a JSONC
	// parse failure is only a warning.
	if err := os.WriteFile(filepath.Join(home, ".config", "yolo-jail", "config.toml"), []byte("packs = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := inheritOptions(t)
	if _, _, err := config.LoadConfigWithoutWorkspaceFileWithSources(o.Workspace, false, func(string) {}); err == nil {
		t.Fatal("fixture: the config loads, so it does not reach effectiveConfigForInherit's error path")
	}
	selectLegacy := func(config.InheritScope, configdoc.Format) (bool, error) { return false, nil }
	files, err := o.inheritedScopeWithSelector("podman", true, selectLegacy)
	if err == nil {
		t.Fatalf("a config that cannot load produced %d inherited files and no error", len(files))
	}
	for _, want := range []string{"config", "yolo check"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
