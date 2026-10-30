package cli

// requestcheck_test.go pins that the `yolo check` a `yolo request` runs after its write reads the
// readable (TOML) config the launch reads: the user's config.toml and config.d fragment with
// `[allow]`, and the project's yolo-jail.toml with `[requests]` (docs/design/readable-config.md
// RC-D12, RC-D17). Before the fix it reported "No user config found" at a config.jsonc that was
// never there, reported no workspace config beside the yolo-jail.toml it had just written,
// refused `config.requests: unknown key` (the launch's validation did the same), and told a user
// whose `[allow.git] always = ["fetch"]` grants the read that it grants nothing, naming
// `brokered.git.grants`. That last note is pinned in config (TestUngrantedBrokerNotesReadThe
// ReadableGrant) with a fixed resolver: here the broker set is the process's memoized pack
// modules (loopholes.PackModules), which an earlier test in the package can leave empty.

import (
	"strings"
	"testing"
)

func TestRequestsPostWriteCheckReadsTheTOMLConfigs(t *testing.T) {
	fx := newNamedVerbFixture(t, "", "[allow.git]\nalways = [\"fetch\"]\non_request = [\"push\"]\n\n[allow.github]\nalways = [\"read\"]\n")
	rc, out, errs := captureStd(t, func() int { return registry["request"]([]string{"request", "git", "push", "main"}) })
	assertPublicPostWriteCheck(t, rc, out, errs)
	for _, want := range []string{
		"Parsed user config: " + fx.userConfig,
		"Parsed workspace config: " + fx.project,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the post-write check lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{
		"No user config found",
		"No workspace",
		"config.requests: unknown key",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("the post-write check says %q:\n%s", bad, out)
		}
	}
}
