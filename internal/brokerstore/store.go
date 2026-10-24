// Package brokerstore is the host-only request/grant API of boundary-broker.md §7.
// Broker calls use it only behind required-delivery readiness; production writes
// still refuse without filing a request.
package brokerstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
)

const sessionLimit = 12 * time.Hour

// PendingLife is how long a filed request waits for an answer before it expires.
const PendingLife = time.Hour

// The kinds of refusal a front end reports with its own stable exit code
// (boundary-broker.md §6.6). Match them with errors.Is; the error's text is the message.
var (
	ErrAlreadyDecided  = errors.New("already decided")
	ErrUnknownID       = errors.New("unknown id")
	ErrRefusedByPolicy = errors.New("refused by policy") // the decision asks beyond the set's ceiling
	// ErrNotConfirmed is a pending request the host can no longer vouch for: its jail's
	// launch is gone without having ended it (a broker killed outright), its scope file is
	// missing, or its set or classification changed. No decision can allow it.
	ErrNotConfirmed = errors.New("not confirmed by the host")
)

// kindError carries its full message and one of the kinds above.
type kindError struct {
	msg  string
	kind error
}

func (e *kindError) Error() string { return e.msg }
func (e *kindError) Unwrap() error { return e.kind }
func refusal(kind error, format string, args ...any) error {
	return &kindError{msg: fmt.Sprintf(format, args...), kind: kind}
}

// The reasons a grant ends, recorded in Grant.EndReason.
const (
	EndExpired = "expired" // its window ran out
	EndUsed    = "used"    // its last use was spent
	EndRevoked = "revoked" // a human revoked it
	EndStopped = "stopped" // the broker lifetime holding it ended
)

// Policy is the resolved windowed set policy, supplied by the HOST broker after
// classification and scope checks, never decoded from a jail's request frame.
// Session distinguishes a session ceiling from an ordinary duration ceiling.
type Policy struct {
	Window    time.Duration
	MaxWindow time.Duration
	Session   bool
	Uses      int
}
type Request struct {
	ID                             string
	LaunchID                       string // host launch scope identity
	Jail, Start, Workspace         string // host-asserted jail identity and random broker lifetime
	Argv                           []string
	Repo, StdinDigest, Digest, Set string
	Policy                         Policy
	Filed, Decided                 time.Time
	State                          string
	// HookFailure is one line saying the user's on_request failed for this request, such
	// as "hook exited 127: sh: notify-send: not found"; HookOutput is the last of what it
	// printed. Both are terminal-safe, and empty unless it failed (NoteHookFailure).
	HookFailure string `json:",omitempty"`
	HookOutput  string `json:",omitempty"`
}

// Expires is when the request stops waiting for an answer, whatever its state now.
func (q Request) Expires() time.Time { return q.Filed.Add(PendingLife) }

type Grant struct {
	ID, Request, Jail, Start, Set, Digest string // empty Digest means the whole requested set
	Expires, Ended                        time.Time
	Uses                                  int
	// EndReason is one of the End* constants once Ended is set. A record written before
	// the field existed has none; EndedBy infers one.
	EndReason string `json:",omitempty"`
	// Issued is the uses the decision granted, so a watch that first sees a grant after it
	// was used can still tell. A record written before the field existed has zero.
	Issued int `json:",omitempty"`
}

// EndedBy is why the grant ended, "" while it is live.
func (g Grant) EndedBy() string {
	switch {
	case g.Ended.IsZero():
		return ""
	case g.EndReason != "":
		return g.EndReason
	case g.Ended.Equal(g.Expires):
		return EndExpired
	case g.Uses == 0:
		return EndUsed
	}
	return EndStopped
}

// Decision cannot change the holder, set or command. Zero For means Allow once.
type Decision struct {
	Deny    bool
	For     time.Duration
	Session bool
	Uses    int
}
type Store struct {
	Dir string
	// Source names the broker source this store serves; empty means the directory's name.
	Source string
	Now    func() time.Time
	// Revalidate is mandatory for approving decisions; it reads current host authority
	// under the transaction lock. Nil fails closed. Deny/revoke need no authority.
	Revalidate Revalidate
	// Audit, when supplied by a host adapter, runs under the shared transaction.
	Audit func(context.Context, string, Request, Grant) error
}

