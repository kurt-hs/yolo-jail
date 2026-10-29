---
title: "Inherited account relaunch implementation plan"
status: accepted
stage: DECIDED
next: "Implement unchanged sealed-hardlink admission and run the targeted regression green"
---

# Inherited account relaunch

Written against `8c0bc42f3a72e19b9f27a06f33b1fdf4a63c4679`, 2026-10-29.
Planner reproduced the reported JSONC refusal through two production Run calls and the real installer.
Behavior authority: the lane A owner-directed unchanged prior-delivery slice recorded below;
background contract: [inherited scope](../reference/loophole-system.md) and
[account home tiers](../reference/macos-user-home-tiers.md).
Precedence: settled behavior wins; the tree wins on cosmetic facts; this plan is advisory on structure.

## Settled behavior — do not re-open

**Constraint:** admit ONLY an unchanged prior-delivered hardlink snapshot. Preserve its inode,
exact bytes and sealed `0444` mode through the second Run and installer. No refresh or ownership claim.
Independent matching user inodes, copies, edited bodies/modes, links and ambiguous siblings remain
refused and preserved. No writable receipt, generated header or digest-only comparison proves ownership.

## Map and reuse

| Path | Change |
| :--- | :--- |
| [inheritscope.go](../../internal/cli/run/inheritscope.go) | Native selector admission, validated sealed-source lookup, preserve delivered paths in stale cleanup |
| [inheritscope_relaunch_test.go](../../internal/cli/run/inheritscope_relaunch_test.go) | New permanent production Run regression; extend with negative admission cases |
| [macosoverlaydispatch_test.go](../../internal/cli/run/macosoverlaydispatch_test.go) | Only minimal stale-cleanup expectation rewrite if required; ordinary owner refusals stay |

- Reuse `dispatchOptions`, `fakeMacosUserRun`, `overlayFileInfo` and `treeLaunchHome` from
  [dispatch fixtures](../../internal/cli/run/packstagedispatch_test.go),
  [backend fixture](../../internal/cli/run/housekeepingmacosuser_test.go),
  [overlay reuse tests](../../internal/cli/run/macoshomeoverlayreuse_test.go) and
  [patched-tree fixture](../../internal/cli/run/patchedtrees_test.go).
- Reuse real `DarwinEnvFrom` / `InstallHomeOverlay` as in
  [installer fixture](../../internal/cli/run/macoshomeoverlay_test.go), with selected synthetic packs.
- Reuse `StageHomeOverlayCommands`, `HomeOverlayStagedCurrent` and `StagedHomeOverlay` in
  [macosuser.go](../../internal/macosuser/macosuser.go); stable private sealed source across launches.
- Reuse `lstatHomeFile`, `selectInheritedJSONCRoute`, existing consumer-filtered rendering and
  exact family ambiguity checks in [inheritscope.go](../../internal/cli/run/inheritscope.go).

## Implementation task — after this planning commit

**Constraint:** limit production changes to the native inherited-scope path in
[inheritscope.go](../../internal/cli/run/inheritscope.go). No generic selector signature change,
container route change, new manifest, environment/config authority, installer behavior or shared Run edits.

**Advice:** a tiny package-private staging-root lookup defaults to `macosuser.StagingRoot`;
 derive the sealed source using `StagedHomeOverlay(runtime.FromWorkspace(ws), root)`.
This lets the permanent test route only its private synthetic root without touching global Mac state.
The regression currently has no production hook; wire it to that lookup when introducing it.
Use the workspace-derived session instead of its current synthetic session then.

**Constraint:** the admission query validates root facts through existing
`Options.MacosUserLaunchProbes`, `StagingRootFacts` and `JudgeStagingRoot` in
[privilege.go](../../internal/macosuser/privilege.go), using the caller euid and real default probes.
A missing/foreign/unsafe/unknown root fails closed. Never infer trust from Run preflight:
its failure can skip build and still leave inheritance before the backend's eventual refusal.

**Constraint:** no-follow every source and destination parent using contained root reads.
Require real sealed source directories `0555`, regular source/destination files `0444`,
caller-owned source where existing metadata exposes ownership, and `os.SameFile` identity.
Compare exact freshly rendered unstamped bytes even for the same inode: the host can edit/chmod both links.
Keep JSON/JSONC/TOML ambiguity checks and the existing safe next-step refusal.

**Advice:** keep the selector native-only: establish candidate identity, select the existing family
using the existing route helper, then compare returned rendered files before cleanup.
This avoids a second renderer and changing container consumers. Factoring is cheap; safety is mandatory.

**Constraint:** `removeStaleInheritedScope` must not remove any kept/delivered rel;
restrict cleanup to undelivered paths. Admitting then deleting the file defeats inode preservation.
The existing installer already takes its hardlink no-op; do not broaden generic overlay handling.
Its [receipt digest](../../internal/entrypoint/darwinoverlay.go) excludes modes and is agent-writable.

### Tests and gates

Baseline RED command (exit 1):

```bash
go test -count=1 -run '^TestMacosUserAccountRelaunchKeepsDeliveredInheritedJSONC$' ./internal/cli/run
```

Observed: first real install sealed/hardlinked the snapshot; second Run returned 1 with only one
backend callback, reporting `refusing macos-user inherited preflight scope` and
`could overwrite existing user file` at the sidecar config JSONC destination.
Delivered bytes, mode and inode remained intact. This is Linux synthetic caller proof, not native execution.

