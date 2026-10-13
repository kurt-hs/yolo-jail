package brokerstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestCurrentGateRequiredForApprovalAndConsumption(t *testing.T) {
	s, _, input := fixture(t)
	q, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	s.Revalidate = nil
	if _, err := s.Decide(q.ID, "pending", Decision{}); err == nil {
		t.Fatal("approved without current gate")
	}
	s.Revalidate = unchangedPolicy
	if _, err := s.Decide(q.ID, "pending", Decision{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Consume(q.Jail, q.Start, q.Set, q.Digest, nil); err == nil || ok {
		t.Fatal("consumed without gate")
	}
}

func scopeFixture(t *testing.T) (*Store, Request, brokerscope.File, string, Revalidate) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, _, q := fixture(t)
	s.Dir = paths.BrokerSourceDir("github")
	sf := brokerscope.File{Source: "github", LaunchID: "transaction", Workspace: q.Workspace, Container: q.Jail, PID: os.Getpid(), Repos: []string{q.Repo}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	gate := func(r Request) (Policy, error) {
		if r.Start != q.Start || r.Jail != q.Jail {
			return Policy{}, errors.New("stale holder")
		}
		if err := brokerscope.ValidateCurrent(path, sf, r.Repo); err != nil {
			return Policy{}, err
		}
		return q.Policy, nil
	}
	s.Revalidate = gate
	filed, err := s.File(q)
	if err != nil {
		t.Fatal(err)
	}
	return s, filed, sf, path, gate
}

func TestCurrentScopeAndPolicyFailClosedWithoutSpending(t *testing.T) {
	for _, change := range []string{"scope", "missing", "corrupt", "unknown", "stale", "policy", "start"} {
		t.Run(change, func(t *testing.T) {
			s, q, sf, path, gate := scopeFixture(t)
			g, err := s.Decide(q.ID, "pending", Decision{For: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "scope":
				sf.Repos = nil
				_, err = brokerscope.Write(sf)
			case "missing":
				err = brokerscope.Remove(path)
			case "corrupt":
				err = os.WriteFile(path, []byte("{"), 0600)
			case "unknown":
				err = os.WriteFile(path, []byte(`{"version":99}`), 0600)
			case "stale":
				sf.LaunchID = "different"
				data := []byte(`{"version":1,"source":"github","launch_id":"different"}`)
				err = os.WriteFile(path, data, 0600)
			case "policy":
				old := gate
				gate = func(r Request) (Policy, error) { p, e := old(r); p.Uses = 1; return p, e }
			case "start":
				gate = func(Request) (Policy, error) { return Policy{}, errors.New("ended broker start") }
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, e := s.Consume(q.Jail, q.Start, q.Set, q.Digest, gate); ok || e == nil {
				t.Fatal(ok, e)
			}
			_, grants, e := s.List()
			if e != nil || len(grants) != 1 || grants[0].Uses != g.Uses {
				t.Fatal(grants, e)
			}
		})
	}
}

// Pause a use inside its gate; the REAL scope writer must wait for that same
// transaction. After it narrows scope, the next use must fail without spending.
func TestActualScopeWriterSerializesWithConsumeGate(t *testing.T) {
	s, q, sf, _, gate := scopeFixture(t)
	g, err := s.Decide(q.ID, "pending", Decision{For: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, ok, e := s.Consume(q.Jail, q.Start, q.Set, q.Digest, func(r Request) (Policy, error) { close(entered); <-release; return gate(r) })
		if !ok && e == nil {
			e = errors.New("unchanged use refused")
		}
		result <- e
	}()
	<-entered
	written := make(chan error, 1)
	begun := make(chan struct{})
	go func() { close(begun); sf.Repos = nil; _, e := brokerscope.Write(sf); written <- e }()
	<-begun
	select {
	case e := <-written:
		t.Fatalf("scope write escaped gate transaction: %v", e)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	if e := <-written; e != nil {
		t.Fatal(e)
	}
	if _, ok, e := s.Consume(q.Jail, q.Start, q.Set, q.Digest, gate); ok || e == nil {
		t.Fatal(ok, e)
	}
	_, gs, e := s.List()
	if e != nil || gs[0].Uses != g.Uses-1 {
		t.Fatal(gs, e)
	}
}

func TestPolicyNarrowingWhileConsumerWaits(t *testing.T) {
	s, q, _, _, gate := scopeFixture(t)
	if _, err := s.Decide(q.ID, "pending", Decision{}); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	data, _ := json.Marshal(q.Policy)
	if err := os.WriteFile(policyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	started := make(chan struct{})
	err := brokerlock.WithTransaction(s.Dir, func() error {
		go func() {
			close(started)
			_, ok, e := s.Consume(q.Jail, q.Start, q.Set, q.Digest, func(r Request) (Policy, error) {
				_, e := gate(r)
				if e != nil {
					return Policy{}, e
				}
				data, e := os.ReadFile(policyPath)
				if e != nil {
					return Policy{}, e
				}
				var p Policy
				e = json.Unmarshal(data, &p)
				return p, e
			})
			if ok {
				e = errors.New("spent narrowed policy")
			}
			result <- e
		}()
		<-started
		p := q.Policy
		p.MaxWindow = time.Minute
		p.Window = time.Minute
		p.Session = false
		data, _ := json.Marshal(p)
		return os.WriteFile(policyPath, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := <-result; e == nil {
		t.Fatal("narrowed policy accepted")
	}
}

func TestEndSerializesWithConsumption(t *testing.T) {
	s, _, input := fixture(t)
	q, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(q.ID, "pending", Decision{For: time.Minute}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	consumed := make(chan error, 1)
	go func() {
		_, ok, e := s.Consume(q.Jail, q.Start, q.Set, q.Digest, func(r Request) (Policy, error) { close(entered); <-release; return unchangedPolicy(r) })
		if !ok && e == nil {
			e = errors.New("use refused")
		}
		consumed <- e
	}()
	<-entered
	ended := make(chan error, 1)
	go func() {
		gs, e := s.End(q.Jail, q.Start)
		if e == nil && len(gs) != 1 {
			e = errors.New("wrong ended grants")
		}
		ended <- e
	}()
	select {
	case e := <-ended:
		t.Fatalf("end escaped transaction: %v", e)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if e := <-consumed; e != nil {
		t.Fatal(e)
	}
	if e := <-ended; e != nil {
		t.Fatal(e)
	}
	if _, ok, e := s.Consume(q.Jail, q.Start, q.Set, q.Digest, unchangedPolicy); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestDecisionUsesCurrentCeilingNotFiledPolicy(t *testing.T) {
	s, now, input := fixture(t)
	q, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	s.Revalidate = func(Request) (Policy, error) {
		return Policy{Window: time.Minute, MaxWindow: 2 * time.Minute, Uses: 2}, nil
	}
	if _, err = s.Decide(q.ID, "pending", Decision{For: time.Hour}); err == nil {
		t.Fatal("filed ceiling bypassed current policy")
	}
	g, err := s.Decide(q.ID, "pending", Decision{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Expires.Sub(*now) != 2*time.Minute || g.Uses != 1 {
		t.Fatal(g)
	}
}
