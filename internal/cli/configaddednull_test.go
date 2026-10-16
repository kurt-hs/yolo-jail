package cli

import (
	"strings"
	"testing"
)

// A `null` the user ADDED is not a deletion, and promote must not write it into a pack as one.
//
// On the steady-state branch mergeDiff records a key the file gained as `"k": null` with the
// same token it uses for a deleted key, so the capture overlay holds a tombstone for a key
// the file still holds. The render reinstates the null (agentcfg.Inputs.LiteralNulls), which
// is what tells the two apart: a genuine deletion leaves the key ABSENT from last_render, an
// added null leaves it there as `null`. Before the fix promote read the tombstone as a
// captured deletion, and --accept-promotion wrote it into the local pack's config-overlay,
// where a merge-patch null deletes the key at every notch: the user's key lost.
//
// The control, `legacyFlag`, is a real in-jail deletion (absent from last_render) and must stay
// one; the nested `env.FOO` null must not be promoted either, while its live sibling is.
func TestPromoteNeverWritesAnAddedNullAsADeletion(t *testing.T) {
	w := newPromoteWorld(t, `["claude"]`)
	w.capture("claude", "settings",
		`{"statusLine":null,"legacyFlag":null,"env":{"FOO":null,"BAR":"1"}}`,
		`{"statusLine":null,"env":{"FOO":null,"BAR":"1","BAZ":"2"}}`)

	out, errw, rc := w.run("claude", "--plan")
	if rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errw)
	}
	if got := dispositionOf(t, out, "statusLine"); got != promotionRedundant {
		t.Errorf("an added null = %s, want %s (the last render holds it):\n%s", got, promotionRedundant, out)
	}
	if got := dispositionOf(t, out, "legacyFlag"); got != promotionPromotable {
		t.Errorf("a genuine deletion = %s, want %s:\n%s", got, promotionPromotable, out)
	}

	if out, errw, rc := w.run("claude", "--accept-promotion"); rc != 0 {
		t.Fatalf("rc=%d: %s%s", rc, out, errw)
	}
	decl := localPackManifest(t, w.home)
	contributes, _ := decl["contributes"].([]any)
	if len(contributes) != 1 {
		t.Fatalf("contributes = %v, want one config-overlay", contributes)
	}
	c, _ := contributes[0].(map[string]any)
	cfg, _ := c["config"].(map[string]any)
	managed, _ := cfg["managed"].(map[string]any)
	if v, present := managed["statusLine"]; present {
		t.Errorf("an added null was promoted as a deletion (%v): the local pack now deletes "+
			"the key at every notch: %v", v, managed)
	}
	if v, present := managed["legacyFlag"]; !present || v != nil {
		t.Errorf("a genuine captured deletion was not promoted as one: %v", managed)
	}
	env, _ := managed["env"].(map[string]any)
	if v, present := env["FOO"]; present {
		t.Errorf("a nested added null was promoted as a deletion (%v): %v", v, managed)
	}
	if env["BAR"] != "1" {
		t.Errorf("the nested live sibling was not promoted: %v", managed)
	}
}

// `config diff` must not call a key the file HOLDS a deletion. A null the user added is
// recorded in the overlay with the tombstone token, and the last render holding the key as
// `null` is what distinguishes it from a key deleted in-jail (absent from the render).
func TestDiffDoesNotReportAnAddedNullAsDeleted(t *testing.T) {
	got := diffFixture(t, "claude", "settings",
		`{"statusLine":null}`,
		`{"statusLine":null,"legacyFlag":null,"env":{"FOO":null,"BAR":"1"}}`)
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "statusLine") && strings.Contains(line, "deleted in-jail") {
			t.Errorf("a null the file holds was reported as deleted in-jail:\n%s", got)
		}
		if strings.Contains(line, "legacyFlag") && !strings.Contains(line, "deleted in-jail") {
			t.Errorf("a genuine in-jail deletion was not reported as one:\n%s", got)
		}
	}
}
