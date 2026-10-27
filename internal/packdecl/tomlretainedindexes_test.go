package packdecl

import (
	"reflect"
	"testing"
)

func TestGroupedUseReadersRetainTypedContributionIndexes(t *testing.T) {
	const data = `[[blocked_tool]]
bin = "blocked-tool"
message = "blocked"
replacement = "safe-tool"
future_field = true
replace = true

[[env]]
vars = { CURRENT = "kept" }
future_field = true
replace = false

[[env]]
vars = { CURRENT = "kept" }
replace = true
`
	for readerName, decode := range map[string]func([]byte) (*Manifest, []string, []string){
		"use":      DecodeTOMLForUse,
		"tolerant": DecodeTOMLTolerant,
	} {
		t.Run(readerName, func(t *testing.T) {
			manifest, problems, skipped := decode([]byte(data))
			if len(problems) != 0 || len(skipped) != 2 {
				t.Fatalf("problems=%v skipped=%v", problems, skipped)
			}
			contributions := manifest.Contributions()
			if len(contributions) != 2 {
				t.Fatalf("retained contributions = %#v", contributions)
			}
			if got := contributions[0]; got.Kind != KindBlockedTool || !got.Replace || got.Location.Line != 1 || got.Location.Key != "blocked_tool[0]" {
				t.Errorf("restrictive KEEP origin/replace = %#v", got)
			}
			if got := contributions[1]; got.Kind != KindEnv || !got.Replace || got.Location.Line != 13 || got.Location.Key != "env[1]" {
				t.Errorf("duplicate-identity retained env origin/replace = %#v", got)
			}
		})
	}
}

func TestGroupedUseReadersRetainProviderRowsWhenWireAPIIsTrimmed(t *testing.T) {
	const data = `[[program]]
bin = "future-tool"
via = "future-installer"
package = "future-tool"

[providers.meta]
alias = "muse"
model = "wire-main"
replace = true

[providers.meta.endpoints.openai]
base_url = "https://api.example.test/v1"
wire_api = "future-wire-api"

[providers.meta.models."wire-main"]
name = "Main"
wire_id = "wire-main"

[providers.meta.models."wire-fast"]
name = "Fast"
wire_id = "wire-fast"
`
	for readerName, decode := range map[string]func([]byte) (*Manifest, []string, []string){
		"use":      DecodeTOMLForUse,
		"tolerant": DecodeTOMLTolerant,
	} {
		t.Run(readerName, func(t *testing.T) {
			manifest, problems, skipped := decode([]byte(data))
			if len(problems) != 0 || len(skipped) != 2 {
				t.Fatalf("problems=%v skipped=%v", problems, skipped)
			}
			contributions := manifest.Contributions()
			if len(contributions) != 1 {
				t.Fatalf("retained contributions = %#v", contributions)
			}
			provider := contributions[0]
			if provider.Kind != KindProvider || !provider.Replace || provider.Origin.Format != ManifestFormatTOML || provider.Location.Line != 6 || provider.Location.Key != "providers.meta" {
				t.Errorf("trimmed provider origin/replace = %#v", provider)
			}
			public := manifest.Providers()
			if len(public) != 1 || public[0].ModelOrder == nil || !reflect.DeepEqual(public[0].ModelOrder, []string{"wire-main", "wire-fast"}) {
				t.Fatalf("public provider model order = %#v", public)
			}
			if public[0].ModelRows["wire-main"].Name != "Main" || public[0].ModelRows["wire-fast"].Name != "Fast" {
				t.Errorf("trimmed provider model rows = %#v", public[0].ModelRows)
			}
			if public[0].Endpoints["openai"].WireAPI != "" || public[0].Endpoints["openai"].BaseURL != "https://api.example.test/v1" {
				t.Errorf("trimmed provider endpoint = %#v", public[0].Endpoints["openai"])
			}
		})
	}
}

func TestGroupedUseReadersKeepAutonomyAndDependentServiceSkipIndexes(t *testing.T) {
	t.Run("second autonomy", func(t *testing.T) {
		const data = `[[autonomy]]
autonomous = { lists = [{ surface = "pi/settings", path = "/packages", add = ["first"] }] }
replace = true

[[autonomy]]
guarded = { lists = [{ surface = "pi/settings", path = "/packages", add = ["second"] }] }
replace = false
`
		manifest, problems, skipped := DecodeTOMLTolerant([]byte(data))
		if len(problems) != 0 || len(skipped) != 1 {
			t.Fatalf("problems=%v skipped=%v", problems, skipped)
		}
		contributions := manifest.Contributions()
		if len(contributions) != 1 || contributions[0].Kind != KindAutonomy || !contributions[0].Replace || contributions[0].Location.Line != 1 || contributions[0].Location.Key != "autonomy[0]" {
			t.Fatalf("first autonomy retained metadata = %#v", contributions)
		}
	})

	t.Run("dependent adapter", func(t *testing.T) {
		const data = `[[adapter]]
adapts = { from = "openai", to = "anthropic", from_platforms = ["aws-bedrock"] }
address = "http://127.0.0.1:8214"

[[adapter]]
adapts = { from = "openai-responses", to = "anthropic" }
address = "http://127.0.0.1:8215"
replace = true

[[service]]
name = "wire-bridge"
host_daemon = { cmd = ["wire-bridge"] }
via_address = "http://127.0.0.1:8216"
future_field = true
`
		manifest, problems, skipped := DecodeTOMLForUse([]byte(data))
		if len(problems) != 0 || len(skipped) != 2 {
			t.Fatalf("problems=%v skipped=%v", problems, skipped)
		}
		contributions := manifest.Contributions()
		if len(contributions) != 1 || contributions[0].Kind != KindAdapter || !contributions[0].Replace || contributions[0].Location.Line != 5 || contributions[0].Location.Key != "adapter[1]" {
			t.Fatalf("dependent skip retained adapter metadata = %#v", contributions)
		}
	})
}
