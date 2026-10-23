package brokerstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// kinds is the event kinds Diff reports between two reads of s, around step.
func kinds(t *testing.T, s *Store, step func()) ([]string, []Event) {
	t.Helper()
	before, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	step()
	after, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	evs := Diff(before, after)
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out, evs
}

func wantKinds(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events %v, want %v", got, want)
	}
}

// Every change a watch reports, from filing to each way a grant ends.
func TestDiffReportsEveryEvent(t *testing.T) {
	s, now, input := fixture(t)
	var r Request
	got, evs := kinds(t, s, func() {
		var err error
		if r, err = s.File(input); err != nil {
			t.Fatal(err)
		}
	})
	wantKinds(t, got, EventRequestFiled)
	if evs[0].Request.ID != r.ID || evs[0].Request.State != "pending" {
		t.Fatalf("filed event: %+v", evs[0].Request)
	}
	var g Grant
	got, evs = kinds(t, s, func() {
		var err error
		if g, err = s.Decide(r.ID, "pending", Decision{For: time.Hour, Uses: 2}); err != nil {
			t.Fatal(err)
		}
	})
	wantKinds(t, got, EventRequestDecided)
	if evs[0].Request.State != "allowed" || evs[0].Grant == nil || evs[0].Grant.ID != g.ID {
		t.Fatalf("decided event carries no grant: %+v", evs[0])
	}
	consume := func() {
		if _, ok, err := s.Consume(input.Jail, input.Start, input.Set, r.Digest, s.Revalidate); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	got, evs = kinds(t, s, consume)
	wantKinds(t, got, EventGrantUsed)
	if evs[0].Grant.Uses != 1 || evs[0].Request == nil || evs[0].Request.ID != r.ID {
		t.Fatalf("used event: %+v", evs[0])
	}
	got, evs = kinds(t, s, consume)
	wantKinds(t, got, EventGrantUsed, EventGrantEnded)
	if evs[1].Grant.EndedBy() != EndUsed {
		t.Fatal(evs[1].Grant)
	}

	// Revoked, and expired.
	input.Argv = []string{"pr", "merge", "2"}
	r2, _ := s.File(input)
	g2, err := s.Decide(r2.ID, "pending", Decision{For: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = kinds(t, s, func() {
		if err := s.Revoke(g2.ID); err != nil {
			t.Fatal(err)
		}
	})
	wantKinds(t, got, EventGrantRevoked)
	input.Argv = []string{"pr", "merge", "3"}
	r3, _ := s.File(input)
	if _, err := s.Decide(r3.ID, "pending", Decision{For: time.Minute}); err != nil {
		t.Fatal(err)
	}
	input.Argv = []string{"pr", "merge", "4"}
	if _, err := s.File(input); err != nil {
		t.Fatal(err)
	}
	got, evs = kinds(t, s, func() { *now = now.Add(PendingLife) })
	wantKinds(t, got, EventRequestExpired, EventGrantEnded)
	if evs[1].Grant.EndedBy() != EndExpired {
		t.Fatal(evs[1].Grant)
	}

	// The jail stopping refuses what is pending and ends what is live.
	input.Argv = []string{"pr", "merge", "5"}
	r5, _ := s.File(input)
	if _, err := s.Decide(r5.ID, "pending", Decision{For: time.Minute}); err != nil {
		t.Fatal(err)
	}
	input.Argv = []string{"pr", "merge", "6"}
	if _, err := s.File(input); err != nil {
		t.Fatal(err)
	}
	got, evs = kinds(t, s, func() {
		if _, err := s.End(input.Jail, input.Start); err != nil {
			t.Fatal(err)
		}
	})
	wantKinds(t, got, EventRequestDecided, EventGrantEnded)
	if evs[0].Request.State != "refused" || evs[1].Grant.EndedBy() != EndStopped {
		t.Fatal(evs[0].Request, evs[1].Grant)
	}
}

// A request filed and decided between two reads is reported as both, the filing first.
func TestDiffSplitsAFilingDecidedBetweenReads(t *testing.T) {
	s, _, input := fixture(t)
	got, evs := kinds(t, s, func() {
		r, _ := s.File(input)
		if _, err := s.Decide(r.ID, "pending", Decision{Deny: true}); err != nil {
			t.Fatal(err)
		}
	})
	wantKinds(t, got, EventRequestFiled, EventRequestDecided)
	if evs[0].Request.State != "pending" || evs[1].Request.State != "denied" {
		t.Fatal(evs[0].Request.State, evs[1].Request.State)
	}
}

// The four refusals a front end gives its own exit code are distinguishable.
func TestDecideRefusalKinds(t *testing.T) {
	s, _, input := fixture(t)
	input.Policy.MaxWindow, input.Policy.Session = time.Hour, false
	r, _ := s.File(input)
	if _, err := s.Decide(r.ID, "pending", Decision{For: 2 * time.Hour}); !errors.Is(err, ErrRefusedByPolicy) {
		t.Fatalf("ceiling: %v", err)
	}
	if _, err := s.Decide("r-00000000000000000000000000000000", "pending", Decision{}); !errors.Is(err, ErrUnknownID) {
		t.Fatalf("unknown: %v", err)
	}
	if err := s.Revoke("g-00000000000000000000000000000000"); !errors.Is(err, ErrUnknownID) {
		t.Fatalf("unknown grant: %v", err)
	}
	if _, err := s.Decide(r.ID, "pending", Decision{Deny: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(r.ID, "pending", Decision{}); !errors.Is(err, ErrAlreadyDecided) || !strings.Contains(err.Error(), "already denied") {
		t.Fatalf("decided: %v", err)
	}
	input.Argv = []string{"pr", "merge", "9"}
	r2, _ := s.File(input)
	s.Revalidate = func(Request) (Policy, error) {
		return Policy{}, errors.New("request holder is not this live host launch")
	}
	if _, err := s.Decide(r2.ID, "pending", Decision{}); !errors.Is(err, ErrNotConfirmed) || errors.Is(err, ErrRefusedByPolicy) {
		t.Fatalf("stale authority: %v", err)
	}
}

// Watch opens with the state, then reports each change until its context ends.
func TestWatchOpensWithStateThenEmitsEvents(t *testing.T) {
	s, _, input := fixture(t)
	s.Now = nil
	input.Argv = []string{"pr", "merge", "0"}
	first, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var opened []Request
	var got []string
	started := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- s.Watch(ctx, 5*time.Millisecond, func(snap Snapshot) { opened = snap.Requests; close(started) },
			func(ev Event) { mu.Lock(); got = append(got, ev.Kind); mu.Unlock() }, nil)
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("the watch ended before it opened: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the watch never handed over its opening state")
	}
	if len(opened) != 1 || opened[0].ID != first.ID {
		t.Fatalf("opened with %+v", opened)
	}
	input.Argv = []string{"pr", "merge", "1"}
	if _, err := s.File(input); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	wantKinds(t, got, EventRequestFiled)
}

// Every JSON object of the surface carries "schema": 1, and the state keeps only what is
// open: pending requests and live grants.
func TestSurfaceJSONCarriesSchemaAndOpenRecordsOnly(t *testing.T) {
	s, _, input := fixture(t)
	r, _ := s.File(input)
	g, _ := s.Decide(r.ID, "pending", Decision{For: time.Hour})
	input.Argv = []string{"pr", "merge", "2"}
	pending, _ := s.File(input)
	input.Argv = []string{"pr", "merge", "3"}
	r3, _ := s.File(input)
	g3, _ := s.Decide(r3.ID, "pending", Decision{})
	_ = s.Revoke(g3.ID)
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(s.StateView(snap))
	var doc struct {
		Schema   int              `json:"schema"`
		Requests []map[string]any `json:"requests"`
		Grants   []map[string]any `json:"grants"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != 1 || len(doc.Requests) != 1 || len(doc.Grants) != 1 {
		t.Fatalf("%s", b)
	}
	req, grant := doc.Requests[0], doc.Grants[0]
	for _, k := range []string{"id", "source", "jail", "workspace", "set", "argv", "command", "repo", "filed", "expires", "state"} {
		if _, ok := req[k]; !ok {
			t.Errorf("request has no %q: %s", k, b)
		}
	}
	if req["id"] != pending.ID || req["source"] != "github" || req["schema"] != float64(1) || req["command"] != "pr merge 2" {
		t.Errorf("request %v", req)
	}
	if grant["id"] != g.ID || grant["coverage"] != "set" || grant["uses_left"] != float64(25) || grant["schema"] != float64(1) || grant["state"] != "live" {
		t.Errorf("grant %v", grant)
	}
	ev := s.EventView(Event{Kind: EventGrantRevoked, Request: &r3, Grant: &g3}, time.Now())
	b, _ = json.Marshal(ev)
	if !strings.Contains(string(b), `"schema":1,"event":"grant-revoked"`) || !strings.Contains(string(b), `"coverage":"command"`) {
		t.Errorf("event %s", b)
	}
}