// Revalidate must re-read approved scope, current host policy, and live jail/start
// identity for this request. At consumption it must also classify and scope-check
// the ACTUAL invocation captured by the caller (a set grant can cover a different
// command/repository from the filed request). Never scope-check only the parent.
// It must return an error for unknown/stale authority,
// must not acquire the transaction lock, and must never trust jail-provided policy.
type Revalidate func(Request) (Policy, error)

func currentPolicy(q Request, gate Revalidate) (Policy, error) {
	if gate == nil {
		return Policy{}, errors.New("current authority gate unavailable; retry after the host broker policy adapter is installed")
	}
	p, err := gate(q)
	if err != nil {
		return Policy{}, err
	}
	if err = validatePolicy(p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// SourceName is Source, or the store directory's name.
func (s *Store) SourceName() string {
	if s.Source != "" {
		return s.Source
	}
	return filepath.Base(s.Dir)
}

func (s *Store) audit(ctx context.Context, kind string, q Request, g Grant) error {
	if s.Audit != nil {
		return s.Audit(ctx, kind, q, g)
	}
	return nil
}

// CommandDigest binds canonical argv, repository and stdin digest, never a policy.
func CommandDigest(q Request) string { return digest(q) }

// Status reveals a request only to its exact authenticated holder and broker start.
func (s *Store) Status(jail, start, id string) (Request, error) {
	return s.StatusContext(context.Background(), jail, start, id)
}
func (s *Store) StatusContext(ctx context.Context, jail, start, id string) (Request, error) {
	var out Request
	err := s.transactionContext(ctx, func(r *records, _ time.Time) error {
		for _, q := range r.requests {
			if q.ID == id && q.Jail == jail && q.Start == start {
				out = q
				return nil
			}
		}
		return errors.New("request not found for this broker lifetime")
	})
	return out, err
}

type records struct {
	requests []Request
	grants   []Grant
	working  []WorkingEntry
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("store directory is not a real directory: %s", path)
	}
	return os.Chmod(path, 0700)
}

// transaction is the only mutator. A grant is usable only after its parent request
// is durably allowed; a crash between the two renames can leave no usable authority.
func (s *Store) transaction(fn func(*records, time.Time) error) error {
	return s.transactionContext(context.Background(), fn)
}
func (s *Store) transactionContext(ctx context.Context, fn func(*records, time.Time) error) error {
	if err := privateDir(s.Dir); err != nil {
		return err
	}
	return brokerlock.WithTransactionContext(ctx, s.Dir, func() error {
		var err error
		for _, name := range []string{"requests", "grants", "working"} {
			if err := privateDir(filepath.Join(s.Dir, name)); err != nil {
				return err
			}
		}
		var r records
		if err = readRecords(filepath.Join(s.Dir, "requests"), &r.requests); err != nil {
			return err
		}
		if err = readRecords(filepath.Join(s.Dir, "grants"), &r.grants); err != nil {
			return err
		}
		if err = readRecords(filepath.Join(s.Dir, "working"), &r.working); err != nil {
			return err
		}
		now := s.now()
		for i := range r.requests {
			q := &r.requests[i]
			if q.State == "pending" && !now.Before(q.Expires()) {
				q.State = "expired"
				q.Decided = q.Expires()
				if err = s.write("requests", q.ID, q); err != nil {
					return err
				}
			}
		}
		for i := range r.grants {
			g := &r.grants[i]
			if g.Ended.IsZero() && !now.Before(g.Expires) {
				g.Ended = g.Expires
				g.EndReason = EndExpired
				if err = s.write("grants", g.ID, g); err != nil {
					return err
				}
			}
		}
		for i := len(r.grants) - 1; i >= 0; i-- {
			g := r.grants[i]
			if !g.Ended.IsZero() && !now.Before(g.Ended.Add(7*24*time.Hour)) {
				if err = os.Remove(filepath.Join(s.Dir, "grants", g.ID+".json")); err != nil {
					return err
				}
				r.grants = append(r.grants[:i], r.grants[i+1:]...)
			}
		}
		for i := len(r.working) - 1; i >= 0; i-- {
			w := r.working[i]
			if !w.Live() && !now.Before(w.Ended.Add(7*24*time.Hour)) {
				if err = os.Remove(filepath.Join(s.Dir, "working", w.ID+".json")); err != nil {
					return err
				}
				r.working = append(r.working[:i], r.working[i+1:]...)
			}
		}
		for i := len(r.requests) - 1; i >= 0; i-- {
			q := r.requests[i]
			if !q.Decided.IsZero() && !now.Before(q.Decided.Add(7*24*time.Hour)) {
				live := false
				for _, g := range r.grants {
					if g.Request == q.ID {
						live = true
					}
				}
				if !live {
					if err = os.Remove(filepath.Join(s.Dir, "requests", q.ID+".json")); err != nil {
						return err
					}
					r.requests = append(r.requests[:i], r.requests[i+1:]...)
				}
			}
		}
		return fn(&r, now)
	})
}
func validID(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) != 34 {
		return false
	}
	_, err := hex.DecodeString(id[2:])
	return err == nil
}
func readRecords[T any](dir string, out *[]T) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if !validID(id, recordPrefix(filepath.Base(dir))) {
			return fmt.Errorf("invalid store record name %q", e.Name())
		}
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		fi, err := f.Stat()
		if err != nil || !fi.Mode().IsRegular() {
			f.Close()
			return fmt.Errorf("store record is not a regular file: %s", e.Name())
		}
		b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
		f.Close()
		if err != nil {
			return err
		}
		if len(b) > 1<<20 {
			return errors.New("store record exceeds 1 MiB")
		}
		var v T
		if err = json.Unmarshal(b, &v); err != nil {
			return err
		}
		*out = append(*out, v)
	}
	return nil
}

