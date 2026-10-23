package brokerstore

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

// watch.go is the store's stable surface for front ends yolo does not ship
// (boundary-broker.md §6.6, BB-D72): a snapshot, the events between two snapshots, a
// polling watch, and the JSON shape of each. The store has no change notification of its
// own (a transaction rewrites whole records under a lock, and expiry happens lazily inside
// whichever transaction runs next), so a watch POLLS: each tick is one List transaction,
// which also applies any expiry that came due, and the events are the difference from the
// previous tick.

// Schema is the version every JSON object of the surface carries as "schema". A field is
// only ever added within one number; a removal or a change of meaning bumps it.
const Schema = 1

// The events a watch reports.
const (
	EventRequestFiled   = "request-filed"
	EventRequestDecided = "request-decided" // allowed, denied, or refused because its jail stopped
	EventRequestExpired = "request-expired"
	EventGrantUsed      = "grant-used"
	EventGrantEnded     = "grant-ended" // expired, used up, or its jail stopped
	EventGrantRevoked   = "grant-revoked"
)

// Snapshot is every record in the store, requests by filing time and grants by expiry.
type Snapshot struct {
	Requests []Request
	Grants   []Grant
}

// Snapshot reads the store in one transaction.
func (s *Store) Snapshot() (Snapshot, error) {
	rs, gs, err := s.List()
	sort.SliceStable(rs, func(i, j int) bool {
		if !rs[i].Filed.Equal(rs[j].Filed) {
			return rs[i].Filed.Before(rs[j].Filed)
		}
		return rs[i].ID < rs[j].ID
	})
	sort.SliceStable(gs, func(i, j int) bool {
		if !gs[i].Expires.Equal(gs[j].Expires) {
			return gs[i].Expires.Before(gs[j].Expires)
		}
		return gs[i].ID < gs[j].ID
	})
	return Snapshot{Requests: rs, Grants: gs}, err
}

// Event is one change between two snapshots. Request is the request it concerns, or a
// grant's parent request when the store still holds it; Grant is set for a grant event and
// for a decision that issued one.
type Event struct {
	Kind    string
	Request *Request
	Grant   *Grant
	// Current is, for a request-filed event, the request's state in the snapshot that
	// reported its filing: "pending", or its later state when it was filed and decided
	// between two polls (Request then reads "pending", as filed). It is not in the JSON view.
	Current string
}

// Diff is the events that turn prev into cur, requests first. A record that disappears
// (the store prunes ended records after 7 days) is no event.
func Diff(prev, cur Snapshot) []Event {
	oldReq := make(map[string]Request, len(prev.Requests))
	for _, q := range prev.Requests {
		oldReq[q.ID] = q
	}
	oldGrant := make(map[string]Grant, len(prev.Grants))
	for _, g := range prev.Grants {
		oldGrant[g.ID] = g
	}
	reqByID := make(map[string]Request, len(cur.Requests))
	for _, q := range cur.Requests {
		reqByID[q.ID] = q
	}
	issued := make(map[string]Grant)
	for _, g := range cur.Grants {
		if _, seen := oldGrant[g.ID]; !seen {
			issued[g.Request] = g
		}
	}
	var out []Event
	for _, q := range cur.Requests {
		old, seen := oldReq[q.ID]
		if !seen {
			filed := q
			filed.State, filed.Decided = "pending", time.Time{}
			out = append(out, Event{Kind: EventRequestFiled, Request: &filed, Current: q.State})
		}
		if q.State != "pending" && (!seen || old.State == "pending") {
			q := q
			ev := Event{Kind: EventRequestDecided, Request: &q}
			if q.State == "expired" {
				ev.Kind = EventRequestExpired
			}
			if g, ok := issued[q.ID]; ok {
				ev.Grant = &g
			}
			out = append(out, ev)
		}
	}
	for _, g := range cur.Grants {
		g := g
		var parent *Request
		if q, ok := reqByID[g.Request]; ok {
			parent = &q
		}
		old, seen := oldGrant[g.ID]
		// A grant first seen already used (issued and used inside one tick, the common flow
		// for Allow once) still reports its use.
		if (seen && g.Uses < old.Uses) || (!seen && ((g.Issued > 0 && g.Uses < g.Issued) || g.EndedBy() == EndUsed)) {
			out = append(out, Event{Kind: EventGrantUsed, Request: parent, Grant: &g})
		}
		if !g.Ended.IsZero() && (!seen || old.Ended.IsZero()) {
			kind := EventGrantEnded
			if g.EndedBy() == EndRevoked {
				kind = EventGrantRevoked
			}
			out = append(out, Event{Kind: kind, Request: parent, Grant: &g})
		}
	}
	return out
}

