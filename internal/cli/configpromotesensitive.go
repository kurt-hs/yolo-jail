package cli

// configpromotesensitive.go is the classification promote does that NOTHING ELSE IN THE
// TREE DOES: deciding, from a captured key alone, whether moving it into a pack manifest
// would move a credential or a machine-local path with it
// (docs/reference/config-ownership.md#what-promote-does).
//
// # It is new work, and the design said otherwise
//
// config-ownership.md#what-promote-does describes the sensitive check as "a key on a deny-list of name patterns, or on a
// surface a pack marks sensitive", and its own warning admits the deny-list "does not exist
// yet" — but the prose still reads as though one were being reused. Nothing in
// internal/agentcfg, internal/packdecl or the config verbs matched `sensitive`, `deny-list`
// or `redact` when this was written. Both halves were new; only the first is built, and the
// paragraphs below say why that is the whole minimum.
//
// # What is built, and what is deliberately not
//
// BUILT: a name-pattern check over the captured subtree, walked to its leaves. The measured
// case this has to catch is four levels down — `mcp.tavily.environment.TAVILY_API_KEY`, a
// live-looking value in this repo's own jail (config-ownership.md#what-promote-does) — and its TOP-LEVEL key is `mcp`, which
// carries no signal at all. A top-level-only check would have promoted it on the first run
// of the verb, which is the outcome the ruling exists to prevent.
//
// NOT BUILT — a per-surface `sensitive` marking in pack manifests. It would be a new field
// in packdecl's closed vocabulary, delivered through the skew machinery, with no shipped
// pack to declare it and no user asking. The kind of thing that is cheap to add the day a
// pack needs it and expensive to have guessed wrong in the meantime.
//
// NOT BUILT — value-shape sniffing (entropy, `sk-`/`tvly-`/`ghp_` prefixes, JWT shapes). It
// reads the very bytes this verb must not handle, it cannot be audited from the key list a
// user sees, and it fails in the direction that matters: a novel token format is promoted
// silently. Names are what a user can read, predict, and override by name.
//
// # Refuse, never redact
//
// config-ownership.md#what-promote-does's ruling, and it is the same one host-side capture already took rather than
// inventing a notion of redactable secret. A redaction would put a placeholder in a pack
// that renders into a real config file, which is how a working agent quietly stops
// authenticating. So a sensitive key is REFUSED, with the key path that tripped it named,
// and `--force <key>` is the per-key way past.
//
// # The false-positive direction is the safe one
//
// `key_bindings` tokenizes to `key`+`bindings` and is refused. That is on purpose: the cost
// of a false positive is one `--force` naming the key, and the cost of a false negative is
// a credential in a file the user may commit to a dotfiles repo.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/sensitivename"
)

// sensitiveName reports whether a key name carries a deny-listed token. The list and the
// tokenizer are internal/sensitivename's, shared with the launch log's redaction.
func sensitiveName(name string) bool { return sensitivename.Is(name) }

// sensitiveKeyPath walks a captured value and returns the first key PATH whose name is
// credential-shaped, or "" when none is. The path is what the refusal names, since a user
// who sees only the top-level key (`mcp`) cannot tell what tripped the check.
//
// The top-level key is tested too: a captured `apiKeyHelper` is the whole subtree.
func sensitiveKeyPath(key string, value any) string {
	if sensitiveName(key) {
		return key
	}
	return walkKeyPaths(key, value, sensitiveName)
}

// environmentBoundPath walks a captured value for a string that is an artifact of the JAIL
// it was set in, returning the key path and the marker it found, or ("", "").
//
// config-ownership.md#what-promote-does's "environment-bound by construction": detectable by inspection and REFUSED rather
// than rewritten. Rewriting would mean guessing which occurrence of `/workspace` is the
// workspace and which is a literal the agent meant, in a value yolo does not own.
//
// The three markers are the jail's FIXED bind destinations plus the substitution token, and
// they are fixed by construction rather than by convention: every container backend mounts
// the workspace at /workspace (the same fixed dest refuseLiveWorkspaceLaunch keys on) and
// the jail's home is /home/agent. That is what makes a value containing one portable
// nowhere — the host has neither path, and another machine's jail has both pointing
// somewhere else.
func environmentBoundPath(key string, value any) (path, marker string) {
	markers := []string{"${workspace}", containerWorkspace, jailHomeDir}
	var found string
	path = walkStrings(key, value, func(s string) bool {
		for _, m := range markers {
			if containsPathMarker(s, m) {
				found = m
				return true
			}
		}
		return false
	})
	return path, found
}

// jailHomeDir is the home every container backend gives the jail's user. A literal here
// rather than a shared constant because there is no shared constant to read: the run
// pipeline spells it in its own mount arguments (`-e JAIL_HOME=/home/agent` and the binds
// beside it), and this is a DETECTOR for a value that already contains it, not a second
// declaration of where the home is.
const jailHomeDir = "/home/agent"

// containsPathMarker reports whether s contains marker as a path PREFIX segment — so
// "/workspace/x" and "/workspace" match while "/workspaces/other" does not. A marker that
// is not a path (the `${workspace}` token) matches anywhere.
func containsPathMarker(s, marker string) bool {
	if !strings.HasPrefix(marker, "/") {
		return strings.Contains(s, marker)
	}
	for i := 0; ; {
		j := strings.Index(s[i:], marker)
		if j < 0 {
			return false
		}
		end := i + j + len(marker)
		if end == len(s) || s[end] == '/' || s[end] == ' ' || s[end] == '"' || s[end] == ':' {
			return true
		}
		i = i + j + 1
	}
}

// walkKeyPaths visits every key path under value (dot-joined, list elements indexed) and
// returns the first whose LEAF NAME satisfies match, or "".
func walkKeyPaths(path string, value any, match func(name string) bool) string {
	switch v := value.(type) {
	case *jsonx.OrderedMap:
		for _, k := range v.Keys() {
			sub, _ := v.Get(k)
			if match(k) {
				return path + "." + k
			}
			if hit := walkKeyPaths(path+"."+k, sub, match); hit != "" {
				return hit
			}
		}
	case map[string]any:
		for _, k := range sortedMapKeys(v) {
			if match(k) {
				return path + "." + k
			}
			if hit := walkKeyPaths(path+"."+k, v[k], match); hit != "" {
				return hit
			}
		}
	case []any:
		for i, item := range v {
			if hit := walkKeyPaths(fmt.Sprintf("%s[%d]", path, i), item, match); hit != "" {
				return hit
			}
		}
	}
	return ""
}

// walkStrings visits every STRING under value and returns the key path of the first that
// satisfies match, or "". A list element's path carries its index, so a refusal points at
// the exact entry.
func walkStrings(path string, value any, match func(s string) bool) string {
	switch v := value.(type) {
	case string:
		if match(v) {
			return path
		}
	case *jsonx.OrderedMap:
		for _, k := range v.Keys() {
			sub, _ := v.Get(k)
			if hit := walkStrings(path+"."+k, sub, match); hit != "" {
				return hit
			}
		}
	case map[string]any:
		for _, k := range sortedMapKeys(v) {
			if hit := walkStrings(path+"."+k, v[k], match); hit != "" {
				return hit
			}
		}
	case []any:
		for i, item := range v {
			if hit := walkStrings(fmt.Sprintf("%s[%d]", path, i), item, match); hit != "" {
				return hit
			}
		}
	}
	return ""
}

// sortedMapKeys keeps a plain map's walk deterministic, so a refusal names the same path
// on every run.
func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