// recordPrefix is the id prefix of a record kind's directory: r- requests, g- grants and
// w- working-set entries.
func recordPrefix(kind string) string {
	switch kind {
	case "grants":
		return "g-"
	case "working":
		return "w-"
	}
	return "r-"
}

func (s *Store) write(kind, id string, v any) error {
	if !validID(id, recordPrefix(kind)) {
		return errors.New("invalid record identity")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return errors.New("store record exceeds 1 MiB")
	}
	dir := filepath.Join(s.Dir, kind)
	f, err := os.CreateTemp(dir, ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// NewStartID draws the host broker lifetime identity (BB-D27).
func NewStartID() (string, error) { return newID("") }

func newID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
func digest(q Request) string {
	b, _ := json.Marshal([]any{q.Argv, q.Repo, q.StdinDigest})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func validatePolicy(p Policy) error {
	if p.Window <= 0 || p.MaxWindow <= 0 || p.Window > p.MaxWindow || p.Uses <= 0 || (p.Session && p.MaxWindow != sessionLimit) {
		return errors.New("invalid resolved windowed set policy")
	}
	return nil
}

// File joins an identical pending command only in this jail, lifetime and set.
func (s *Store) File(input Request) (Request, error) {
	return s.FileContext(context.Background(), input)
}
func (s *Store) FileContext(ctx context.Context, input Request) (Request, error) {
	var out Request
	if input.Jail == "" || input.Start == "" || input.Workspace == "" || input.Set == "" || input.Repo == "" || len(input.Argv) == 0 || input.StdinDigest == "" {
		return out, errors.New("request needs host holder, workspace, set, command, repository and stdin digest")
	}
	if err := validatePolicy(input.Policy); err != nil {
		return out, err
	}
	err := s.transactionContext(ctx, func(r *records, now time.Time) error {
		if s.Revalidate != nil {
			if _, err := currentPolicy(input, s.Revalidate); err != nil {
				return err
			}
		}
		input.Digest = digest(input)
		for _, q := range r.requests {
			if q.Jail == input.Jail && q.Start == input.Start && q.Set == input.Set && q.Digest == input.Digest && q.State == "pending" {
				out = q
				return nil
			}
		}
		if err := filingLimit(r, input, now); err != nil {
			return err
		}
		id, err := newID("r-")
		if err != nil {
			return err
		}
		input.ID = id
		input.Filed = now
		input.Decided = time.Time{}
		input.State = "pending"
		out = input
		if err := s.audit(ctx, "request", input, Grant{}); err != nil {
			return err
		}
		return s.write("requests", id, input)
	})
	return out, err
}
func (s *Store) List() ([]Request, []Grant, error) {
	var out records
	err := s.transaction(func(r *records, _ time.Time) error { out = *r; return nil })
	return out.requests, out.grants, err
}

// Decide compares the state seen by the frontend; only pending can transition.
func (s *Store) Decide(id, expected string, d Decision) (Grant, error) {
	var out Grant
	err := s.transaction(func(r *records, now time.Time) error {
		for i := range r.requests {
			q := &r.requests[i]
			if q.ID != id {
				continue
			}
			if q.State != expected || q.State != "pending" {
				return refusal(ErrAlreadyDecided, "request %s is already %s", id, q.State)
			}
			if err := validatePolicy(q.Policy); err != nil {
				return refusal(ErrNotConfirmed, "%v", err)
			}
			kind := "decision"
			if d.Deny {
				if d.For != 0 || d.Session || d.Uses != 0 {
					return errors.New("deny cannot also grant")
				}
				if err := s.audit(context.Background(), kind, *q, Grant{}); err != nil {
					return err
				}
				q.State = "denied"
				q.Decided = now
				return s.write("requests", q.ID, q)
			}
			current, err := currentPolicy(*q, s.Revalidate)
			if err != nil {
				return refusal(ErrNotConfirmed, "%v", err)
			}
			// Frontends cannot widen either the filed request or today's ceiling.
			q.Policy.Window = min(q.Policy.Window, current.Window)
			q.Policy.MaxWindow = min(q.Policy.MaxWindow, current.MaxWindow)
			q.Policy.Uses = min(q.Policy.Uses, current.Uses)
			q.Policy.Session = q.Policy.Session && current.Session
			duration := min(15*time.Minute, q.Policy.MaxWindow)
			uses := 1
			command := q.Digest
			if d.Session && d.For != 0 {
				return errors.New("choose a duration or session, not both")
			}
			if d.For < 0 {
				return errors.New("grant duration must be positive")
			}
			if d.For != 0 || d.Session {
				duration = d.For
				if d.Session {
					if !q.Policy.Session {
						return refusal(ErrRefusedByPolicy, "session exceeds this set's policy ceiling")
					}
					duration = sessionLimit
				}
				if duration <= 0 || duration > q.Policy.MaxWindow {
					return refusal(ErrRefusedByPolicy, "duration exceeds this set's policy ceiling")
				}
				uses = q.Policy.Uses
				command = ""
			}
			if d.Uses != 0 {
				if d.Uses < 1 || d.Uses > uses {
					return refusal(ErrRefusedByPolicy, "uses exceed this grant's policy ceiling")
				}
				uses = d.Uses
			}
			gid, err := newID("g-")
			if err != nil {
				return err
			}
			out = Grant{ID: gid, Request: q.ID, Jail: q.Jail, Start: q.Start, Set: q.Set, Digest: command, Expires: now.Add(duration), Uses: uses, Issued: uses}
			if err = s.audit(context.Background(), kind, *q, out); err != nil {
				return err
			}
			if err = s.write("grants", gid, out); err != nil {
				return err
			}
			q.State = "allowed"
			q.Decided = now
			return s.write("requests", q.ID, q)
		}
		return refusal(ErrUnknownID, "request %q not found", id)
	})
	return out, err
}

// Consume revalidates current authority inside the same transaction immediately
// before matching/spending. A missing gate fails closed for an otherwise live grant.
// Spending is persisted before execution: a crash cannot refund a use.
func (s *Store) Consume(jail, start, set, commandDigest string, revalidate Revalidate) (Grant, bool, error) {
	return s.ConsumeContext(context.Background(), jail, start, set, commandDigest, revalidate)
}
func (s *Store) ConsumeContext(ctx context.Context, jail, start, set, commandDigest string, revalidate Revalidate) (Grant, bool, error) {
	return s.ConsumeDispatchContext(ctx, jail, start, set, commandDigest, revalidate, nil)
}

// ConsumeDispatchContext durably spends and audits before invoking dispatch under
// the same source lock. Dispatch must honor ctx and never nest a store/scope lock.
// Scope removal/revocation that wins this lock prevents dispatch; one that follows
// waits for bounded dispatch. A crash or dispatch failure never refunds the use.
func (s *Store) ConsumeDispatchContext(ctx context.Context, jail, start, set, commandDigest string, revalidate Revalidate, dispatch func() error) (Grant, bool, error) {
	var out Grant
	found := false
	err := s.transactionContext(ctx, func(r *records, now time.Time) error {
		for i := range r.grants {
			g := &r.grants[i]
			if g.Jail != jail || g.Start != start || g.Set != set || (!g.Ended.IsZero()) || g.Uses <= 0 || !now.Before(g.Expires) || (g.Digest != "" && g.Digest != commandDigest) {
				continue
			}
			allowed := false
			var parent Request
			for _, q := range r.requests {
				if q.ID == g.Request && q.State == "allowed" && q.Jail == jail && q.Start == start && q.Set == set {
					current, err := currentPolicy(q, revalidate)
					if err != nil {
						return err
					}
					// Narrowing invalidates a grant rather than silently rewriting
					// its coverage. Refile under the new policy before approving.
					if current.MaxWindow < q.Policy.MaxWindow || current.Window < q.Policy.Window || current.Uses < q.Policy.Uses || (q.Policy.Session && !current.Session) {
						return errors.New("current policy narrowed; refile the command under the host's current policy")
					}
					allowed = true
					parent = q
					break
				}
			}
			if !allowed {
				continue
			}
			g.Uses--
			if g.Uses == 0 {
				g.Ended = now
				g.EndReason = EndUsed
			}
			if err := s.write("grants", g.ID, g); err != nil {
				return err
			}
			if err := s.audit(ctx, "grant-used", parent, *g); err != nil {
				return err
			}
			out = *g
			found = true
			if err := ctx.Err(); err != nil {
				return err
			}
			if dispatch != nil {
				return dispatch()
			}
			return nil
		}
		return nil
	})
	return out, found, err
}

// Revoke ends one grant or working-set entry by its id, or with "all" every live grant
// (RevokeIn says what `all` leaves).
func (s *Store) Revoke(id string) error { return s.RevokeIn(id, "") }

// NoteHookFailure records that the user's on_request failed for request id, with a one-line
// failure and the tail of its output, so every listing of the request shows it. It decides
// nothing and is not audited: the hook's output never moves a request's state.
func (s *Store) NoteHookFailure(id, failure, output string) error {
	return s.transaction(func(r *records, _ time.Time) error {
		for i := range r.requests {
			q := &r.requests[i]
			if q.ID != id {
				continue
			}
			q.HookFailure, q.HookOutput = failure, output
			return s.write("requests", q.ID, q)
		}
		return refusal(ErrUnknownID, "request %q not found", id)
	})
}

// End closes this broker lifetime, returning exactly the grants it durably ended
// under this transaction, including partial progress on error. Audit those grants
// even on error and report/retry incomplete teardown. Pending requests also end.
func (s *Store) End(jail, start string) ([]Grant, error) {
	var ended []Grant
	err := s.transaction(func(r *records, now time.Time) error {
		for i := range r.grants {
			g := &r.grants[i]
			if g.Jail == jail && g.Start == start && g.Ended.IsZero() {
				g.Ended = now
				g.EndReason = EndStopped
				if err := s.write("grants", g.ID, g); err != nil {
					return err
				}
				ended = append(ended, *g)
			}
		}
		for i := range r.requests {
			q := &r.requests[i]
			if q.Jail == jail && q.Start == start && q.State == "pending" {
				q.State = "refused"
				q.Decided = now
				if err := s.write("requests", q.ID, q); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return ended, err
}
