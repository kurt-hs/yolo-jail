package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
)

// Host `yolo check` says up front what a selected pack's broker may do (docs/design/boundary-broker.md
// BB-D108, BB-D109): with no read grant in the user config, a note that it grants nothing, naming
// the grant; with the grant, no note; and a switch a previous yolo wrote into the per-workspace
// file is reported as ignored, naming the grant and the command that takes it out.
func TestCheckSaysUpFrontWhatASelectedBrokerGrants(t *testing.T) {
	const note = "loophole gb runs in every workspace while its pack is selected, and grants nothing"
	t.Run("no grant", func(t *testing.T) {
		brokeredCheckHome(t)
		out := runCheckOverConfigWith(t, `{}`, false, nil, onTheHost)
		for _, want := range []string{note, "brokered.gbsrc.grants", `{"kind": "read", "repo": "*"}`, "yolo config apply"} {
			if !strings.Contains(out, want) {
				t.Errorf("yolo check does not say %q:\n%s", want, out)
			}
		}
	})
	t.Run("the user config's read grant", func(t *testing.T) {
		home := brokeredCheckHome(t)
		cfg := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
		must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
		must(t, os.WriteFile(cfg, []byte(`{"brokered": {"gbsrc": {"grants": [{"kind": "read", "repo": "*"}]}}}`), 0o644))
		out := runCheckOverConfigWith(t, `{}`, false, nil, onTheHost)
		if strings.Contains(out, note) {
			t.Errorf("yolo check says a granted broker grants nothing:\n%s", out)
		}
	})
	t.Run("an old per-workspace switch", func(t *testing.T) {
		brokeredCheckHome(t)
		out := runCheckOverConfigWith(t, `{}`, false, func(w string) {
			_, err := config.SetWorkspaceLoophole(w, "gb", true)
			must(t, err)
		}, onTheHost)
		for _, want := range []string{"loopholes.gb.enabled (on) is ignored", "`yolo loopholes disable gb --workspace "} {
			if !strings.Contains(out, want) {
				t.Errorf("yolo check does not say %q:\n%s", want, out)
			}
		}
	})
}
