package packdecl

import (
	"strings"
	"testing"
)

// formerly_test.go pins the manifest facts docs/design/provider-enablement.md adds to a pack: the
// `formerly` record of a rename (OQ-PE3, PE-D7) and the provider's `pinned_endpoint` (PE-D4),
// through Decode, the strict reader every shipped and local pack passes.

func decodeProblems(t *testing.T, manifest string) []string {
	t.Helper()
	_, problems := Decode([]byte(manifest))
	return problems
}

func TestAFormerlyRecordRoundTrips(t *testing.T) {
	m, problems := Decode([]byte(`{"name": "copilot-auth",
		"contributes": [
			{"kind": "provider", "name": "github-copilot", "platform": "github-copilot", "pinned_endpoint": true},
			{"kind": "profile", "name": "copilot", "provider": "github-copilot"}],
		"formerly": {"packs": ["github-copilot"], "profiles": {"github-copilot": "copilot"}}}`))
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	f := m.FormerNames()
	if f == nil || len(f.Packs) != 1 || f.Packs[0] != "github-copilot" || f.Profiles["github-copilot"] != "copilot" {
		t.Errorf("FormerNames() = %+v", f)
	}
	if provs := m.Providers(); len(provs) != 1 || !provs[0].PinnedEndpoint {
		t.Errorf("Providers() = %+v, want the pin carried", provs)
	}
}

func TestAFormerlyRecordNoRefusalCouldUseIsRefused(t *testing.T) {
	for _, c := range []struct{ formerly, want string }{
		{`{"packs": ["a=b"]}`, "is not a pack name"},
		{`{"profiles": {"old": "missing"}}`, "is not a profile this pack declares"},
		{`{"profiles": {"copilot": "copilot"}}`, "still declares a profile named"},
	} {
		got := strings.Join(decodeProblems(t, `{"name": "p", "contributes": [
			{"kind": "provider", "name": "x", "platform": "x"},
			{"kind": "profile", "name": "copilot", "provider": "x"}],
			"formerly": `+c.formerly+`}`), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("formerly %s: problems %q, want one saying %q", c.formerly, got, c.want)
		}
	}
}

func TestPinnedEndpointIsAProviderKey(t *testing.T) {
	got := strings.Join(decodeProblems(t, `{"name": "p", "contributes": [
		{"kind": "profile", "name": "x", "provider": "y", "pinned_endpoint": true}]}`), "\n")
	if !strings.Contains(got, `does not take "pinned_endpoint"`) {
		t.Errorf("a profile carrying pinned_endpoint must be refused, got %q", got)
	}
}
