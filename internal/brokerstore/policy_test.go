package brokerstore

import (
	"sync"
	"testing"
	"time"
)

func TestAllowOnceNarrowsToPolicyCeiling(t *testing.T) {
	for _, ceiling := range []time.Duration{time.Minute, 15 * time.Minute, time.Hour} {
		t.Run(ceiling.String(), func(t *testing.T) {
			s, now, input := fixture(t)
			input.Policy.Window = min(input.Policy.Window, ceiling)
			input.Policy.MaxWindow = ceiling
			input.Policy.Session = false
			r, err := s.File(input)
			if err != nil {
				t.Fatal(err)
			}
			g, err := s.Decide(r.ID, "pending", Decision{})
			if err != nil {
				t.Fatal(err)
			}
			if g.Expires.Sub(*now) != min(15*time.Minute, ceiling) || g.Uses != 1 {
				t.Fatal(g)
			}
			if _, ok, _ := s.Consume(input.Jail, input.Start, input.Set, "wrong digest"); ok {
				t.Fatal("once covered another command")
			}
			*now = g.Expires
			if _, ok, _ := s.Consume(input.Jail, input.Start, input.Set, r.Digest); ok {
				t.Fatal("consumed at expiry")
			}
		})
	}
}
func TestInvalidPolicyCannotFile(t *testing.T) {
	for _, p := range []Policy{{}, {Window: -time.Minute, MaxWindow: time.Hour, Uses: 1}, {Window: time.Hour, MaxWindow: time.Minute, Uses: 1}, {Window: time.Minute, MaxWindow: time.Hour, Uses: 0}, {Window: time.Minute, MaxWindow: time.Hour, Session: true, Uses: 1}} {
		s, _, r := fixture(t)
		r.Policy = p
		if _, err := s.File(r); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
func TestConcurrentDecisionsAcrossStoreHandles(t *testing.T) {
	s, _, input := fixture(t)
	r, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	successes := make(chan bool, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := &Store{Dir: s.Dir, Now: s.Now}
			_, err := other.Decide(r.ID, "pending", Decision{})
			successes <- err == nil
		}()
	}
	wg.Wait()
	close(successes)
	n := 0
	for success := range successes {
		if success {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d decisions won", n)
	}
}
func TestRevokeAllEndsEveryHolderGrant(t *testing.T) {
	s, _, input := fixture(t)
	for _, jail := range []string{"one", "two"} {
		input.Jail = jail
		r, err := s.File(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Decide(r.ID, "pending", Decision{For: time.Minute}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Revoke("all"); err != nil {
		t.Fatal(err)
	}
	for _, jail := range []string{"one", "two"} {
		if _, ok, err := s.Consume(jail, input.Start, input.Set, "any"); err != nil || ok {
			t.Fatal(ok, err)
		}
	}
}
