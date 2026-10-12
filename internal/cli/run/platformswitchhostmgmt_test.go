package run

// platformswitchhostmgmt_test.go pins the jail notch's half of what a PP-D1 line says about a
// switch yolo's host apply wrote (docs/design/providers-and-profiles-redesign.md, ruled
// 2026-09-29). Since PP-D13 (2026-10-11) a jail leaves every provider key of the host copy out of
// what it renders, in every host_management posture, so the jail's line sends the user to no
// host-side removal at all — the host-side remedies (`yolo host apply`, `--revert`, removal by
// hand) are the `yolo host` launch's line alone (internal/cli's hostplatformswitch_test.go).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// THE ATTACH ARM, through deliverChannelOnAttach, so deleting InJail's assignment in
// notePlatformSwitchConflicts fails here in one direction or the other: the user config is read
// at the launch (config.HostManagementMode), never by packload.
func TestAnAttachNamesAYoloWrittenSwitchsRemovalByWhetherAHostApplyRendersHere(t *testing.T) {
	packs := []*packload.Pack{officialPack(t, "claude"), officialPack(t, "bedrock")}
	for _, tc := range []struct {
		name, userConfig string
		want, wantNot    []string
	}{
		{"unset", "", []string{"this jail leaves it out of claude's settings"},
			[]string{"by hand", "--revert", "or run `yolo host apply` with"}},
		{"none", `{"host_management": "none"}`, []string{"this jail leaves it out of claude's settings"},
			[]string{"by hand", "--revert", "or run `yolo host apply` with"}},
		{"own", `{"host_management": "own"}`, []string{"this jail leaves it out of claude's settings"},
			[]string{"by hand", "--revert", "or run `yolo host apply` with"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, cfg, channel, stderr := attachFixture(t, currentJailEnv, packs, emptyEnv(), nil)
			home := os.Getenv("HOME")
			if tc.userConfig != "" {
				writeUserConfig(t, home, tc.userConfig)
			}
			writeHostClaudeSettings(t, home, `{"env": {"CLAUDE_CODE_USE_BEDROCK": "1"}}`)
			rec := render.Host(home, nil, render.OwnershipUnstated).LeafRecordPath("claude", "settings")
			if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(rec, []byte(`{"/env/CLAUDE_CODE_USE_BEDROCK": "1"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			o.deliverChannelOnAttach("yolo-ws-abcd1234", "podman", cfg,
				stagedPacks{root: "/ctx/packs", packs: packs}, channel)
			got := stderr.String()
			if !strings.Contains(got, "which `yolo host apply` wrote there for claude's host selection") {
				t.Fatalf("no line naming the switch yolo wrote:\n%s", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("the line does not say %q:\n%s", w, got)
				}
			}
			for _, w := range tc.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("the line says %q:\n%s", w, got)
				}
			}
		})
	}
}
