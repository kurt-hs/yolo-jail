package brokerstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestThreePendingPerJailAcrossSetsStartsAndReopen(t *testing.T) {
	s, now, input := fixture(t)
	var ids []string
	for i := 0; i < 3; i++ {
		q := input
		q.Argv = []string{"pr", "merge", fmt.Sprint(i)}
		q.Start = fmt.Sprint(i)
		q.Set = fmt.Sprint(i)
		r, err := s.File(q)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
	}
	reopened := &Store{Dir: s.Dir, Now: s.Now, Revalidate: unchangedPolicy}
	input.Argv = []string{"pr", "merge", "fourth"}
	_, err := reopened.File(input)
	var limit *PendingLimitError
	if !errors.As(err, &limit) || len(limit.Requests) != 3 {
		t.Fatalf("limit %v", err)
	}
	for _, id := range ids {
		if !slices.Contains(limit.Requests, id) {
			t.Fatal(limit.Requests)
		}
	}
	other := input
	other.Jail = "other"
	if _, err := s.File(other); err != nil {
		t.Fatal("cap crossed jail", err)
	}
	if _, err := s.Decide(ids[0], "pending", Decision{Deny: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.File(input); err != nil {
		t.Fatal("decided slot not freed", err)
	}
	*now = now.Add(time.Hour)
	input.Argv = []string{"pr", "merge", "after-expiry"}
	if _, err := s.File(input); err != nil {
		t.Fatal("expired slot not freed", err)
	}
}
func TestPendingDuplicateAtCapAndConcurrentDistinctFiling(t *testing.T) {
	s, _, input := fixture(t)
	var wg sync.WaitGroup
	var filed atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := input
			q.Argv = []string{"pr", "merge", fmt.Sprint(i)}
			if _, err := s.File(q); err == nil {
				filed.Add(1)
			} else {
				var limit *PendingLimitError
				if !errors.As(err, &limit) {
					t.Errorf("unexpected %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
	if filed.Load() != 3 {
		t.Fatal("cap race", filed.Load())
	}
	rs, _, _ := s.List()
	for _, q := range rs {
		joined, err := s.File(q)
		if err != nil || joined.ID != q.ID {
			t.Fatal("duplicate at cap", err)
		}
	}
}
func TestDenialCooldownDigestJailAndExactBoundary(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			s, now, input := fixture(t)
			q, err := s.File(input)
			if err != nil {
				t.Fatal(err)
			}
			if expired {
				*now = now.Add(time.Hour)
				s.List()
			} else {
				if _, err := s.Decide(q.ID, "pending", Decision{Deny: true}); err != nil {
					t.Fatal(err)
				}
			}
			deniedAt := *now
			reopened := &Store{Dir: s.Dir, Now: s.Now, Revalidate: unchangedPolicy}
			input.Start = "new-start"
			input.Set = "changed-set"
			for _, delta := range []time.Duration{0, 10*time.Minute - time.Nanosecond} {
				*now = deniedAt.Add(delta)
				_, err := reopened.File(input)
				var denial *DenialCooldownError
				if !errors.As(err, &denial) || denial.Request != q.ID {
					t.Fatal("cooldown", delta, err)
				}
			}
			other := input
			other.Jail = "other"
			if _, err := s.File(other); err != nil {
				t.Fatal(err)
			}
			different := input
			different.StdinDigest = "changed"
			if _, err := s.File(different); err != nil {
				t.Fatal("different digest", err)
			}
			*now = deniedAt.Add(10 * time.Minute)
			if _, err := reopened.File(input); err != nil {
				t.Fatal("exact boundary", err)
			}
		})
	}
}
func TestConsumeDispatchCrashKeepsDurableSpentUse(t *testing.T) {
	s, _, input := fixture(t)
	q, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Decide(q.ID, "pending", Decision{})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("no simulated crash")
			}
		}()
		s.ConsumeDispatchContext(context.Background(), q.Workspace, q.Jail, q.Set, q.Digest, s.Revalidate, func() error { panic("process died at dispatch") })
	}()
	reopened := &Store{Dir: s.Dir, Revalidate: unchangedPolicy, Now: s.Now}
	_, gs, err := reopened.List()
	if err != nil || gs[0].ID != g.ID || gs[0].Uses != 0 {
		t.Fatal(gs, err)
	}
	if _, ok, err := reopened.Consume(q.Workspace, q.Jail, q.Set, q.Digest, reopened.Revalidate); err != nil || ok {
		t.Fatal("crash refunded", ok, err)
	}
}
