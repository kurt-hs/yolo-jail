package config

import (
	"strings"
	"testing"
)

// renamedpacks_test.go pins the refusal of a former pack name (docs/design/provider-enablement.md
// OQ-PE3, PE-D7): a `packs` entry still naming `openai-auth` or `github-copilot` is refused naming
// the pack that took the name over, read from that pack's `formerly` record, and nothing is
// aliased. Through lowerPackSource, the one funnel every `packs` entry passes (packhome_test.go).
func TestAFormerPackNameIsRefusedNamingTheNewOne(t *testing.T) {
	for old, now := range map[string]string{"openai-auth": "codex-auth", "github-copilot": "copilot-auth"} {
		_, joined := lowerPackSource(old, "", "config.packs[0]")
		want := `config.packs[0].source: "` + old + `" is now "` + now + `"; write "` + now + `"`
		if !strings.Contains(joined, want) {
			t.Errorf("packs [%s]: errors %q, want one saying %s", old, joined, want)
		}
		if strings.Contains(joined, "available:") {
			t.Errorf("packs [%s]: the refusal lists every shipped pack instead of the one answer: %q", old, joined)
		}
		if entry, problem := lowerPackSource(now, "", "config.packs[0]"); problem != "" || entry.Name != now {
			t.Errorf("packs [%s]: %+v, %q", now, entry, problem)
		}
	}
}
