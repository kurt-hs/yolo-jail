package packload

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	officialpacks "github.com/mschulkind-oss/yolo-jail/packs"
)

func TestGuardrailsComposesGitOptionalLocksOnlyWhenSelected(t *testing.T) {
	loaded, problems := MaterializeEmbedded(officialpacks.FS, t.TempDir())
	if len(problems) != 0 {
		t.Fatalf("materializing official packs: %v", problems)
	}
	var guardrails *Pack
	for _, p := range loaded {
		if p.Name == "guardrails" {
			guardrails = p
			break
		}
	}
	if guardrails == nil {
		t.Fatal("official guardrails pack not found")
	}

	for _, tc := range []struct {
		name  string
		packs []*Pack
		value string
		set   bool
	}{
		{name: "selected", packs: []*Pack{guardrails}, value: "0", set: true},
		{name: "unselected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := ScopeCredentials(ScopeInput{Packs: tc.packs, Providers: jsonx.NewOrderedMap()})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := scope.SharedEnv().Value("GIT_OPTIONAL_LOCKS")
			if got != tc.value || ok != tc.set {
				t.Fatalf("shared GIT_OPTIONAL_LOCKS = %q, present=%v; want %q, present=%v", got, ok, tc.value, tc.set)
			}
		})
	}

	sources := jsonx.NewOrderedMap()
	sources.Set("GIT_OPTIONAL_LOCKS", "1")
	scope, err := ScopeCredentials(ScopeInput{
		Packs: []*Pack{guardrails}, Providers: jsonx.NewOrderedMap(), EnvSources: sources,
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := scope.SharedEnv().Lookup("GIT_OPTIONAL_LOCKS")
	if !ok || entry.Value != "1" || entry.Origin != FromEnvSources {
		t.Errorf("env_sources winner = %+v, present=%v; want value 1 from env_sources", entry, ok)
	}
}
