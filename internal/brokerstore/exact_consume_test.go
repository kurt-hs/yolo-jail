package brokerstore

import (
	"context"
	"testing"
	"time"
)

func TestConsumeExactDispatchRejectsLegacySetWideGrant(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Source: "github", Revalidate: unchangedPolicy}
	q, err := s.File(Request{Jail: "jail-1", Start: "start-1", Workspace: "/workspace/app", Argv: []string{"pr", "merge", "1"}, Repo: "owner/repo", StdinDigest: "sha256:empty", Set: "read-write", Policy: Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 25}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Decide(q.ID, "pending", Decision{For: time.Minute})
	if err != nil || g.Digest != "" {
		t.Fatalf("legacy set-wide grant: %+v, %v", g, err)
	}
	dispatched := false
	_, found, err := s.ConsumeExactDispatchContext(context.Background(), q.Workspace, "jail-2", q.Set, q.Digest, s.Revalidate, func() error {
		dispatched = true
		return nil
	})
	if err != nil || found || dispatched {
		t.Fatalf("legacy grant authorized exact path: found=%t dispatched=%t err=%v", found, dispatched, err)
	}
}

func TestConsumeExactDispatchRequiresParentRequestDigestMatch(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Source: "github", Revalidate: unchangedPolicy}
	q, err := s.File(Request{Jail: "jail-1", Start: "start-1", Workspace: "/workspace/app", Argv: []string{"pr", "merge", "1"}, Repo: "owner/repo", StdinDigest: "sha256:empty", Set: "read-write", Policy: Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 25}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Decide(q.ID, "pending", Decision{})
	if err != nil || g.Digest == "" {
		t.Fatalf("exact grant: %+v, %v", g, err)
	}
	dispatched := false
	_, found, err := s.ConsumeExactDispatchContext(context.Background(), q.Workspace, "jail-2", q.Set, "different-action", s.Revalidate, func() error {
		dispatched = true
		return nil
	})
	if err != nil || found || dispatched {
		t.Fatalf("different invocation authorized: found=%t dispatched=%t err=%v", found, dispatched, err)
	}
	_, found, err = s.ConsumeExactDispatchContext(context.Background(), q.Workspace, "jail-2", q.Set, q.Digest, s.Revalidate, func() error {
		dispatched = true
		return nil
	})
	if err != nil || !found || !dispatched {
		t.Fatalf("matching exact invocation not dispatched: found=%t dispatched=%t err=%v", found, dispatched, err)
	}
	reopened := &Store{Dir: s.Dir, Source: s.Source, Revalidate: unchangedPolicy}
	if _, found, err := reopened.ConsumeExactDispatchContext(context.Background(), q.Workspace, "jail-3", q.Set, q.Digest, reopened.Revalidate, nil); err != nil || found {
		t.Fatalf("spent exact grant replayed after reopen: found=%t err=%v", found, err)
	}
}
