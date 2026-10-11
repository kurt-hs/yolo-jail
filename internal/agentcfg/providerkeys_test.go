package agentcfg

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
)

// THE ENGINE HALF of the provider keys: a captured value at one leaves the decided overlay and
// the render, on the steady-state branch and on adoption, and is reported; a value-conditional
// key drops only what its pattern matches; a captured deletion and every other key stay.
func TestComposeStatefulDropsCapturedProviderKeys(t *testing.T) {
	surface := manifest.Surface{Agent: "claude", Name: "settings", Path: "~/.claude/settings.json",
		Codec: "json", Mode: manifest.ModeStateful}
	owned := []OwnedLeaf{
		{Pointer: "/env/USE_BR"},
		{Pointer: "/model", Matches: regexp.MustCompile(`^global\.anthropic\.`)},
		{Pointer: "/env/GONE"},
	}
	for _, tc := range []struct {
		name    string
		in      StatefulInputs
		overlay string
	}{
		{"steady state", StatefulInputs{
			Base:              Inputs{Surface: surface},
			CurrentBytes:      []byte(`{"env":{"USE_BR":"1","KEEP":"k"},"model":"global.anthropic.x","theme":"d"}`),
			LastRenderPresent: true,
			LastRenderBytes:   []byte(`{}`),
			OverlayJSON:       []byte(`{"env":{"GONE":null}}`),
			ProviderKeys:      owned,
		}, `{"env":{"GONE":null,"KEEP":"k"},"theme":"d"}`},
		{"adoption", StatefulInputs{
			Base:         Inputs{Surface: surface},
			CurrentBytes: []byte(`{"env":{"USE_BR":"1"},"model":"opus[1m]"}`),
			ProviderKeys: owned,
		}, `{"model":"opus[1m]"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ComposeStateful(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			_ = json.Unmarshal(out.OverlayJSON, &got)
			_ = json.Unmarshal([]byte(tc.overlay), &want)
			if g, _ := json.Marshal(got); string(g) != mustJSON(want) {
				t.Errorf("overlay = %s, want %s", g, tc.overlay)
			}
			cfg := out.Result.ConfigMap()
			if env, _ := cfg["env"].(map[string]any); env["USE_BR"] != nil {
				t.Errorf("the render kept the captured switch: %v", cfg)
			}
			if len(out.ProviderKeyDrops) == 0 || out.ProviderKeyDrops[0].Pointer != "/env/USE_BR" {
				t.Errorf("drops = %+v", out.ProviderKeyDrops)
			}
		})
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
