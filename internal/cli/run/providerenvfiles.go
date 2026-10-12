package run

// providerenvfiles.go writes each agent's PROVIDER-KEYS FILE (entrypoint.ProviderKeysFile):
// the agent's provider variables (packload.ProviderEnv, declared by its pack) made exactly what
// its profile selection says, sourced by the agent's launcher AFTER its env file
// (entrypoint.agentEnvShellFn), at every container backend and on macos-user.
//
// WHY A SECOND FILE (docs/design/providers-and-profiles-redesign.md PP-D14, the maintainer's
// 2026-10-11 ruling: "a Claude started from a Claude shell needs to follow the same auth path
// everywhere"). The env file writes every value against the launcher's incoming environment
// (OQ-CN8, "the user's explicit value wins": a def-form `${K:-v}` or a `case` over the values
// yolo set elsewhere), so a variable the incoming environment already held — a Bedrock claude's
// CLAUDE_CODE_USE_BEDROCK in the shell it gives a nested claude — kept beating the profile, and a
// variable the selection does not set at all was never touched. A provider variable is not a
// preference: it decides where the agent's requests and credentials go. So for those names alone
// the profile wins, and every other name keeps OQ-CN8's precedence in the env file, unchanged.
//
// It is a separate file rather than more lines in the env file because the env file's grammar is
// READ BACK: the macos-user MCP view infers the shared composition's value from the env files'
// `case` guards (internal/entrypoint's sharedValueInAgentFiles), on the invariant that no agent's
// file sets a name to the shared value, and a forced line breaks exactly that. The wire bridge's
// key channel reads the env file too. Neither reads this one. And it is written for EVERY agent
// whose pack declares provider variables, a profile-less one included — the env file exists only
// for an agent with values of its own, and an unprofiled claude is precisely the one that must
// lose an inherited switch.

import (
	"slices"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// providerEnvAgents is every agent a selected pack installs whose pack declares provider
// variables, sorted and deduplicated: the agents that get a provider-keys file this entry.
func (c *packChannel) providerEnvAgents() []string {
	if c == nil || c.scope == nil {
		return nil
	}
	var out []string
	for _, p := range c.packs {
		if p == nil {
			continue
		}
		for _, bin := range p.InstallBins() {
			if packdecl.ValidBinName(bin) && len(c.scope.ProviderEnv(bin)) > 0 {
				out = append(out, bin)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// providerEnvFileContent renders agent's provider-keys file, "" when its pack declares no
// provider variables. For each variable (packload.ProviderEnvVar):
//
//   - the agent's composition (scope.EnvFor: its profile's values over what every process
//     receives) sets it: `export K='v'`, whatever the incoming value — except the region fill's
//     fallback for a region the selection reads, which stays the default it is
//     (`export K=${K:-'v'}`, BR-D23);
//   - the composition removes it, or says nothing and the selection does not read it:
//     `unset K`;
//   - the composition says nothing and the selection reads it (a credential its provider
//     claims, its platform's region): no line, the inherited value being the selection's input;
//   - a value-conditional variable is unset only while its value matches the pattern, tested by
//     bash's own `=~` (every launcher is bash).
//
// Each name whose incoming value the file replaces or removes is collected, and one line on
// stderr names them, so a user who exported one sees why the agent does not have it.
func providerEnvFileContent(channel *packChannel, agent string) string {
	vars := channel.scope.ProviderEnv(agent)
	if len(vars) == 0 {
		return ""
	}
	comp := channel.scope.EnvFor(agent)
	d := channel.scope.Agent(agent)
	var check, apply strings.Builder
	for _, v := range vars {
		k := v.Name
		if !packdecl.ValidEnvName(k) || slices.Contains(entrypoint.WireTables(), k) {
			continue
		}
		if v.Matches != nil {
			if _, decided := comp.Lookup(k); decided {
				continue // the composition's own line in the env file stands
			}
			apply.WriteString("_yolo_pk_re=" + shSingleQuote(v.Pattern) + "\n")
			apply.WriteString("if [[ \"${" + k + "-}\" =~ $_yolo_pk_re ]]; then _yolo_pk=\"$_yolo_pk " +
				k + "\"; unset " + k + "; fi\n")
			continue
		}
		e, decided := comp.Lookup(k)
		switch {
		case decided && !e.Unset:
			if v.ReadBySelection && d != nil && filledRegion(d, e) {
				apply.WriteString(exportDefault(k, e.Value))
				continue
			}
			check.WriteString("[ -z \"${" + k + "-}\" ] || [ \"$" + k + "\" = " + shSingleQuote(e.Value) +
				" ] || _yolo_pk=\"$_yolo_pk " + k + "\"\n")
			apply.WriteString(exportPlain(k, e.Value))
		case decided || !v.ReadBySelection:
			check.WriteString("[ -z \"${" + k + "-}\" ] || _yolo_pk=\"$_yolo_pk " + k + "\"\n")
			apply.WriteString("unset " + k + "\n")
		}
	}
	var b strings.Builder
	b.WriteString("# Auto-generated by yolo: " + agent + "'s provider variables, exactly as its profile\n")
	b.WriteString("# selection says (PP-D14). Sourced by its launcher after " + agent + ".sh, so it wins\n")
	b.WriteString("# over the launching environment; set these through a profile, not an export.\n")
	b.WriteString("_yolo_pk=\n")
	b.WriteString(check.String())
	b.WriteString(apply.String())
	b.WriteString("if [ -n \"$_yolo_pk\" ]; then printf '%s\\n' \"yolo: " + agent + ": replaced or cleared" +
		"$_yolo_pk: " + agent + "'s profile selection decides its provider variables. To change " +
		"them, select a profile (yolo config-ref: profile, providers).\" >&2; fi\n")
	b.WriteString("unset _yolo_pk _yolo_pk_re\n")
	return b.String()
}

// shSingleQuote is s as one single-quoted shell word.
func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
