package entrypoint

import "strings"

// reasonDash is what ends a build's reason's headline: the act that tries again follows it.
const reasonDash = " — "

// CutReason splits a build's reason at the dash before the act that tries again: head is the
// reason's own words, next what follows the dash, ok whether there was one. A dash INSIDE
// PARENTHESES is not that dash: a failed build's reason carries its error, and the path of the
// build's own log after it, in parentheses ("… failed on the host (<error>; its log: <path>) —
// <next>"), and an error may say " — " itself, so cutting at the first dash kept the head of the
// error and dropped the log path from every headline a launch or a launcher says. A reason whose
// parentheses never close is never cut inside them, so it is said whole rather than without its
// log.
func CutReason(reason string) (head, next string, ok bool) {
	depth := 0
	for i := 0; i < len(reason); i++ {
		switch reason[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.HasPrefix(reason[i:], reasonDash) {
				return reason[:i], reason[i+len(reasonDash):], true
			}
		}
	}
	return reason, "", false
}
