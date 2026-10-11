// Package reqlog is the one-line-per-request log a small yolo HTTP daemon writes: the in-jail
// credential adapters (aws-credential-adapter, openai-auth-adapter), whose logs used to hold a
// 401 with no time and nothing at all for a request that succeeded or hung
// (docs/reference/agent-credentials.md, "The adapters' logs").
//
// Each request gets ONE line when it ends, stamped by internal/logstamp:
//
//	2026-10-11T12:17:03.412-04:00 aws-credential-adapter: request #12 GET /credentials 200 peer=127.0.0.1:40122 caller=claude caller_pid=136 auth=ok upstream=aws-auth upstream_outcome=ok bytes_out=1280 elapsed_ms=4
//
// and, while it is still in flight, a "still in flight" line every StillWaitingInterval naming
// what it waits on, so a hang shows in the log while it happens rather than never.
//
// The fields after the status are the handler's (Entry.Set), in the order it set them; the
// package adds peer, the caller's fields right after it when the Logger resolves callers
// (ResolveCallers), bytes_out and elapsed_ms. NEVER A VALUE A CALLER SENT: a handler sets
// outcomes and names, never a header, a token or a body, and the adapters' tests drive every
// refusal with the real token and a wrong one and fail if either reaches the log.
package reqlog

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
)

// StillWaitingInterval is how long a request may be in flight before the log says so, and how
// often it says so again. A variable so a test can reach it; never rebound in production.
var StillWaitingInterval = 30 * time.Second

// Logger is one daemon's log: every line is "<stamp> <name>: …".
type Logger struct {
	name string
	mu   sync.Mutex
	out  io.Writer
	seq  atomic.Uint64

	// callers names each request's caller (ResolveCallers), nil when nothing does.
	callers atomic.Pointer[callerResolver]
}

// CallerFunc names the caller of a connection from local to peer (both host:port; local is
// "" when the server recorded none) as key/value pairs for the request line, returning by the
// time ctx is done.
type CallerFunc func(ctx context.Context, local, peer string) [][2]string

type callerResolver struct {
	resolve CallerFunc
	budget  time.Duration
}

// ResolveCallers makes every request line name its caller, right after peer=, through resolve
// (internal/peercaller in the credential adapters). The lookup starts with the request and runs
// beside the handler; the response's first byte waits for it, at most budget from the request's
// start, because the lookup needs the caller's socket still open, and a client that has its
// answer may close it at once (Go's HTTP client closed one given a 401 before a lookup that ran
// after the response could find it). A lookup still running at the budget is
// logged as caller=unknown with why, and the response goes out regardless.
func (l *Logger) ResolveCallers(budget time.Duration, resolve CallerFunc) {
	l.callers.Store(&callerResolver{resolve: resolve, budget: budget})
}

// New returns a Logger writing to out.
func New(name string, out io.Writer) *Logger { return &Logger{name: name, out: out} }

// SetOutput redirects the log (a test reads it).
func (l *Logger) SetOutput(out io.Writer) {
	l.mu.Lock()
	l.out = out
	l.mu.Unlock()
}

// Printf writes one stamped line.
func (l *Logger) Printf(format string, args ...any) {
	line := logstamp.Now() + " " + l.name + ": " + strings.TrimRight(fmt.Sprintf(format, args...), "\n") + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.out, line)
}

// Writer is an io.Writer onto the log for a collaborator that writes its own diagnostics
// (a client's stderr frames): each line it writes is stamped, and written as it is.
func (l *Logger) Writer() io.Writer { return logstamp.Writer(lockedWriter{l}) }

type lockedWriter struct{ l *Logger }

func (w lockedWriter) Write(p []byte) (int, error) {
	w.l.mu.Lock()
	defer w.l.mu.Unlock()
	return w.l.out.Write(p)
}

type entryKey struct{}

// Entry is one request's record.
type Entry struct {
	mu        sync.Mutex
	id        uint64
	start     time.Time
	method    string
	path      string
	peer      string
	keys      []string
	values    map[string]string
	waitingOn string
	status    int
	wrote     bool
	bytesOut  int64
	// caller is the caller's fields (ResolveCallers), nil until the lookup answers; callerSet
	// freezes it once the line has given up waiting.
	caller    [][2]string
	callerSet bool
	// callerDone is closed when the lookup has answered, nil when there is none; callerWait makes
	// the wait for it happen once.
	callerDone   <-chan struct{}
	callerBudget time.Duration
	callerWait   sync.Once
}

func (e *Entry) setCaller(fields [][2]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.callerSet {
		e.caller, e.callerSet = fields, true
	}
}

// From returns the request's Entry, or nil for a handler used outside Wrap. Every Entry method
// is nil-safe.
func From(ctx context.Context) *Entry {
	e, _ := ctx.Value(entryKey{}).(*Entry)
	return e
}

