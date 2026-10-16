package loopholedecl_test

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

// scopedManifest is a minimal host_daemon declaration, so each test below varies exactly
// the field it is about.
func scopedManifest(overrides map[string]any) map[string]any {
	hd := map[string]any{
		"cmd":       []any{"d", "{socket}"},
		"publishes": "socket",
	}
	for k, v := range overrides {
		if v == nil {
			delete(hd, k)
			continue
		}
		hd[k] = v
	}
	return map[string]any{"name": "scoped", "description": "x", "host_daemon": hd}
}

// TestHostDaemonScopeDefaultsToJail: every manifest that declares nothing means "one daemon
// per jail, spawned and stopped with it", which since HD-R1 is the only meaning there is.
//
// The default lives in the DECODER, not at the readers, for `preamble`'s reason — that is
// what makes "no manifest declares anything to keep working" literally true rather than true
// of the one reader somebody checked.
func TestHostDaemonScopeDefaultsToJail(t *testing.T) {
	m, err := decodeMap(t, "scoped", map[string]any{
		"name": "scoped", "description": "x",
		"host_daemon": map[string]any{"cmd": []any{"d", "{endpoint}"}},
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.HostDaemon.Scope != loopholedecl.ScopeJail {
		t.Errorf("Scope = %q, want the default %q", m.HostDaemon.Scope, loopholedecl.ScopeJail)
	}
}

// TestHostDaemonScopeJailStillDecodes is the anti-overreach control: a manifest spelling the
// one value left keeps loading, under either publication shape.
func TestHostDaemonScopeJailStillDecodes(t *testing.T) {
	for _, publishes := range []string{"endpoint", "socket"} {
		m, err := decodeMap(t, "scoped", scopedManifest(map[string]any{
			"publishes": publishes, "scope": loopholedecl.ScopeJail,
		}))
		if err != nil {
			t.Fatalf("decode of scope=jail publishes=%q: %v", publishes, err)
		}
		if m.HostDaemon.Scope != loopholedecl.ScopeJail {
			t.Errorf("Scope = %q, want %q", m.HostDaemon.Scope, loopholedecl.ScopeJail)
		}
	}
}

// TestHostDaemonScopeHostIsRetired is HD-R1 at the manifest: `scope: "host"` named the
// machine-wide daemon every jail shared, which no launch of this yolo starts. A manifest still
// declaring it is refused at load, and the refusal names the next step — delete the key, and
// declare `detach` if the daemon's work in flight must outlive its jail — rather than the key
// silently meaning per-jail, so the manifest's author learns the daemon's lifetime changed.
func TestHostDaemonScopeHostIsRetired(t *testing.T) {
	_, err := decodeMap(t, "scoped", scopedManifest(map[string]any{"scope": loopholedecl.RetiredScopeHost}))
	if err == nil {
		t.Fatal("decode accepted the retired scope=\"host\"")
	}
	for _, want := range []string{"host_daemon.scope", "retired", "Delete 'scope'", "'detach': true"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lacks %q: %s", want, err.Error())
		}
	}
}

// TestHostDaemonScopeRefusesAnUnknownValue: the vocabulary is CLOSED, like every other enum
// here, and the refusal lists what is accepted.
func TestHostDaemonScopeRefusesAnUnknownValue(t *testing.T) {
	for _, bad := range []string{"machine", "singleton", "Host", "global"} {
		_, err := decodeMap(t, "scoped", scopedManifest(map[string]any{"scope": bad}))
		if err == nil {
			t.Fatalf("decode accepted scope=%q", bad)
		}
		if !strings.Contains(err.Error(), "host_daemon.scope") || !strings.Contains(err.Error(), "'jail'") {
			t.Errorf("error does not name the key and the accepted value: %s", err.Error())
		}
	}
}

// TestHostDaemonScopeTypoIsUnknown: `scop`, `host_scope`, `singleton` are UNKNOWN KEYS, so
// the strict decode reports them.
func TestHostDaemonScopeTypoIsUnknown(t *testing.T) {
	for _, key := range []string{"scop", "scopes", "host_scope", "singleton"} {
		man := scopedManifest(map[string]any{key: "host"})
		if _, err := decodeMap(t, "scoped", man); err == nil {
			t.Errorf("strict decode accepted the unknown host_daemon key %q", key)
		}
	}
}

// TestValidScopesIsTheClosedSet keeps the accessor honest: it is what the "not in [...]"
// message renders.
func TestValidScopesIsTheClosedSet(t *testing.T) {
	got := loopholedecl.ValidScopes()
	if len(got) != 1 || got[0] != loopholedecl.ScopeJail {
		t.Errorf("ValidScopes() = %v, want [jail]", got)
	}
	// A copy, not the package's own slice — the accessor exists so a caller cannot mutate
	// the vocabulary.
	got[0] = "mutated"
	if loopholedecl.ValidScopes()[0] != loopholedecl.ScopeJail {
		t.Error("ValidScopes() hands out the package's own slice")
	}
}

// THE VERSION BOUNDARY READS THE RETIRED VALUE, the authoring decoder still refuses it. Every
// jail an older yolo launched booted shipped manifests declaring `scope: "host"`, and an attach
// to one reads that jail's own frozen tree: refusing the value there made the jail's credential
// loopholes vanish from the host-side records and told the user to edit a shipped manifest in a
// running jail's tree. Read tolerantly, the value means what every daemon now is — one per jail
// — and a skew note says so, naming both next steps.
func TestTheTolerantDecoderReadsTheRetiredHostScopeAsPerJail(t *testing.T) {
	m, skipped, err := loopholedecl.DecodeTolerant(
		manifestBytes(t, scopedManifest(map[string]any{"scope": loopholedecl.RetiredScopeHost})),
		"/loopholes/scoped")
	if err != nil || m == nil {
		t.Fatalf("the tolerant decode refused the retired scope=\"host\": %v", err)
	}
	if m.HostDaemon.Scope != loopholedecl.ScopeJail {
		t.Errorf("Scope = %q, want it read as %q", m.HostDaemon.Scope, loopholedecl.ScopeJail)
	}
	note := strings.Join(skipped, "\n")
	for _, want := range []string{"host_daemon.scope", "retired", "one per jail", "Delete 'scope'",
		"launched by an older yolo"} {
		if !strings.Contains(note, want) {
			t.Errorf("the skew note lacks %q: %q", want, note)
		}
	}
	if _, err := decodeMap(t, "scoped", scopedManifest(map[string]any{"scope": loopholedecl.RetiredScopeHost})); err == nil {
		t.Error("the strict (authoring) decoder now accepts scope=\"host\"; it must still refuse it")
	}
}

// TestDecodeReleasedReadsOnlyTheRetiredScope pins the release wiring's reader of a released
// tree: a manifest from before HD-R1 declaring `scope: "host"` decodes, as one per jail, while
// every other strict refusal stands, so the reader is not the tolerant decoder that would also
// skip a key this build does not know.
func TestDecodeReleasedReadsOnlyTheRetiredScope(t *testing.T) {
	body := []byte(`{"name": "probe", "transport": "none", "host_daemon": {"cmd": ["probe-daemon"], "scope": "host"}}`)
	if _, err := loopholedecl.Decode(body, probeDir); err == nil {
		t.Fatal("the strict decoder accepted the retired scope; the test no longer exercises DecodeReleased's one exception")
	}
	m, err := loopholedecl.DecodeReleased(body, probeDir)
	if err != nil {
		t.Fatalf("DecodeReleased refused a released manifest declaring the retired scope: %v", err)
	}
	if m.HostDaemon == nil || m.HostDaemon.Scope != loopholedecl.ScopeJail {
		t.Fatalf("the retired scope was not read as per-jail: %+v", m.HostDaemon)
	}
	unknown := []byte(`{"name": "probe", "transport": "none", "not_a_key": 1}`)
	if _, err := loopholedecl.DecodeReleased(unknown, probeDir); err == nil {
		t.Fatal("DecodeReleased accepted an unknown key; it must stay as strict as Decode")
	}
}
