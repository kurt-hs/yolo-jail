// Package reqlog is the one-line-per-request log a small yolo HTTP daemon writes: the in-jail
// credential adapters (aws-credential-adapter, openai-auth-adapter), whose logs used to hold a
// 401 with no time and nothing at all for a request that succeeded or hung
// (docs/reference/agent-credentials.md, "The adapters' logs").
//
// Each request gets ONE line when it ends, stamped by internal/logstamp:
//
//	2026-10-11T12:17:03.412-04:00 aws-credential-adapter: request #12 GET /credentials 200 peer=127.0.0.1:40122 auth=ok upstream=aws-auth upstream_outcome=ok bytes_out=1280 elapsed_ms=4
//
// and, while it is still in flight, a "still in flight" line every StillWaitingInterval naming
// what it waits on, so a hang shows in the log while it happens rather than never.
//
// The fields after the status are the handler's (Entry.Set), in the order it set them; the
// package adds peer, bytes_out and elapsed_ms. NEVER A VALUE A CALLER SENT: a handler sets
// outcomes and names, never a header, a token or a body, and the adapters' tests drive every
// refusal with the real token and a wrong one and fail if either reaches the log.
package reqlog

import (
	"context"
	"fmt"
	"io"
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
		go func() {
			t := time.NewTicker(StillWaitingInterval)
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
		defer func() {
			close(done)
			l.Printf("%s", e.finalLine())
		}()
		next.ServeHTTP(&countingWriter{ResponseWriter: w, e: e}, r.WithContext(context.WithValue(r.Context(), entryKey{}, e)))
	})
}

func (e *Entry) head() string {
	return fmt.Sprintf("request #%d %s %s", e.id, e.method, quote(e.path))
}

func (e *Entry) fields() string {
	var b strings.Builder
	fmt.Fprintf(&b, " peer=%s", quote(e.peer))
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
	w.e.mu.Lock()
	if !w.e.wrote {
		w.e.status, w.e.wrote = code, true
	}
	w.e.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}

func (w *countingWriter) Write(p []byte) (int, error) {
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
