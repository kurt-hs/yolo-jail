package brokerstore

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Store, *time.Time, Request) {
	t.Helper()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := &Store{Dir: filepath.Join(t.TempDir(), "github"), Now: func() time.Time { return now }}
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
	if _, ok, err := s.Consume(input.Jail, input.Start, input.Set, r.Digest); err != nil || !ok {
		t.Fatalf("consume %v %v", ok, err)
	}
	if _, ok, _ := s.Consume(input.Jail, input.Start, input.Set, r.Digest); ok {
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
	for _, who := range [][2]string{{"other", "start"}, {"jail", "new"}} {
		if _, ok, _ := s.Consume(who[0], who[1], input.Set, r.Digest); ok {
			t.Fatal("wrong holder")
		}
	}
	if _, ok, _ := s.Consume(input.Jail, input.Start, "other", r.Digest); ok {
		t.Fatal("wrong set")
	}
	*now = now.Add(time.Hour)
	if _, ok, _ := s.Consume(input.Jail, input.Start, input.Set, r.Digest); ok {
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
			_, ok, err := s.Consume(input.Jail, input.Start, input.Set, "another command")
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
	r, _ = s.File(input)
	_, err := s.Decide(r.ID, "pending", Decision{Session: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.End(input.Jail, input.Start); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Consume(input.Jail, input.Start, input.Set, r.Digest); ok {
		t.Fatal("ended lifetime granted")
	}
}
