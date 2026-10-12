package packdecl

// providerkeys.go is the PROVIDER KEY declaration: a setting in an agent's own config that the
// agent's provider SELECTION decides, so no captured in-jail edit may set it.
//
// THE FAULT IT CLOSES (2026-10-11, measured on the maintainer's workspace): claude ran with
// `"profile": {"*": "bedrock", "claude": null}`, so yolo selected claude's subscription and
// delivered it no AWS credentials, while ~/.claude/settings.json still carried
// `env.CLAUDE_CODE_USE_BEDROCK` from the capture overlay (the boot said "4 keys from captured
// in-jail edits"). Captured edits outrank every layer below computed and managed, the settings
// derive asserts the switch only when Bedrock is selected and never tombstones it, so the switch
// an earlier Bedrock render wrote, once captured, kept Claude Code on Bedrock with no
// credentials: the first request hung thirteen minutes and timed out. A provider key is the
// declaration that ends that split: at a jail the COMPUTED layer alone decides it — the value
// the derive asserts for this selection lands, and otherwise the key is absent — and a captured
// edit at it is dropped, the boot saying so in one line.
//
// A PACK FACT because core knows no agent's settings file or variable (OQ-CS8), declared
// PACK-WIDE and keyed by the program's bin (`"provider_keys": {"claude": [...]}`), the place
// model_list_check took for the same reason: a new field on a `program` contribution makes an
// older yolo's tolerant reader skip the WHOLE program, while it ignores an unknown top-level key
// (packs/releasedecode_test.go measures both). Every platform switch is a provider key too,
// without being restated (PlatformSwitch.ProviderKey).
//
// WHERE IT REACHES (docs/design/providers-and-profiles-redesign.md). At a jail: the capture
// overlay, which is yolo's record of in-jail edits, and since PP-D13 (2026-10-11) the user's own
// HOST copy as the jail imports it — the jail composes it without its provider keys, so the
// selection decides them there; a capture never records one (PP-D15). And the program's process
// environment, at every notch, through provider_env (providerenv.go, PP-D14). What it does NOT
// reach, by PP-D1: the user's real file, which yolo never edits, and `yolo host apply`'s render
// of it, which obeys the user's own keys.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
)

// ProviderKey is one setting in an agent's own config that the agent's provider selection
// decides (Manifest.ProviderKeysByBin).
type ProviderKey struct {
	// Surface is the config surface, "<agent>/<name>", carrying the key: one of the program's
	// agent's surfaces, as for a PlatformSwitch.
	Surface string `json:"surface"`
	// Pointer is the RFC 6901 JSON Pointer to the key, "/env/AWS_REGION".
	Pointer string `json:"pointer"`
	// Matches, when set, narrows the key to the VALUES a regular expression (RE2) matches: a
	// captured string value at Pointer is dropped only when it matches, and only while the
	// agent's selected provider is not of Platform. packs/claude declares `/model` this way, so
	// a Bedrock model id an earlier Bedrock session left never reaches claude's subscription,
	// while a subscription model the user picked with `/model` stays theirs. Unset, every
	// captured value at Pointer is dropped whatever the selection.
	Matches string `json:"matches,omitempty"`
	// Platform is the provider platform whose selection keeps a Matches value: required with
	// Matches, refused without it.
	Platform string `json:"platform,omitempty"`
}

// ProviderKey is the switch as a provider key: its pointer, owned whatever its value.
func (s PlatformSwitch) ProviderKey() ProviderKey {
	return ProviderKey{Surface: s.Surface, Pointer: s.Pointer}
}

// ProviderKeys is every provider key the program installing bin declares — its platform
// switches first, then its provider_keys entry — nil when it declares neither or this manifest
// installs no such program. A fork restates none: the base manifest's map is the one kept.
func (m *Manifest) ProviderKeys(bin string) []ProviderKey {
	if m == nil || bin == "" {
		return nil
	}
	for _, c := range m.Contributions() {
		if c.Kind != KindProgram || c.Bin != bin {
			continue
		}
		var out []ProviderKey
		for _, s := range c.PlatformSwitches {
			out = append(out, s.ProviderKey())
		}
		if !c.IsFork() {
			out = append(out, m.ProviderKeysByBin[bin]...)
		}
		return out
	}
	return nil
}

// ProviderKeysFor is every provider key any program this manifest installs declares on the
// surface "<agent>/<name>", in declaration order.
func (m *Manifest) ProviderKeysFor(surface string) []ProviderKey {
	if m == nil {
		return nil
	}
	var out []ProviderKey
	seen := map[string]bool{}
	for _, c := range m.Contributions() {
		if c.Kind != KindProgram || seen[c.Bin] {
			continue
		}
		seen[c.Bin] = true
		for _, k := range m.ProviderKeys(c.Bin) {
			if k.Surface == surface {
				out = append(out, k)
			}
		}
	}
	return out
}

// validateProviderKeys binds each provider_keys entry to a non-fork program this pack declares
// and checks every key in it (providerKeyProblems).
func (m *Manifest) validateProviderKeys() []string {
	bins := make([]string, 0, len(m.ProviderKeysByBin))
	for bin := range m.ProviderKeysByBin {
		bins = append(bins, bin)
	}
	sort.Strings(bins)
	var out []string
	for _, bin := range bins {
		label := fmt.Sprintf("provider_keys[%q]", bin)
		owned := false
		for _, c := range m.Contributions() {
			if bin != "" && c.Kind == KindProgram && c.Bin == bin && !c.IsFork() {
				owned = true
				break
			}
		}
		if !owned {
			out = append(out, label+": must name a non-fork program declared by this pack; move "+
				"the keys to the pack that declares the program")
		}
		for i, k := range m.ProviderKeysByBin[bin] {
			out = append(out, providerKeyProblems(fmt.Sprintf("%s[%d]", label, i), bin, k)...)
		}
	}
	return out
}

// providerKeyProblems validates one provider key of the program bin: a surface of that program's
// agent, a pointer naming a key, and a Matches that compiles and comes with a Platform.
func providerKeyProblems(at, bin string, k ProviderKey) []string {
	var out []string
	agent, name, ok := strings.Cut(k.Surface, "/")
	switch {
	case !ok || agent == "" || name == "" || strings.Contains(name, "/"):
		out = append(out, fmt.Sprintf("%s.surface: %q must be \"<agent>/<name>\", one of this "+
			"pack's own config surfaces", at, k.Surface))
	case agent != bin:
		out = append(out, fmt.Sprintf("%s.surface: %q is agent %q's, and a provider key is in "+
			"this program's (%q) own config", at, k.Surface, agent, bin))
	}
	if steps, err := jsonptr.Parse(k.Pointer); err != nil || len(steps) == 0 {
		out = append(out, fmt.Sprintf("%s.pointer: %q must be an RFC 6901 pointer to a key "+
			"(\"/env/NAME\"), not the whole file", at, k.Pointer))
	}
	switch {
	case k.Matches == "" && k.Platform != "":
		out = append(out, fmt.Sprintf("%s.platform: %q says which selection keeps a matching "+
			"value, and there is no \"matches\" — drop it, or add the pattern", at, k.Platform))
	case k.Matches != "":
		if _, err := regexp.Compile(k.Matches); err != nil {
			out = append(out, fmt.Sprintf("%s.matches: %q is not a regular expression: %v",
				at, k.Matches, err))
		}
		if k.Platform == "" {
			out = append(out, at+".matches: needs the \"platform\" whose selection keeps a "+
				"matching value")
		} else if prob := PlatformProblem(at+".platform", k.Platform); prob != "" {
			out = append(out, prob)
		}
	}
	return out
}