Add negative cases in the new test file: independent exact-body/mode inode, changed body,
header-preserving edit, changed mode (including same hardlink), final/parent link,
JSONC+TOML ambiguity, unsafe/foreign/missing staging root and missing/corrupt/forged writable receipt.
All refuse without modifying owner inode/bytes/mode/link. Dry-run mutates nothing.
Copied/cross-volume delivery remains conservatively refused, not claimed as fixed.

```bash
go test -short ./internal/cli/run ./internal/entrypoint
go test -count=1 -run '^(TestMacosUserInheritedUserScopeArrivesReadOnly|TestMacosUserOverlayInstallKeepsTheAgentStateBesideItsSkills)$' ./integration
python3 scripts/completion-census.py
```

The [native integration witnesses](../../integration/macosusercontent_test.go) and
[overlay-state witness](../../integration/macosuseroverlaystate_test.go) may skip on Linux;
record the skip, never native success. Root owns combined landing gates and full launch integration.
Planning census was attempted and refused this intentionally permanent RED; rerun and commit census
output after GREEN, without weakening or skipping the regression. Shared Go caches remain unchanged.

**Stop:** unexpected RED mechanism, dirty owned file, missing named symbol, required production
change outside the fence, or unrelated failing gate: report rather than expand scope.

Planning commit: `test(run): reproduce inherited account scope relaunch refusal`.
Implementation report: exact RED→GREEN command/results, changed paths, census receipt,
owner-file preservation negatives, and missing native/cross-volume witnesses.
No agents, login, API, keychain or owner-session actions.

## Permanent regression in full

```go
package run

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// A real first install must not turn an unchanged account relaunch into an owner-file
// refusal. This is synthetic account delivery, not native Seatbelt execution or refresh.
func TestMacosUserAccountRelaunchKeepsDeliveredInheritedJSONC(t *testing.T) {
	treeLaunchHome(t, true) // Object pack entries exercise RenderFilteredForDelivery's JSONC fallback.
	ws := floortest.ResolvedTemp(t)
	sidecar := paths.WorkspaceHomeState(ws)
	guestHome := floortest.ResolvedTemp(t)
	if err := os.MkdirAll(filepath.Join(sidecar, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sidecar, "config"), filepath.Join(guestHome, ".config")); err != nil {
		t.Fatal(err)
	}
	entries, err := config.LoadPacks(nil)
	if err != nil {
		t.Fatal(err)
	}
	var selected []*packload.Pack
	for _, entry := range entries {
		p, problems := packload.LoadDir(strings.TrimPrefix(entry.Source, "file://"), entry.Name)
		if p == nil || len(problems) != 0 {
			t.Fatalf("loading selected pack %s: %v", entry.Name, problems)
		}
		selected = append(selected, p)
	}
	stageRoot := floortest.ResolvedTemp(t)
	const session = "synthetic-account"
	sealed := macosuser.StagedHomeOverlay(session, stageRoot)
	// Production staging seals the source before the installer hardlinks it. Keep
	// that same private source across launches; never install from writable Run staging.
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", stageRoot).Run() })
	calls := 0
	launch := func() (int, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		o := dispatchOptions(t, ws, "macos-user", &stdout, &stderr, nil)
		o.MacosUserLaunchProbes = setUpMacProbes
		o.CapturesDir = func() string { return filepath.Join(stageRoot, "captures") }
		o.BuildSlot = deliverTreeCopyWithFile(t)
		o.MacosUserRun = fakeMacosUserRun(func(c macosUserCall) int {
			calls++
			if !slices.Contains(c.overlay.Dests, inheritPreflightLegacyRel) {
				t.Fatal("fixture did not render the inherited JSONC fallback")
			}
			if !macosuser.HomeOverlayStagedCurrent(c.overlay.Tree, session, stageRoot) {
				for _, argv := range macosuser.StageHomeOverlayCommands(c.overlay.Tree, session, stageRoot) {
					if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
						t.Fatalf("synthetic overlay staging %v: %v\n%s", argv, err, out)
					}
				}
			}
			e := entrypoint.DarwinEnvFrom(map[string]string{
				"HOME": guestHome, "JAIL_HOME": guestHome,
				entrypoint.DarwinHomeSidecarEnv: sidecar,
				"YOLO_DARWIN_HOME_OVERLAY":      sealed,
			}, guestHome)
			e.Stderr = &stderr
			if err := entrypoint.InstallHomeOverlay(e, selected); err != nil {
				t.Fatalf("real installer: %v\n%s", err, stderr.String())
			}
			return 0
		})
		return Run(*o), stderr.String()
	}
	if rc, stderr := launch(); rc != 0 || calls != 1 {
		t.Fatalf("first Run() = %d, callbacks = %d\nstderr:\n%s", rc, calls, stderr)
	}
	delivered := filepath.Join(sidecar, "config", "yolo-jail", "config.jsonc")
	before := overlayFileInfo(t, delivered)
	body, err := os.ReadFile(delivered)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0o444 || !os.SameFile(before, overlayFileInfo(t, filepath.Join(sealed, filepath.FromSlash(inheritPreflightLegacyRel)))) {
		t.Fatal("first install did not hardlink the sealed 0444 inherited snapshot")
	}
	rc, stderr := launch()
	after := overlayFileInfo(t, delivered)
	got, err := os.ReadFile(delivered)
	if err != nil || !bytes.Equal(body, got) || before.Mode() != after.Mode() || !os.SameFile(before, after) {
		t.Fatalf("relaunch changed delivered inode, mode or bytes: read error %v", err)
	}
	if rc != 0 || calls != 2 {
		t.Fatalf("unchanged second Run() = %d, callbacks = %d; want successful dispatch with preserved delivery\nstderr:\n%s", rc, calls, stderr)
	}
}
```
