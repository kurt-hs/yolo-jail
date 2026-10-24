package brokerstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workingFixture(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	return &Store{Dir: filepath.Join(t.TempDir(), "git"), Source: "git", Now: func() time.Time { return now },
		Revalidate: unchangedPolicy}, &now
}

func branch(ws, ref string) WorkingEntry {
	return WorkingEntry{Workspace: ws, Repo: "github.com/o/r", Kind: WorkingBranch, Resource: ref, By: "create-branch"}
}

func TestWorkingAddIsIdempotent(t *testing.T) {
	s, _ := workingFixture(t)
	e, added, err := s.AddWorking(branch("/ws", "refs/heads/agent/x"))
	if err != nil || !added {
		t.Fatalf("first add: %+v %v %v", e, added, err)
	}
	if !strings.HasPrefix(e.ID, "w-") || e.Added.IsZero() || e.By != "create-branch" {
		t.Fatalf("entry %+v", e)
	}
	again, added, err := s.AddWorking(branch("/ws", "refs/heads/agent/x"))
	if err != nil || added || again.ID != e.ID {
		t.Fatalf("duplicate add: %+v added %v %v (want the first entry back, not added)", again, added, err)
	}
	all, err := s.Working()
	if err != nil || len(all) != 1 {
		t.Fatalf("working %+v %v", all, err)
	}
	got, ok, err := s.LiveWorking("/ws", "github.com/o/r", "refs/heads/agent/x")
	if err != nil || !ok || got.ID != e.ID {
		t.Fatalf("live %+v %v %v", got, ok, err)
	}
	if _, ok, _ := s.LiveWorking("/other", "github.com/o/r", "refs/heads/agent/x"); ok {
		t.Fatal("another workspace's entry covered this one")
	}
	for _, bad := range []WorkingEntry{{Repo: "r", Resource: "refs/heads/x", By: "human"}, {Workspace: "/ws", Resource: "x", By: "human"},
		{Workspace: "/ws", Repo: "r", By: "human"}, {Workspace: "/ws", Repo: "r", Resource: "refs/heads/x"}} {
		if _, _, err := s.AddWorking(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestWorkingEndReasons(t *testing.T) {
	s, now := workingFixture(t)
	for _, reason := range []string{EndDeleted, EndGone, EndRevoked} {
		e, _, err := s.AddWorking(branch("/ws", "refs/heads/"+reason))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EndWorking(e.ID, reason); err != nil {
			t.Fatalf("end %s: %v", reason, err)
		}
		if _, ok, _ := s.LiveWorking("/ws", e.Repo, e.Resource); ok {
			t.Fatalf("%s: still live", reason)
		}
		all, _ := s.Working()
		var ended WorkingEntry
		for _, w := range all {
			if w.ID == e.ID {
				ended = w
			}
		}
		if ended.EndReason != reason || !ended.Ended.Equal(*now) {
			t.Fatalf("%s: ended %+v", reason, ended)
		}
		// An ended entry is not live, so the same ref can join again as a new entry.
		next, added, err := s.AddWorking(branch("/ws", "refs/heads/"+reason))
		if err != nil || !added || next.ID == e.ID {
			t.Fatalf("%s: re-add %+v %v %v", reason, next, added, err)
		}
	}
	if err := s.EndWorking("w-00000000000000000000000000000000", EndDeleted); !errors.Is(err, ErrUnknownID) {
		t.Fatalf("unknown id: %v", err)
	}
	e, _, _ := s.AddWorking(branch("/ws", "refs/heads/bad-reason"))
	if err := s.EndWorking(e.ID, "expired"); err == nil {
		t.Fatal("an entry ended for a grant's reason")
	}
	// Ended entries are pruned a week after they end, like grants.
	*now = now.Add(8 * 24 * time.Hour)
	all, _ := s.Working()
	for _, w := range all {
		if !w.Ended.IsZero() {
			t.Fatalf("an entry ended 8 days ago survived: %+v", w)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir, "working"))
	if len(entries) != len(all) {
		t.Fatalf("%d files for %d entries", len(entries), len(all))
	}
}

func TestRevokeWorkingByID(t *testing.T) {
	s, _ := workingFixture(t)
	e, _, _ := s.AddWorking(branch("/ws", "refs/heads/agent/x"))
	other, _, _ := s.AddWorking(branch("/ws", "refs/heads/agent/y"))
	if err := s.Revoke(e.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.LiveWorking("/ws", e.Repo, e.Resource); ok {
		t.Fatal("revoked entry still live")
	}
	if _, ok, _ := s.LiveWorking("/ws", other.Repo, other.Resource); !ok {
		t.Fatal("revoking one entry ended another")
	}
	all, _ := s.Working()
	for _, w := range all {
		if w.ID == e.ID && w.EndReason != EndRevoked {
			t.Fatalf("end reason %q", w.EndReason)
		}
	}
	if err := s.Revoke("w-11111111111111111111111111111111"); !errors.Is(err, ErrUnknownID) {
		t.Fatalf("unknown entry: %v", err)
	}
}

func TestRevokeAllForWorkspace(t *testing.T) {
	s, _ := workingFixture(t)
	grantIn := func(ws string) Grant {
		r, err := s.File(Request{Jail: "jail-" + ws, Start: "start", Workspace: ws, Argv: []string{"push"}, Repo: "o/r",
			StdinDigest: "d", Set: "read-write", Policy: Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 5}})
		if err != nil {
			t.Fatal(err)
		}
		g, err := s.Decide(r.ID, "pending", Decision{For: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	gA, gB := grantIn("/a"), grantIn("/b")
	eA, _, _ := s.AddWorking(branch("/a", "refs/heads/x"))
	eB, _, _ := s.AddWorking(branch("/b", "refs/heads/x"))
	if n, err := s.RevokeIn("all", "/a"); err != nil || n != 2 {
		t.Fatalf("revoke /a: ended %d, want its grant and entry: %v", n, err)
	}
	_, gs, _ := s.List()
	for _, g := range gs {
		switch g.ID {
		case gA.ID:
			if g.EndedBy() != EndRevoked {
				t.Fatalf("workspace /a's grant not revoked: %+v", g)
			}
		case gB.ID:
			if !g.Ended.IsZero() {
				t.Fatalf("workspace /b's grant revoked: %+v", g)
			}
		}
	}
	if _, ok, _ := s.LiveWorking("/a", eA.Repo, eA.Resource); ok {
		t.Fatal("workspace /a's entry still live")
	}
	if _, ok, _ := s.LiveWorking("/b", eB.Repo, eB.Resource); !ok {
		t.Fatal("workspace /b's entry revoked")
	}
	// `all` with no workspace ends every grant and leaves the working set (BB-D86's spelling).
	if err := s.Revoke("all"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.LiveWorking("/b", eB.Repo, eB.Resource); !ok {
		t.Fatal("--revoke all with no workspace ended a working-set entry")
	}
	// A workspace with nothing in the store is not an error: it ends nothing, and says so.
	if n, err := s.RevokeIn("all", "/none"); err != nil || n != 0 {
		t.Fatalf("empty workspace: ended %d, %v", n, err)
	}
	if _, err := s.RevokeIn(eB.ID, "/a"); err == nil {
		t.Fatal("a workspace filter was accepted with one id")
	}
}
