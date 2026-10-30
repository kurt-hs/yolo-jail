package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A first read grant over a workspace whose only approval record is a legacy one (repositories
// recorded, no grants key) is shown as added at the gate, never as "unchanged": the record was
// made while the broker could do nothing (BB-D110).
func TestAFirstGrantOverALegacyRecordIsShownAsAdded(t *testing.T) {
	h := newBrokeredHost(t)
	write(t, filepath.Join(h.ws, ".git", "config"), "[remote \"origin\"]\n\turl = git@github.com:o/r.git\n")
	write(t, filepath.Join(h.ws, WorkspaceConfigName), `{}`)
	write(t, h.userCfg, `{"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}}`)
	if err := os.MkdirAll(filepath.Dir(ApprovalScopePath(h.ws)), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, ApprovalScopePath(h.ws), `{"git": ["git@github.com:o/r.git"]}`)
	read, err := ReadWorkspaceForGate(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	check := NewScopeCheck(h.ws, []ScopeBroker{{Source: "git", Label: "git-broker", RemoteHost: "*"}}, read)
	r, err := ScopeChanges(h.ws, check)
	if err != nil {
		t.Fatal(err)
	}
	block := strings.Join(r.ScopeBlock, "\n")
	t.Logf("%s", block)
	if !r.ScopeChanged || strings.Contains(block, "unchanged") || !strings.Contains(block, "+ grant read") {
		t.Fatalf("a first grant is not shown as added:\n%s", block)
	}
}

// The same holds when the record names the read grant but no repository (a grant over nothing
// approved nothing): the grant is first held now, so it is added, not unchanged.
func TestAFirstGrantOverARecordOfAGrantWithNoScopeIsShownAsAdded(t *testing.T) {
	h := newBrokeredHost(t)
	write(t, filepath.Join(h.ws, ".git", "config"), "[remote \"origin\"]\n\turl = git@github.com:o/r.git\n")
	write(t, filepath.Join(h.ws, WorkspaceConfigName), `{}`)
	write(t, h.userCfg, `{"brokered": {"git": {"grants": [{"kind": "read", "repo": "*"}]}}}`)
	if err := os.MkdirAll(filepath.Dir(ApprovalScopePath(h.ws)), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, ApprovalScopePath(h.ws), `{"git.grants": ["read *"]}`)
	read, err := ReadWorkspaceForGate(h.ws)
	if err != nil {
		t.Fatal(err)
	}
	check := NewScopeCheck(h.ws, []ScopeBroker{{Source: "git", Label: "git-broker", RemoteHost: "*"}}, read)
	r, err := ScopeChanges(h.ws, check)
	if err != nil {
		t.Fatal(err)
	}
	block := strings.Join(r.ScopeBlock, "\n")
	t.Logf("%s", block)
	if !r.ScopeChanged || !strings.Contains(block, "+ grant read") || strings.Contains(block, "grant read *  unchanged") {
		t.Fatalf("a first grant is not shown as added:\n%s", block)
	}
	if !strings.Contains(strings.Join(r.ScopeCounts, ""), "grants: 1 added") {
		t.Fatalf("counts %q do not count the grant as added", r.ScopeCounts)
	}
}
