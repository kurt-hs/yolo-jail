package entrypoint

// configlistremove_test.go is the BEHAVIORAL proof that a config-list's `remove`, and its
// `profile` gate, are wired through the jail boot (ConfigurePackSurfaces → packoverlay.Collect
// → agentcfg.Compose) and the host apply (RenderHostPack): each test renders and reads the
// FILE the agent would read.
//
// The fixture is the acceptance case: an owner pack declaring pi/settings and a `lean`
// profile, and a personal pack whose profile-less config-overlay sets packages [A, B, C] and
// whose config-list, gated on `lean`, removes B.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packoverlay"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// removeOwnerPack owns pi/settings (no packages of its own) and declares the `lean` profile.
func removeOwnerPack(t *testing.T, mode string) *packload.Pack {
	t.Helper()
	surface := map[string]any{"agent": "pi", "name": "settings", "codec": "json",
		"path": "~/" + listSettings, "defaults": map[string]any{"theme": "dark"}}
	if mode != "" {
		surface["mode"] = mode
	}
	raw, err := json.Marshal([]any{surface})
	if err != nil {
		t.Fatal(err)
	}
	return &packload.Pack{Name: "pi", Decl: &packdecl.Manifest{
		Contributes: []packdecl.Contribution{
			{Kind: packdecl.KindProgram, Bin: "pi", Via: "npm", Package: "@acme/pi"},
			{Kind: packdecl.KindConfig, Raw: raw},
			{Kind: packdecl.KindProfile, Name: "lean", Provider: "bedrock"},
		},
	}}
}

// removePersonalPack overlays packages [A, B, C] with no profile, then declares lists.
func removePersonalPack(lists ...packdecl.Contribution) *packload.Pack {
	contributes := []packdecl.Contribution{{
		Kind: packdecl.KindConfigOverlay, Surface: "pi/settings",
		Raw: json.RawMessage(`{"managed":{"packages":["A","B","C"]}}`),
	}}
	return &packload.Pack{Name: "personal", Decl: &packdecl.Manifest{
		Contributes: append(contributes, lists...)}}
}

func gatedRemove(remove, add string) packdecl.Contribution {
	c := packdecl.Contribution{Kind: packdecl.KindConfigList, Surface: "pi/settings",
		Path: "/packages", Profile: "lean"}
	if remove != "" {
		c.Remove = json.RawMessage(remove)
	}
	if add != "" {
		c.Add = json.RawMessage(add)
	}
	return c
}

// bootWithProfiles boots the jail with the given profile selection ("" selects none) and
// returns the boot's stderr.
func bootWithProfiles(t *testing.T, profiles string, packs ...*packload.Pack) (*Env, string) {
	t.Helper()
	e, errw := overlayRenderEnv(t)
	if profiles != "" {
		e.Vars["YOLO_USE_PROFILES"] = profiles
	}
	bootJail(t, e, packs...)
	return e, errw.String()
}

// THE ACCEPTANCE CASE. The profile-less overlay alone yields [A, B, C]; the `lean`-gated
// remove of B renders [A, C] while lean is active for pi and [A, B, C] while it is not.
// Delete the profile gate in packoverlay.Collect and the inactive case loses B; delete the
// remove loop in agentcfg.applyListContributions, or the Remove field's trip through
// packdecl.ListContributions / packoverlay's NewListEdit, and the active case keeps it.
func TestJailConfigListProfileGatedRemove(t *testing.T) {
	t.Run("overlay alone", func(t *testing.T) {
		e, _ := bootWithProfiles(t, `{"pi":"lean"}`, removeOwnerPack(t, ""), removePersonalPack())
		wantPackages(t, e.Home, "A", "B", "C")
	})
	t.Run("profile active", func(t *testing.T) {
		e, _ := bootWithProfiles(t, `{"pi":"lean"}`, removeOwnerPack(t, ""),
			removePersonalPack(gatedRemove(`["B"]`, "")))
		wantPackages(t, e.Home, "A", "C")
	})
	t.Run("profile inactive", func(t *testing.T) {
		e, _ := bootWithProfiles(t, "", removeOwnerPack(t, ""),
			removePersonalPack(gatedRemove(`["B"]`, "")))
		wantPackages(t, e.Home, "A", "B", "C")
	})
}

// Removing an entry nothing holds changes nothing, and the boot says nothing about it — no
// failure (bootJail fails the test on one), no warning naming the entry.
func TestJailConfigListRemoveOfAbsentEntryIsSilent(t *testing.T) {
	e, stderr := bootWithProfiles(t, `{"pi":"lean"}`, removeOwnerPack(t, ""),
		removePersonalPack(gatedRemove(`["Z"]`, "")))
	wantPackages(t, e.Home, "A", "B", "C")
	if strings.Contains(stderr, `"Z"`) || strings.Contains(strings.ToLower(stderr), "warn") {
		t.Errorf("a remove of an absent entry was reported:\n%s", stderr)
	}
}

// Remove-then-add within one contribution: B is removed and re-appended, C is removed, D is
// appended — the order applyListContributions documents.
func TestJailConfigListRemoveThenAddInOneContribution(t *testing.T) {
	e, _ := bootWithProfiles(t, `{"pi":"lean"}`, removeOwnerPack(t, ""),
		removePersonalPack(gatedRemove(`["B","C"]`, `["B","D"]`)))
	wantPackages(t, e.Home, "A", "B", "D")
}

// AT AN `rmw` SURFACE a remove withdraws only what another contribution added: the file's own
// entries are the user's (pack-system.md#config-list-remove-at-rmw). An entry one pack adds
// and a later pack removes is never inserted; the owner's entry the remove also names stays.
func TestHostApplyRMWRemoveWithdrawsOnlyContributedEntries(t *testing.T) {
	home := t.TempDir()
	trim := &packload.Pack{Name: "trim", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindConfigList, Surface: "pi/settings", Path: "/packages",
		Remove: json.RawMessage(`["X","npm:owner-a"]`)}}}}
	applyHostPacks(t, home, render.OwnershipOwn, false, listOwnerPack(t, manifest.ModeRMW, nil),
		listContributorPack(t, "adder", "pi/settings", "/packages", "X", "Y"), trim)
	wantPackages(t, home, "npm:owner-a", "npm:owner-b", "Y")
}

// THE HOST HALF, on a `stateful` owner at an owned host: the same gate and the same removal,
// through RenderHostPack, collected the way `yolo host apply` collects (across the set, with
// the resolved profile table).
func TestHostApplyConfigListProfileGatedRemove(t *testing.T) {
	for _, c := range []struct {
		name     string
		profiles map[string][]string
		want     []any
	}{
		{"profile active", map[string][]string{"pi": {"lean"}}, []any{"A", "C"}},
		{"profile inactive", nil, []any{"A", "B", "C"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			packs := []*packload.Pack{removeOwnerPack(t, manifest.ModeStateful),
				removePersonalPack(gatedRemove(`["B"]`, ""))}
			set := packoverlay.Collect(packs, false, c.profiles)
			for _, p := range packs {
				if _, err := RenderHostPack(p, home, render.OwnershipOwn, false, set, nil); err != nil {
					t.Fatalf("RenderHostPack(%s): %v", p.Name, err)
				}
			}
			wantPackages(t, home, c.want...)
		})
	}
}
