package entrypoint

import "testing"

// CutReason cuts at the first dash outside parentheses, and a reason whose parentheses never close
// is said whole rather than cut inside them.
func TestCutReasonNeverCutsInsideParentheses(t *testing.T) {
	for _, tc := range []struct{ in, head, next string }{
		{"a (b — c; its log: /l) — retry", "a (b — c; its log: /l)", "retry"},
		{"a (b (c — d) — e; its log: /l) — retry", "a (b (c — d) — e; its log: /l)", "retry"},
		{"plain — retry — more", "plain", "retry — more"},
		{"no dash at all", "no dash at all", ""},
		{"unclosed (b — c — d", "unclosed (b — c — d", ""},
		{"stray ) close — retry", "stray ) close", "retry"},
	} {
		head, next, _ := CutReason(tc.in)
		if head != tc.head || next != tc.next {
			t.Errorf("CutReason(%q) = %q, %q; want %q, %q", tc.in, head, next, tc.head, tc.next)
		}
	}
}
