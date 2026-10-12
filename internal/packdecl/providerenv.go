package packdecl

// providerenv.go is the PROVIDER ENVIRONMENT declaration: which of a program's provider keys
// (providerkeys.go) are also variables of its PROCESS environment, so every launcher of the
// program, at every notch, can make them exactly what the agent's provider selection says.
//
// THE FAULT IT CLOSES (maintainer ruling 2026-10-11, PP-D14 in
// docs/design/providers-and-profiles-redesign.md: "a Claude started from a Claude shell needs to
// follow the same auth path everywhere"). The per-agent env file set a provider variable only when
// the incoming environment had none (`${VAR:-default}`, OQ-CN8's "the user's explicit value
// wins") and said nothing about a variable the selection does not set, so a claude started from
// a Bedrock claude's shell inherited CLAUDE_CODE_USE_BEDROCK and ran on Bedrock whatever its own
// profile said.
//
// WHY A POINTER AND NOT A LIST OF NAMES. The names are already declared, once, as provider keys
// on the program's settings surface (`/env/CLAUDE_CODE_USE_BEDROCK`); what core cannot know is
// that the members of claude's settings `env` object are its process environment (OQ-CS8: core
// knows no agent's settings file). So the pack says that one fact — `{"surface":
// "claude/settings", "pointer": "/env"}` — and the variables are the provider keys directly
// beneath it, a value-conditional key (`matches`) keeping its pattern and platform. A second
// list of the same names would be the drift the single declaration avoids.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
)

// ProviderEnvSource is where a program's process environment sits in its own config: the
// surface ("<agent>/<name>") and the RFC 6901 pointer to the object whose members are
// environment variables ("/env").
type ProviderEnvSource struct {
	Surface string `json:"surface"`
	Pointer string `json:"pointer"`
}

// ProviderEnvVar is one variable of a program's process environment that its provider selection
// decides: its name, and for a value-conditional provider key the pattern and platform it keeps
// (ProviderKey.Matches, ProviderKey.Platform).
type ProviderEnvVar struct {
	Name     string
	Matches  string
	Platform string
}

// ProviderEnv is every provider variable of the program installing bin, in declaration order:
// each provider key (ProviderKeys, platform switches first) on the declared surface whose
// pointer is exactly one step beneath the declared pointer. nil when the pack declares no
// provider_env for bin.
func (m *Manifest) ProviderEnv(bin string) []ProviderEnvVar {
	if m == nil || bin == "" {
		return nil
	}
	src, ok := m.ProviderEnvByBin[bin]
	if !ok {
		return nil
	}
	base, err := jsonptr.Parse(src.Pointer)
	if err != nil {
		return nil
	}
	var out []ProviderEnvVar
	seen := map[string]bool{}
	for _, k := range m.ProviderKeys(bin) {
		if k.Surface != src.Surface {
			continue
		}
		steps, err := jsonptr.Parse(k.Pointer)
		if err != nil || len(steps) != len(base)+1 {
			continue
		}
		under := true
		for i := range base {
			if steps[i] != base[i] {
				under = false
				break
			}
		}
		name := steps[len(steps)-1]
		if !under || !ValidEnvName(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, ProviderEnvVar{Name: name, Matches: k.Matches, Platform: k.Platform})
	}
	return out
}

// validateProviderEnv binds each provider_env entry to a non-fork program this pack declares, a
// surface of that program's agent, and a pointer that parses.
func (m *Manifest) validateProviderEnv() []string {
	bins := make([]string, 0, len(m.ProviderEnvByBin))
	for bin := range m.ProviderEnvByBin {
		bins = append(bins, bin)
	}
	sort.Strings(bins)
	var out []string
	for _, bin := range bins {
		at := fmt.Sprintf("provider_env[%q]", bin)
		src := m.ProviderEnvByBin[bin]
		owned := false
		for _, c := range m.Contributions() {
			if bin != "" && c.Kind == KindProgram && c.Bin == bin && !c.IsFork() {
				owned = true
				break
			}
		}
		if !owned {
			out = append(out, at+": must name a non-fork program declared by this pack; move it "+
				"to the pack that declares the program")
		}
		agent, name, ok := strings.Cut(src.Surface, "/")
		switch {
		case !ok || agent == "" || name == "" || strings.Contains(name, "/"):
			out = append(out, fmt.Sprintf("%s.surface: %q must be \"<agent>/<name>\", one of this "+
				"pack's own config surfaces", at, src.Surface))
		case agent != bin:
			out = append(out, fmt.Sprintf("%s.surface: %q is agent %q's, and a program's "+
				"environment is in its own (%q) config", at, src.Surface, agent, bin))
		}
		if _, err := jsonptr.Parse(src.Pointer); err != nil {
			out = append(out, fmt.Sprintf("%s.pointer: %q must be an RFC 6901 pointer to the "+
				"object whose members are the environment (\"/env\")", at, src.Pointer))
		}
	}
	return out
}