// Watch reads the store, hands that snapshot to start, then polls every interval and hands
// each event to emit until ctx ends. It returns the first read's error; a later failed read
// goes to warn and the next tick tries again, so a watch outlives a passing fault.
func (s *Store) Watch(ctx context.Context, interval time.Duration, start func(Snapshot), emit func(Event), warn func(error)) error {
	prev, err := s.Snapshot()
	if err != nil {
		return err
	}
	if start != nil {
		start(prev)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		cur, err := s.Snapshot()
		if err != nil {
			if warn != nil {
				warn(err)
			}
			continue
		}
		for _, ev := range Diff(prev, cur) {
			emit(ev)
		}
		prev = cur
	}
}

// RequestView is a request as the surface shows it.
type RequestView struct {
	Schema    int        `json:"schema"`
	ID        string     `json:"id"`
	Source    string     `json:"source"`
	Jail      string     `json:"jail"`
	Workspace string     `json:"workspace"`
	Set       string     `json:"set"`
	Argv      []string   `json:"argv"`    // the command the classifier rebuilt, after the program
	Command   string     `json:"command"` // Argv quoted for reading
	Repo      string     `json:"repo"`
	Filed     time.Time  `json:"filed"`
	Expires   time.Time  `json:"expires"`
	Decided   *time.Time `json:"decided,omitempty"`
	State     string     `json:"state"` // pending, allowed, denied, expired, refused
	// HookFailure and HookOutput say the user's on_request failed for this request
	// (Request.HookFailure); absent when it did not.
	HookFailure string `json:"hook_failure,omitempty"`
	HookOutput  string `json:"hook_output,omitempty"`
}

// GrantView is a grant as the surface shows it.
type GrantView struct {
	Schema    int        `json:"schema"`
	ID        string     `json:"id"`
	Source    string     `json:"source"`
	Request   string     `json:"request"`
	Jail      string     `json:"jail"` // the holder
	Set       string     `json:"set"`
	Coverage  string     `json:"coverage"` // "command": the request's one command; "set": the whole set
	UsesLeft  int        `json:"uses_left"`
	Expires   time.Time  `json:"expires"`
	Ended     *time.Time `json:"ended,omitempty"`
	EndReason string     `json:"end_reason,omitempty"` // expired, used, revoked, stopped
	State     string     `json:"state"`                // live or ended
}

// StateView is the current state: pending requests and live grants.
type StateView struct {
	Schema   int           `json:"schema"`
	Event    string        `json:"event,omitempty"` // "state" when it opens a watch
	Requests []RequestView `json:"requests"`
	Grants   []GrantView   `json:"grants"`
}

// EventView is one watch event.
type EventView struct {
	Schema  int          `json:"schema"`
	Event   string       `json:"event"`
	Source  string       `json:"source"`
	Time    time.Time    `json:"time"` // when the watch saw it
	Summary string       `json:"summary"`
	Request *RequestView `json:"request,omitempty"`
	Grant   *GrantView   `json:"grant,omitempty"`
}

func (s *Store) RequestView(q Request) RequestView {
	v := RequestView{Schema: Schema, ID: q.ID, Source: s.SourceName(), Jail: q.Jail, Workspace: q.Workspace,
		Set: q.Set, Argv: q.Argv, Command: shquote.JoinDisplay(q.Argv), Repo: q.Repo, Filed: q.Filed,
		Expires: q.Expires(), State: q.State, HookFailure: q.HookFailure, HookOutput: q.HookOutput}
	if v.Argv == nil {
		v.Argv = []string{}
	}
	if !q.Decided.IsZero() {
		d := q.Decided
		v.Decided = &d
	}
	return v
}

