package integration

import (
	"strings"
	"testing"
)

// AN UNCHANGED RELAUNCH ON A MAC REPLACES NO DELIVERED FILE, and each one is the sealed staged file.
//
// The Linux gate pins every hop with fakes: the host overlay reused on an unchanged digest
// (run.TestAnUnchangedMacosUserRelaunchReusesTheHomeOverlay), no stage command for a current sealed
// copy (macosuser.TestALaunchStagesTheOverlayOnlyWhenItsStagedCopyIsStale), and the guest's hard
// links and inode check (entrypoint.TestOverlayInstallOfAnUnchangedStagedTreeReplacesNothing). What
// only a Mac can say: that the sandbox account may hard-link a file the host user owns from the
// staging root into its home on APFS — if it may not, every file falls back to a copy, which this
// test sees as a file the sandbox account owns and an inode that moves on every launch.
func TestMacosUserUnchangedRelaunchKeepsTheDeliveredFiles(t *testing.T) {
	requireMacosUser(t)
	packHome(t, `{"packs": ["claude"]}`)
	ws := macosUserWorkspace(t, `{}`)
	probe := `
skill=$(ls -d "$HOME"/.claude/skills/*/SKILL.md 2>/dev/null | head -n 1)
echo "=== DELIVERED ==="
printf 'skill|%s\n' "${skill:-NONE}"
printf 'inode|%s\n' "$(stat -f %i "$skill" 2>/dev/null || echo NONE)"
printf 'owner|%s\n' "$(stat -f %Su "$skill" 2>/dev/null || echo NONE)"
echo "=== END DELIVERED ==="
`
	var got [2]map[string]string
	for i := range got {
		r := runMacosUser(t, ws, probe)
		if r.rc != 0 {
			t.Fatalf("launch %d failed (rc %d)\nstdout:\n%s\nstderr:\n%s", i+1, r.rc, r.stdout, r.stderr)
		}
		got[i] = macosUserHomeProbeFields(t, r.stdout, "DELIVERED")
		if got[i]["skill"] == "NONE" || got[i]["inode"] == "NONE" {
			t.Fatalf("launch %d delivered no skill under ~/.claude/skills:\n%s", i+1, r.stdout)
		}
	}
	if got[0]["inode"] != got[1]["inode"] {
		t.Errorf("the relaunch replaced %s (inode %s, then %s): the guest re-delivered an unchanged "+
			"overlay", got[1]["skill"], got[0]["inode"], got[1]["inode"])
	}
	if owner := strings.TrimSpace(got[1]["owner"]); owner == "_yolojail" {
		t.Errorf("the delivered skill is the sandbox account's own file, so it is a copy, not a hard " +
			"link of the sealed staged file: linkat from the staging root was refused on this Mac")
	}
}
