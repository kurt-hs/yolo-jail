package run

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// limitPack builds a pack declaring a MACHINE-scoped state dir and a reads-host grant —
// the two contributions whose macos-user behaviour differs from every other backend.
//
// `scope: "machine"` and not "workspace", which is the DP-B11 flip. The fixture declared
// the workspace tier until 2026-09-13 and the assertion below passed on it, which is what
// made the stale claim invisible: since entrypoint.InstallDarwinHomeLayout a
// `scope: workspace` dir is symlinked into <workspace>/.yolo/home and is NOT shared, so
// the old pairing asserted the "shared by every workspace" sentence about precisely the
// directories that are not. TestBackendLimitsDoNotCallWorkspaceStateShared below is the
// other half — it fails if the call site goes back to packload.WritableDirs.
func limitPack(t *testing.T) *packload.Pack {
	t.Helper()
	// MayAccessHost: HonoredHostFiles refuses a FETCHED pack's grants outright, so a
	// pack without it produces no ungranted list and the test would pass vacuously.
	return &packload.Pack{Name: "acme", Root: t.TempDir(), Decl: &packdecl.Manifest{
		Name: "acme",
		Contributes: []packdecl.Contribution{
			{Kind: packdecl.KindState, At: ".acme", Scope: "machine"},
			{Kind: packdecl.KindReadsHost, Host: ".acme/settings.json", From: ".acme/settings.json",
				Into: "settings.json"},
		},
	}}
}

// workspaceStatePack declares the OTHER tier and nothing else: one `scope: workspace`
// state dir, which the darwin home layout links into this workspace's own sidecar.
func workspaceStatePack(t *testing.T) *packload.Pack {
	t.Helper()
	return &packload.Pack{Name: "wsonly", Root: t.TempDir(), Decl: &packdecl.Manifest{
		Name: "wsonly",
		Contributes: []packdecl.Contribution{
			{Kind: packdecl.KindState, At: ".wsonly", Scope: "workspace"},
		},
	}}
}

// Container backends impose no standing constraints beyond what the rest of the
// briefing already says, so they get nothing — a section that always renders trains
// the reader to skip it.
func TestBackendLimitsAreEmptyForContainerBackends(t *testing.T) {
	for _, rt := range []string{"podman", "container"} {
		if got := backendLimits(rt, []*packload.Pack{limitPack(t)}, jsonx.NewOrderedMap()); len(got) != 0 {
			t.Errorf("%s: got %d limits, want none: %v", rt, len(got), got)
		}
	}
}

// The three facts an agent reasons WRONGLY from without them. Each is printed at
// launch to stderr, where the human reads it and the agent never does.
func TestBackendLimitsTellTheAgentWhatStderrTellsTheHuman(t *testing.T) {
	got := strings.Join(backendLimits("macos-user",
		[]*packload.Pack{limitPack(t)}, jsonx.NewOrderedMap()), "\n")

	// The home is machine-wide: an agent believing it is its own writes project state
	// into a directory every other workspace reads.
	if !strings.Contains(got, "SHARED by every workspace") {
		t.Errorf("does not say the home is shared:\n%s", got)
	}
	// Config rendered from DEFAULTS: an agent reading its own settings.json otherwise
	// takes it for the human's preferences and acts on them.
	if !strings.Contains(got, "DEFAULTS") {
		t.Errorf("does not say the agent config is not the human's:\n%s", got)
	}
	// Content is a writable copy: a skill the agent edits is silently overwritten.
	if !strings.Contains(got, "writable COPY") {
		t.Errorf("does not say content is a writable copy:\n%s", got)
	}
	// The in-jail loophole clients have nothing to talk to.
	if !strings.Contains(got, "yolo-ps") {
		t.Errorf("does not say the loophole clients are inert:\n%s", got)
	}
}

// A jail with no packs has no shared state, no ungranted host files and no copied
// content — so only the backend's own standing facts survive. A limit list that reported
// constraints a jail does not have would be the same overclaim in a new place.
func TestBackendLimitsScaleWithWhatIsActuallyThere(t *testing.T) {
	got := backendLimits("macos-user", nil, jsonx.NewOrderedMap())
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "SHARED by every workspace") {
		t.Errorf("claimed shared state dirs for a jail with no packs:\n%s", joined)
	}
	if strings.Contains(joined, "DEFAULTS") {
		t.Errorf("claimed ungranted host files for a jail with no packs:\n%s", joined)
	}
}

// DP-B11, the direction the old fixture could not see. A pack whose only state dir is
// `scope: workspace` gets that directory symlinked into <workspace>/.yolo/home by
// entrypoint.DeriveDarwinHomeLayout, so it is this workspace's alone — and naming it in a
// sentence that says "the same directories another workspace's session reads and writes"
// is not a vague overclaim but a specific false one, about the very dirs an agent writes
// its state into.
//
// This fails if the call site goes back to packload.WritableDirs, which is the whole point
// of asserting on the PACK rather than on the helper: WritableDirs and SharedDirs have the
// same signature and the same shape of answer, so only a fixture that distinguishes the
// two tiers can tell them apart.
func TestBackendLimitsDoNotCallWorkspaceStateShared(t *testing.T) {
	got := strings.Join(backendLimits("macos-user",
		[]*packload.Pack{workspaceStatePack(t)}, jsonx.NewOrderedMap()), "\n")

	if strings.Contains(got, "SHARED by every workspace") {
		t.Errorf("a scope:workspace state dir is linked into this workspace's own sidecar, "+
			"so calling it machine-wide tells the agent the opposite of what is true:\n%s", got)
	}
	if strings.Contains(got, ".wsonly") {
		t.Errorf("named a workspace-tier directory in the machine-tier sentence:\n%s", got)
	}
}

// §5.1.1 (3): the one network fact neither briefing paragraph states. Under host
// networking the briefing says `localhost` reaches the host and that no port mapping is
// needed — both true here and both incomplete, because they leave the usual container
// reading intact: that a listener is confined until something publishes it. There is no
// namespace on this backend, so binding IS publishing and `network.ports` pins nothing.
//
// It is unconditional: every macos-user jail has it, whether or not the config ever
// mentioned a port, because the fact is about the absence of a namespace rather than about
// any declaration.
func TestBackendLimitsSayBindingIsPublishingWithNoNamespace(t *testing.T) {
	for _, packs := range [][]*packload.Pack{nil, {limitPack(t)}} {
		got := strings.Join(backendLimits("macos-user", packs, jsonx.NewOrderedMap()), "\n")
		if !strings.Contains(got, "no network namespace") {
			t.Errorf("does not tell the agent there is no network namespace:\n%s", got)
		}
		if !strings.Contains(got, "real interfaces") {
			t.Errorf("does not say a bound port lands on the machine's real interfaces:\n%s", got)
		}
	}
	// And no container backend gains it: there the usual reading is the correct one.
	for _, rt := range []string{"podman", "container"} {
		if got := strings.Join(backendLimits(rt, nil, jsonx.NewOrderedMap()), "\n"); got != "" {
			t.Errorf("%s gained a standing limit: %s", rt, got)
		}
	}
}