func (s *Store) GrantView(g Grant) GrantView {
	v := GrantView{Schema: Schema, ID: g.ID, Source: s.SourceName(), Request: g.Request, Jail: g.Jail, Set: g.Set,
		Coverage: "set", UsesLeft: g.Uses, Expires: g.Expires, EndReason: g.EndedBy(), State: "live"}
	if g.Digest != "" {
		v.Coverage = "command"
	}
	if !g.Ended.IsZero() {
		e := g.Ended
		v.Ended, v.State = &e, "ended"
	}
	return v
}

// StateView keeps what is still open: pending requests and live grants.
func (s *Store) StateView(snap Snapshot) StateView {
	v := StateView{Schema: Schema, Requests: []RequestView{}, Grants: []GrantView{}}
	for _, q := range snap.Requests {
		if q.State == "pending" {
			v.Requests = append(v.Requests, s.RequestView(q))
		}
	}
	for _, g := range snap.Grants {
		if g.Ended.IsZero() {
			v.Grants = append(v.Grants, s.GrantView(g))
		}
	}
	return v
}

func (s *Store) EventView(ev Event, at time.Time) EventView {
	v := EventView{Schema: Schema, Event: ev.Kind, Source: s.SourceName(), Time: at.UTC(), Summary: s.Summary(ev)}
	if ev.Request != nil {
		r := s.RequestView(*ev.Request)
		v.Request = &r
	}
	if ev.Grant != nil {
		g := s.GrantView(*ev.Grant)
		v.Grant = &g
	}
	return v
}

// SummaryMax bounds a request summary in bytes. It reaches a hook as one environment
// variable, and Linux refuses any single one over 128 KiB (MAX_ARG_STRLEN), so it stays far
// below that; a notification shows less still.
const SummaryMax = 2048

// RequestSummary is the one line a person reads about a request. What is being approved
// comes first: the request id, the set, the repository and the jail; the command comes
// last, each value over 60 characters elided to its first 40, its length and a short hash
// (boundary-broker.md §6.3), so a long --body cannot push the rest off screen. The line is
// cut at SummaryMax, and nothing in it can drive a terminal.
func (s *Store) RequestSummary(q Request) string {
	argv := make([]string, len(q.Argv))
	for i, v := range q.Argv {
		argv[i] = elide(v)
	}
	line := termsafe.Visible(fmt.Sprintf("%s %s: set %s on %s for jail %s, to run: %s",
		s.SourceName(), q.ID, elide(q.Set), elide(q.Repo), elide(q.Jail), shquote.JoinDisplay(argv)))
	if len(line) <= SummaryMax {
		return line
	}
	const more = "…"
	cut := SummaryMax - len(more)
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + more
}

// elide shortens a value over 60 characters to its first 40, its length and a short hash.
func elide(v string) string {
	r := []rune(v)
	if len(r) <= 60 {
		return v
	}
	sum := sha256.Sum256([]byte(v))
	return fmt.Sprintf("%s…[%d chars, sha256 %x]", string(r[:40]), len(r), sum[:4])
}

// Summary is an event's one line.
func (s *Store) Summary(ev Event) string {
	if ev.Grant == nil || ev.Kind == EventRequestDecided {
		if ev.Request == nil {
			return ev.Kind
		}
		line := ev.Kind + " " + s.RequestSummary(*ev.Request)
		if ev.Kind == EventRequestDecided {
			line += ": " + ev.Request.State
		}
		return line
	}
	g := ev.Grant
	coverage := "whole set " + g.Set
	if g.Digest != "" {
		coverage = "one command, set " + g.Set
	}
	line := fmt.Sprintf("%s %s %s: %s for jail %s, %d use(s) left, expires %s", ev.Kind, s.SourceName(), g.ID,
		coverage, g.Jail, g.Uses, g.Expires.Format(time.RFC3339))
	if r := g.EndedBy(); r != "" {
		line += ", ended (" + r + ")"
	}
	return termsafe.Visible(line)
}
