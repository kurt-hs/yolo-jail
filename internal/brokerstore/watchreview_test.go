package brokerstore

import (
	"strings"
	"testing"
	"time"
)

// A grant issued and used between two reads, the common flow for Allow once, still reports
// its use: once used up, and once with uses left.
func TestDiffReportsAUseOfAGrantFirstSeenUsed(t *testing.T) {
	s, _, input := fixture(t)
	consume := func(digest string) {
		if _, ok, err := s.Consume(input.Workspace, input.Jail, input.Set, digest, s.Revalidate); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	got, _ := kinds(t, s, func() {
		r, err := s.File(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Decide(r.ID, "pending", Decision{}); err != nil {
			t.Fatal(err)
		}
		consume(r.Digest)
	})
	wantKinds(t, got, EventRequestFiled, EventRequestDecided, EventGrantUsed, EventGrantEnded)

	input.Argv = []string{"pr", "merge", "9"}
	got, evs := kinds(t, s, func() {
		r, err := s.File(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Decide(r.ID, "pending", Decision{For: time.Hour, Uses: 3}); err != nil {
			t.Fatal(err)
		}
		consume(r.Digest)
	})
	wantKinds(t, got, EventRequestFiled, EventRequestDecided, EventGrantUsed)
	if g := evs[2].Grant; g.Uses != 2 || g.Issued != 3 {
		t.Fatalf("used grant: %+v", g)
	}

	// A record written before Issued existed: an unused grant reports no use, a used-up one does.
	unused := Snapshot{Grants: []Grant{{ID: "g-old", Uses: 2, Expires: time.Now().Add(time.Hour)}}}
	wantKinds(t, kindsOf(Diff(Snapshot{}, unused)))
	usedUp := Snapshot{Grants: []Grant{{ID: "g-old", Uses: 0, Expires: time.Now().Add(time.Hour), Ended: time.Now()}}}
	wantKinds(t, kindsOf(Diff(Snapshot{}, usedUp)), EventGrantUsed, EventGrantEnded)
}

func kindsOf(evs []Event) []string {
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out
}

// The summary leads with what is approved, elides a long value with its length and a
// hash, and stays far below Linux's per-string limit however long the command is.
func TestRequestSummaryLeadsWithTheSetAndPreservesDigestsWhenCommandIsLong(t *testing.T) {
	s, _, input := fixture(t)
	value := strings.Repeat("x", 3000)
	input.Argv = []string{"pr", "comment", "1", "--label", value}
	r, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	line := s.RequestSummary(r)
	head := "github " + r.ID + ": set " + input.Set + " on " + input.Repo + " for jail " + input.Jail + ", to run: "
	if !strings.HasPrefix(line, head) {
		t.Fatalf("summary %q, want it to start %q", line, head)
	}
	if strings.Contains(line, strings.Repeat("x", 41)) || !strings.Contains(line, strings.Repeat("x", 40)+"…[3000 chars, sha256 ") {
		t.Fatalf("the long argument is not elided: %q", line)
	}
	if !strings.Contains(line, "pr comment 1 --label ") {
		t.Fatalf("a short value was elided: %q", line)
	}
	if !strings.Contains(line, "command SHA-256 "+r.Digest) || !strings.Contains(line, "stdin SHA-256 "+r.StdinDigest) || strings.Contains(line, "private body") {
		t.Fatalf("the complete safe action identity is missing: %q", line)
	}

	many := make([]string, 5000)
	for i := range many {
		many[i] = "--flag"
	}
	r.Argv = many
	line = s.RequestSummary(r)
	if len(line) > SummaryMax || !strings.HasPrefix(line, head) || !strings.Contains(line, "… [command SHA-256 "+r.Digest+"; stdin SHA-256 "+r.StdinDigest+"]") {
		t.Fatalf("long command hid identity or exceeded %d bytes: %d bytes: %q", SummaryMax, len(line), line)
	}
}

func TestRequestSummaryRedactsBodyAndHeaderValues(t *testing.T) {
	s, _, input := fixture(t)
	input.Argv = []string{"api", "repos/o/r/issues/1", "--field", "body=private body text", "--body=private inline body", "-H", "Authorization: Bearer private-token", "-HAuthorization: Bearer inline-token"}
	r, err := s.File(input)
	if err != nil {
		t.Fatal(err)
	}
	line := s.RequestSummary(r)
	for _, secret := range []string{"private body text", "private inline body", "private-token", "inline-token", "Authorization: Bearer"} {
		if strings.Contains(line, secret) {
			t.Fatalf("request summary exposed sensitive argument %q: %q", secret, line)
		}
	}
	for _, want := range []string{"--field '[redacted]'", "--body=[redacted]", "-H '[redacted]'", "-H[redacted]", "command SHA-256 " + r.Digest, "stdin SHA-256 " + r.StdinDigest} {
		if !strings.Contains(line, want) {
			t.Fatalf("request summary lacks safe value %q: %q", want, line)
		}
	}
}
