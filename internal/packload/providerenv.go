package packload

// providerenv.go resolves a program's PROVIDER ENVIRONMENT (packdecl.ProviderEnv) for one
// launch: the variables of the agent's process environment that its provider selection decides,
// which every launcher of the agent, at every notch, makes exactly what the selection says
// (docs/design/providers-and-profiles-redesign.md PP-D14, the maintainer's 2026-10-11 ruling: "a
// Claude started from a Claude shell needs to follow the same auth path everywhere").
//
// THREE CASES, per variable, against the environment the launcher was started with:
//
//   - the agent's composition SETS OR REMOVES it: that is the value, whatever the launching
//     process held — the selection's value is exported, not defaulted;
//   - the composition says nothing and the selected provider READS it (ReadBySelection: a
//     credential it claims, a region variable of its platform): the inherited value is the
//     selection's own input, the AWS profile a Bedrock user exported, and is kept;
//   - otherwise it is REMOVED, so a CLAUDE_CODE_USE_BEDROCK a Bedrock parent left in the
//     environment never reaches a claude whose profile is its subscription.
//
// A VALUE-CONDITIONAL variable (ANTHROPIC_MODEL, declared with a Bedrock-only pattern) is
// removed only while it holds a value its pattern matches and the selected provider is not of
// its platform, so a subscription model the user typed (`ANTHROPIC_MODEL=opus claude`) is still
// theirs: OQ-CN8's "the user's explicit value wins" keeps holding for every value the selection
// does not decide.
//
// Core names no variable: the list is the agent pack's declaration.

import (
	"regexp"
	"slices"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/agentenv"
)

// ProviderEnvVar is one provider variable resolved for a launch: its name, and for a
// value-conditional variable whose platform is not the selected one, the pattern a value must
// match to be removed. Matches nil = unconditional.
type ProviderEnvVar struct {
	Name    string
	Matches *regexp.Regexp
	// Pattern is Matches' source, for a writer that spells it in another language (the jail's
	// env file, read by bash).
	Pattern string
	// ReadBySelection says the agent's selected provider itself reads the variable from the
	// environment: a credential it claims (api_key_env_name, PP-D9's platform co-claims) or a
	// region variable of its platform (region_env_name, platform_regions). Such a value the
	// composition does not set is the selection's own input and is kept.
	ReadBySelection bool
}

// ProviderEnvFor is the provider environment of the program installing bin among packs,
// resolved for a selection whose provider is of platform ("" for none): every unconditional
// variable, and each value-conditional one whose platform is not the selected one (the
// selection that serves its values keeps them). A pattern that does not compile is skipped —
// packdecl refuses it at decode. nil when no selected pack installs bin or it declares none.
func ProviderEnvFor(packs []*Pack, bin, platform string) []ProviderEnvVar {
	p := binOwner(packs, bin)
	if p == nil || p.Decl == nil {
		return nil
	}
	var out []ProviderEnvVar
	for _, v := range p.Decl.ProviderEnv(bin) {
		r := ProviderEnvVar{Name: v.Name}
		if v.Matches != "" {
			if v.Platform == platform {
				continue
			}
			re, err := regexp.Compile(v.Matches)
			if err != nil {
				continue
			}
			r.Matches, r.Pattern = re, v.Matches
		}
		out = append(out, r)
	}
	return out
}

// ProviderEnv is ProviderEnvFor for agent at this launch — the gate's own packs and the
// platform of the agent's selected provider — with ReadBySelection answered from the gate's
// claims and the selected platforms' region variables.
func (s *CredentialScope) ProviderEnv(agent string) []ProviderEnvVar {
	if s == nil {
		return nil
	}
	vars := ProviderEnvFor(s.packs, agent, s.sel.Platforms[agent])
	for i := range vars {
		vars[i].ReadBySelection = s.selectionReads(agent, vars[i].Name)
	}
	return vars
}

// selectionReads reports whether a provider of agent's active set reads name from the
// environment: one that claims it as a credential, or whose platform lists it as a region
// variable (for this agent's program, agentRegionVars, or for the platform). An agent with no
// selection reads none.
func (s *CredentialScope) selectionReads(agent, name string) bool {
	for _, p := range s.agents[agent].setProviders() {
		if slices.Contains(s.claims[name], p) {
			return true
		}
	}
	platforms := s.sel.SetPlatforms[agent]
	if len(platforms) == 0 && s.sel.Platforms[agent] != "" {
		platforms = []string{s.sel.Platforms[agent]}
	}
	if len(platforms) == 0 {
		return false
	}
	reqs := regionRequirements(s.packs)
	for _, platform := range platforms {
		if platform == "" {
			continue
		}
		own, _ := agentRegionVars(s.packs, agent, platform, reqs[platform].vars)
		if slices.Contains(own, name) || slices.Contains(reqs[platform].vars, name) {
			return true
		}
	}
	return false
}

// ClearProviderEnv is environ with every provider variable in vars removed that decided does not
// mention and the selection does not read (ReadBySelection): an unconditional one whatever it
// holds, a value-conditional one when its value matches. decided reports whether the launch's
// own composition sets or removes the name, which is then already what the selection says. It
// also returns, in vars order, the names it removed that held a non-empty value — what the
// launch says it cleared.
func ClearProviderEnv(environ []string, vars []ProviderEnvVar, decided func(string) bool) ([]string, []string) {
	if len(vars) == 0 {
		return environ, nil
	}
	held := map[string]string{}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			held[k] = v
		}
	}
	var drop []agentenv.Var
	var cleared []string
	for _, v := range vars {
		if v.ReadBySelection || (decided != nil && decided(v.Name)) {
			continue
		}
		cur, present := held[v.Name]
		if !present {
			continue
		}
		if v.Matches != nil && !v.Matches.MatchString(cur) {
			continue
		}
		drop = append(drop, agentenv.Var{Key: v.Name, Unset: true})
		if cur != "" {
			cleared = append(cleared, v.Name)
		}
	}
	return agentenv.Apply(environ, drop), cleared
}
