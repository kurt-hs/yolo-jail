package brokerstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Store, *time.Time, Request) {
	t.Helper()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := &Store{Dir: filepath.Join(t.TempDir(), "github"), Now: func() time.Time { return now }, Revalidate: unchangedPolicy}
	r := Request{Jail: "jail", Start: "start", Workspace: "/project", Argv: []string{"pr", "merge", "1"}, Repo: "o/r", StdinDigest: "digest", Set: "read-write", Policy: Policy{Window: 15 * time.Minute, MaxWindow: 12 * time.Hour, Session: true, Uses: 25}}
	return s, &now, r
}
func TestFirstDecisionWinsAndOnceIsSpent(t *testing.T) {
	s, _, input := fixture(t)
	r, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Decide(r.ID, "pending", Decision{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(r.ID, "pending", Decision{Deny: true}); err == nil {
		t.Fatal("second decision won")
	}
	if g.Uses != 1 || g.Digest != r.Digest {
		t.Fatalf("once: %+v", g)
	}
	if _, ok, err := s.Consume(input.Workspace, input.Jail, input.Set, r.Digest, s.Revalidate); err != nil || !ok {
		t.Fatalf("consume %v %v", ok, err)
	}
	if _, ok, _ := s.Consume(input.Workspace, input.Jail, input.Set, r.Digest, s.Revalidate); ok {
		t.Fatal("reused once")
	}
}
func TestPolicyExpiryIdentityAndRevocation(t *testing.T) {
	s, now, input := fixture(t)
	input.Policy.MaxWindow = time.Hour
	input.Policy.Session = false
	r, _ := s.File(input)
	for _, d := range []Decision{{For: 2 * time.Hour}, {Session: true}, {For: time.Minute, Uses: 26}, {For: -time.Minute}} {
		if _, err := s.Decide(r.ID, "pending", d); err == nil {
			t.Fatalf("accepted %+v", d)
		}
	}
	g, err := s.Decide(r.ID, "pending", Decision{For: time.Hour, Uses: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{"/other", ""} {
		if _, ok, _ := s.Consume(ws, input.Jail, input.Set, r.Digest, s.Revalidate); ok {
			t.Fatal("wrong holder")
		}
	}
	if _, ok, _ := s.Consume(input.Workspace, input.Jail, "other", r.Digest, s.Revalidate); ok {
		t.Fatal("wrong set")
	}
	*now = now.Add(time.Hour)
	if _, ok, _ := s.Consume(input.Workspace, input.Jail, input.Set, r.Digest, s.Revalidate); ok {
		t.Fatal("expiry boundary")
	}
	if err := s.Revoke(g.ID); err != nil {
		t.Fatal(err)
	}
	rs, gs, err := s.List()
	if err != nil || len(rs) != 1 || len(gs) != 1 || gs[0].Ended.IsZero() {
		t.Fatalf("list %v %v %v", rs, gs, err)
	}
	*now = now.Add(7 * 24 * time.Hour)
	rs, gs, err = s.List()
	if err != nil || len(rs) != 0 || len(gs) != 0 {
		t.Fatalf("retention %v %v %v", rs, gs, err)
	}
}
func TestConcurrentFilingAndUseAreSerialized(t *testing.T) {
	s, _, input := fixture(t)
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.File(input)
			if err != nil {
				t.Error(err)
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for got := range ids {
		if id != "" && got != id {
			t.Fatal("duplicates did not join")
		}
		id = got
	}
	_, err := s.Decide(id, "pending", Decision{For: time.Minute, Uses: 3})
	if err != nil {
		t.Fatal(err)
	}
	used := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := s.Consume(input.Workspace, input.Jail, input.Set, "another command", s.Revalidate)
			if err != nil {
				t.Error(err)
			}
			used <- ok
		}()
	}
	wg.Wait()
	close(used)
	n := 0
	for ok := range used {
		if ok {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("spent %d uses", n)
	}
}
func TestStorePermissionsAndSymlinks(t *testing.T) {
	s, _, input := fixture(t)
	r, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{s.Dir: 0700, filepath.Join(s.Dir, "requests"): 0700, filepath.Join(s.Dir, "requests", r.ID+".json"): 0600, filepath.Join(s.Dir, ".lock"): 0600} {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != want {
			t.Fatalf("%s: %v %v", path, fi, err)
		}
	}
	path := filepath.Join(s.Dir, "requests", r.ID+".json")
	os.Remove(path)
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.List(); err == nil {
		t.Fatal("followed record symlink")
	}
}
func TestPendingExpiryAndEndLifetime(t *testing.T) {
	s, now, input := fixture(t)
	r, _ := s.File(input)
	*now = now.Add(time.Hour)
	if _, err := s.Decide(r.ID, "pending", Decision{}); err == nil {
		t.Fatal("approved expired request")
	}
	input.Start = "next"
	// An unanswered expiry cools the same command down as a denial for ten
	// minutes (boundary-broker.md §9.2); refile past the quiet period.
	*now = now.Add(11 * time.Minute)
	r, _ = s.File(input)
	_, err := s.Decide(r.ID, "pending", Decision{Session: true})
	if err != nil {
		t.Fatal(err)
	}
	input.Argv = []string{"pr", "merge", "2"}
	waiting, _ := s.File(input)
	if err := s.End(input.Jail, input.Start); err != nil {
		t.Fatal(err)
	}
	if q, err := s.Status(input.Jail, input.Start, waiting.ID); err != nil || q.State != "refused" {
		t.Fatalf("a pending request outlived its broker lifetime: %+v %v", q, err)
	}
	// Ending the lifetime ends no grant (BB-D99): the session grant still serves the workspace.
	if _, ok, err := s.Consume(input.Workspace, input.Jail, input.Set, r.Digest, s.Revalidate); err != nil || !ok {
		t.Fatalf("ending a broker lifetime ended its workspace's grant: %v %v", ok, err)
	}
}

// A GRANT OUTLIVES ITS BROKER, FOR ITS WORKSPACE ONLY (boundary-broker.md BB-D99). A grant is
// approved under one broker; that broker ends its lifetime and a new broker opens the store
// afresh, as a restart or relaunch does. Every jail of the workspace may spend the grant, and
// the use records which; another workspace never sees it, nor can `--revoke all --workspace`
// of another workspace end it.
func TestAGrantOutlivesItsBrokerForItsWorkspaceOnly(t *testing.T) {
	s, now, input := fixture(t)
	once, _ := s.File(input)
	g1, err := s.Decide(once.ID, "pending", Decision{})
	if err != nil {
		t.Fatal(err)
	}
	input.Argv, input.Set = []string{"pr", "merge", "2"}, "timed-set" // its own set, so it cannot cover the Allow once
	timed, _ := s.File(input)
	g2, err := s.Decide(timed.ID, "pending", Decision{For: 15 * time.Minute, Uses: 3})
	if err != nil {
		t.Fatal(err)
	}
	if g1.Workspace != "/project" || g2.Workspace != "/project" {
		t.Fatalf("a decision did not bind its grant to the request's workspace: %+v %+v", g1, g2)
	}
	if err := s.End(input.Jail, input.Start); err != nil {
		t.Fatal(err)
	}

	next := &Store{Dir: s.Dir, Now: func() time.Time { return *now }, Revalidate: unchangedPolicy}
	*now = now.Add(10 * time.Minute)
	for _, ws := range []string{"/elsewhere", "/project/sub", ""} {
		if _, ok, _ := next.Consume(ws, "jail", input.Set, timed.Digest, next.Revalidate); ok {
			t.Fatalf("workspace %q spent /project's grant", ws)
		}
	}
	if n, err := next.RevokeIn("all", "/elsewhere"); err != nil || n != 0 {
		t.Fatalf("revoking another workspace ended %d records: %v", n, err)
	}
	if g, ok, err := next.Consume("/project", "jail", once.Set, once.Digest, next.Revalidate); err != nil || !ok || g.ID != g1.ID {
		t.Fatalf("Allow once from the stopped broker: %+v %v %v", g, ok, err)
	}
	g, ok, err := next.Consume("/project/", "second-jail", input.Set, timed.Digest, next.Revalidate)
	if err != nil || !ok || g.ID != g2.ID || g.UsedBy != "second-jail" || g.Uses != 2 {
		t.Fatalf("timed grant from the stopped broker, spent by a second jail: %+v %v %v", g, ok, err)
	}
	*now = g2.Expires
	if _, ok, _ := next.Consume("/project", "jail", input.Set, timed.Digest, next.Revalidate); ok {
		t.Fatal("a grant outlived its expiry")
	}
}

// An Allow once written before grants carried a workspace takes its request's (BB-D99's migration),
// and one an older yolo ended as "stopped" stays ended.
func TestALegacyGrantTakesItsRequestsWorkspace(t *testing.T) {
	s, _, input := fixture(t)
	r, _ := s.File(input)
	g, err := s.Decide(r.ID, "pending", Decision{})
	if err != nil {
		t.Fatal(err)
	}
	input.Argv = []string{"pr", "merge", "2"}
	r2, _ := s.File(input)
	stopped, err := s.Decide(r2.ID, "pending", Decision{})
	if err != nil {
		t.Fatal(err)
	}
	legacy := func(g Grant) {
		g.Workspace = ""
		b, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("Workspace")) {
			t.Fatalf("legacy record still names a workspace: %s", b)
		}
		if err := os.WriteFile(filepath.Join(s.Dir, "grants", g.ID+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	legacy(g)
	stopped.Ended, stopped.EndReason = stopped.Expires.Add(-time.Minute), EndStopped
	legacy(stopped)
	if _, ok, _ := s.Consume("/elsewhere", "jail", input.Set, r.Digest, s.Revalidate); ok {
		t.Fatal("a legacy grant served another workspace")
	}
	used, ok, err := s.Consume("/project", "jail", input.Set, r.Digest, s.Revalidate)
	if err != nil || !ok || used.ID != g.ID || used.Workspace != "/project" {
		t.Fatalf("legacy grant: %+v %v %v", used, ok, err)
	}
	if _, ok, _ := s.Consume("/project", "jail", input.Set, r2.Digest, s.Revalidate); ok {
		t.Fatal("a grant an older broker ended as stopped came back")
	}
}

func unchangedPolicy(q Request) (Policy, error) { return q.Policy, nil }