// Set records one field, keeping the order fields were first set in. A value with a space or
// a quote is quoted.
func (e *Entry) Set(key, value string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.values[key]; !ok {
		e.keys = append(e.keys, key)
	}
	e.values[key] = value
}

// Waiting says what the request is about to wait on; the still-in-flight line names it.
func (e *Entry) Waiting(what string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.waitingOn = what
	e.mu.Unlock()
}

// Wrap logs every request next serves.
func (l *Logger) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := &Entry{id: l.seq.Add(1), start: time.Now(), method: r.Method, path: r.URL.Path,
			peer: r.RemoteAddr, values: map[string]string{}}
		done := make(chan struct{})
		interval := StillWaitingInterval // read here, not in the goroutine, which may start late
		go func() {
			t := time.NewTicker(interval)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					l.Printf("%s", e.waitingLine())
				}
			}
		}()
		e.callerDone, e.callerBudget = l.startCallerLookup(e, r)
		defer func() {
			close(done)
			e.awaitCaller()
			l.Printf("%s", e.finalLine())
		}()
		next.ServeHTTP(&countingWriter{ResponseWriter: w, e: e}, r.WithContext(context.WithValue(r.Context(), entryKey{}, e)))
	})
}

// startCallerLookup starts the request's caller lookup, when the Logger resolves callers, and
// returns the channel closed when it has answered (nil when there is none) and its budget.
func (l *Logger) startCallerLookup(e *Entry, r *http.Request) (<-chan struct{}, time.Duration) {
	cr := l.callers.Load()
	if cr == nil {
		return nil, 0
	}
	local := ""
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && a != nil {
		local = a.String()
	}
	peer := r.RemoteAddr
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), cr.budget)
		defer cancel()
		e.setCaller(cr.resolve(ctx, local, peer))
	}()
	return done, cr.budget
}

// awaitCaller waits for the caller lookup, once, until at most its budget from the request's
// start: before the response's first byte, and at the end for a handler that wrote nothing.
// Past the budget the line names the caller unknown and says why.
func (e *Entry) awaitCaller() {
	if e.callerDone == nil {
		return
	}
	e.callerWait.Do(func() {
		t := time.NewTimer(time.Until(e.start.Add(e.callerBudget)))
		defer t.Stop()
		select {
		case <-e.callerDone:
		case <-t.C:
			e.setCaller([][2]string{{"caller", "unknown"},
				{"caller_why", "the lookup ran past its " + e.callerBudget.String() + " budget"}})
		}
	})
}

func (e *Entry) head() string {
	return fmt.Sprintf("request #%d %s %s", e.id, e.method, quote(e.path))
}

func (e *Entry) fields() string {
	var b strings.Builder
	fmt.Fprintf(&b, " peer=%s", quote(e.peer))
	for _, kv := range e.caller {
		fmt.Fprintf(&b, " %s=%s", kv[0], quote(kv[1]))
	}
	for _, k := range e.keys {
		fmt.Fprintf(&b, " %s=%s", k, quote(e.values[k]))
	}
	return b.String()
}

func (e *Entry) waitingLine() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	what := e.waitingOn
	if what == "" {
		what = "the handler"
	}
	return fmt.Sprintf("%s still in flight after %s%s: still waiting on %s", e.head(),
		time.Since(e.start).Round(time.Second), e.fields(), what)
}

func (e *Entry) finalLine() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	status := e.status
	if !e.wrote {
		status = http.StatusOK
	}
	return fmt.Sprintf("%s %d%s bytes_out=%d elapsed_ms=%d", e.head(), status, e.fields(),
		e.bytesOut, time.Since(e.start).Milliseconds())
}

func quote(s string) string {
	if s == "" {
		return "-"
	}
	for _, r := range s {
		if r <= ' ' || r == '"' || r >= 0x7f {
			return strconv.Quote(s)
		}
	}
	return s
}

type countingWriter struct {
	http.ResponseWriter
	e *Entry
}

func (w *countingWriter) WriteHeader(code int) {
	w.e.awaitCaller()
	w.e.mu.Lock()
	if !w.e.wrote {
		w.e.status, w.e.wrote = code, true
	}
	w.e.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.e.awaitCaller()
	w.e.mu.Lock()
	w.e.wrote = true
	if w.e.status == 0 {
		w.e.status = http.StatusOK
	}
	w.e.mu.Unlock()
	n, err := w.ResponseWriter.Write(p)
	w.e.mu.Lock()
	w.e.bytesOut += int64(n)
	w.e.mu.Unlock()
	return n, err
}

// Unwrap lets http.ResponseController reach the server's writer.
func (w *countingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
