package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
)

// usergrant_test.go covers the user config's read grant for every workspace
// (docs/design/boundary-broker.md BB-D105, BB-D107): how the gate shows and records it, and the
// advice every broker refusal names.

// The user config's read grant joins each workspace's scope at the config-change gate, never
// unseen: NewScopeCheck (the call a launch, `yolo config apply` and `yolo check` share) carries
// it, the gate's block shows it on its own line marked as the user config's, and only an
// approval records it, where a launch hands it to the broker.
func TestTheUserReadGrantIsShownAtTheGate(t *testing.T) {
	h := newBrokeredHost(t)
	write(t, filepath.Join(h.ws, ".git", "config"), "[remote \"origin\"]\n\turl = git@github.com:o/r.git\n")
	brokers := []ScopeBroker{{Source: "git", Label: "git-broker", RemoteHost: "*", GrantKinds: []string{"create-branch"}}}
	check := func() *ScopeCheck {
		t.Helper()
		read, err := ReadWorkspaceForGate(h.ws)
		if err != nil {
			t.Fatal(err)
		}
		return NewScopeCheck(h.ws, brokers, read)
	}
	if err := RecordApproval(h.ws, decode(t, `{}`), check()); err != nil {
		t.Fatal(err)
	}
	write(t, h.userCfg, `{"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}}`)
	c := check()
	read := brokerscope.Grant{Kind: brokerscope.KindRead, Repo: brokerscope.AnyRepo}
	if got := c.Sources[0].Grants; len(got) != 1 || got[0] != read {
		t.Fatalf("the gate's grants %v, want the user's read grant", got)
	}
	r, err := ScopeChanges(h.ws, c)
	if err != nil || !r.ScopeChanged {
		t.Fatalf("the user's read grant is no change at the gate: %+v, %v", r, err)
	}
	block := strings.Join(r.ScopeBlock, "\n")
	if !strings.Contains(block, "+ grant read *  added (from your user config, for every workspace)") {
		t.Fatalf("the block has no line for the user's read grant:\n%s", block)
	}
	if got := ApprovedGrants(h.ws, "git"); len(got) != 0 {
		t.Fatalf("the grant was recorded before any approval: %v", got)
	}
	if err := RecordApproval(h.ws, decode(t, `{}`), c); err != nil {
		t.Fatal(err)
	}
	if got := c.ApprovedGrants()["git-broker"]; len(got) != 1 || got[0] != read {
		t.Fatalf("the approved grants a launch hands the broker: %v", got)
	}
	if got := ApprovedGrants(h.ws, "git"); len(got) != 1 || got[0] != read {
		t.Fatalf("the approval record's grants %v", got)
	}

	// A workspace naming the same read grant adds nothing: the gate holds it once.
	write(t, h.userCfg, `{"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}], "grant_kinds": ["read"]}}}`)
	write(t, filepath.Join(h.ws, WorkspaceConfigName), `{"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}}`)
	if got := check().Sources[0].Grants; len(got) != 1 || got[0] != read {
		t.Fatalf("the user's and the workspace's same grant: %v, want it once", got)
	}
}

// Every broker refusal for want of a grant names the user-config key, its exact spelling, and
// the host step that approves it, through the source's name alone.
func TestBrokerGrantAdviceNamesTheGrantAndTheApply(t *testing.T) {
	newBrokeredHost(t)
	got := BrokerNoReadRefusal("github", "github.com/o/r", true)
	for _, want := range []string{"no read grant on github.com/o/r", "`brokered.github.grants`",
		`{"kind": "read", "repo": "*"}`, "yolo config apply", "takes the grant at its next call"} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, got)
		}
	}
	if got := BrokerNoReadRefusal("github", "github.com/o/r", false); !strings.Contains(got, "restart the jail") {
		t.Errorf("a broker that takes no live scope does not say to restart:\n%s", got)
	}
}
