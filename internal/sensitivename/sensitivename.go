// Package sensitivename decides, from a NAME alone, whether the value it names may be a
// credential: a config key (`TAVILY_API_KEY`, `apiKeyHelper`), an environment assignment's
// variable, or a command-line flag (`--api-key`).
//
// It reads names, never values, for the reason `yolo config promote` gave when it built the
// check (internal/cli's configpromotesensitive.go): a name is what a user can read, predict
// and override, while value-shape sniffing reads the very bytes it must not handle and fails
// silently on a novel token format. The false-positive direction is the safe one: `key_bindings`
// is called sensitive, which costs a refusal or a redacted log value and never a leak.
package sensitivename

import (
	"strings"
	"unicode"
)

// tokens is the deny-list, as whole TOKENS rather than substrings: `key` would match `monkey`
// as a substring, while a check narrow enough to avoid that misses `API_KEY`. Splitting a name
// into words first (Tokens) matches the way people name credential fields.
var tokens = map[string]bool{
	"apikey": true, "auth": true, "bearer": true, "cert": true, "certificate": true,
	"cookie": true, "credential": true, "credentials": true, "key": true, "keys": true,
	"oauth": true, "passphrase": true, "password": true, "passwd": true, "private": true,
	"pwd": true, "secret": true, "secrets": true, "session": true, "signature": true,
	"token": true, "tokens": true,
}

// Is reports whether name carries a deny-listed token.
func Is(name string) bool {
	for _, tok := range Tokens(name) {
		if tokens[tok] {
			return true
		}
	}
	return false
}

// Tokens lowercases a name and splits it into words on separators and camelCase boundaries:
// "TAVILY_API_KEY" → [tavily api key], "apiKeyHelper" → [api key helper], "OAuthToken" →
// [o auth token].
//
// The acronym rule is the usual one — split before an upper-case rune that follows a
// lower-case one, and before the last upper-case rune of a run that is followed by a
// lower-case one — so "APIKey" yields [api key] rather than [apikey]. `apikey` is in the
// deny-list anyway, so the two spellings agree whichever way a name is written.
func Tokens(name string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
			continue
		case unicode.IsUpper(r) && i > 0:
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		cur.WriteRune(r)
	}
	flush()
	return out
}
