package brokerhook

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
)

// fixture is a store whose decisions are recorded as the audit sees them, and a request to
// file under broker lifetime "start".
type fixture struct {
	store  *brokerstore.Store
	mu     sync.Mutex
	kinds  []string
	dir    string
	filing brokerstore.Request
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dir: t.TempDir()}
	f.store = &brokerstore.Store{Dir: filepath.Join(f.dir, "github"), Source: "github",
		Revalidate: func(q brokerstore.Request) (brokerstore.Policy, error) { return q.Policy, nil },
		Audit: func(_ context.Context, kind string, _ brokerstore.Request, _ brokerstore.Grant) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.kinds = append(f.kinds, kind)
			return nil
		}}
	f.filing = brokerstore.Request{Jail: "jail", Start: "start", Workspace: "/project", Argv: []string{"pr", "comment", "1", "--body", "hi"},
		Repo: "o/r", StdinDigest: "digest", Set: "read-write",
		Policy: brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 25}}
	return f
}

// script writes an executable hook; its body runs under /bin/sh.
func (f *fixture) script(t *testing.T, name, body string) string {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("the hook runs under /bin/sh")
	}
	p := filepath.Join(f.dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// run starts a runner for lifetime "start" and returns its stop.
func (f *fixture) run(t *testing.T, command string, log *syncBuffer) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Store: f.store, Start: "start", Command: command, Interval: 5 * time.Millisecond, Log: log}
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	// Run reads the store once before it watches; a request filed after that is an event.
	time.Sleep(50 * time.Millisecond)
	return func() {
		cancel()
		<-done
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *syncBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *fixture) state(t *testing.T, id string) string {
	t.Helper()
	rs, _, err := f.store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range rs {
		if q.ID == id {
			return q.State
		}
	}
	return ""
}

// A hook that prints `approve` allows the request once, and the audit records a hook decision.
func TestAHookThatPrintsApproveAllowsOnce(t *testing.T) {
	f := newFixture(t)
	var log syncBuffer
	stop := f.run(t, f.script(t, "hook", "cat >/dev/null\necho approve\n"), &log)
	defer stop()
	q, err := f.store.File(f.filing)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hook's approval", func() bool { return f.state(t, q.ID) == "allowed" })
	_, gs, _ := f.store.List()
	if len(gs) != 1 || gs[0].Uses != 1 || gs[0].Digest != q.Digest {
		t.Fatalf("approve is not Allow once: %+v", gs)
	}
	f.mu.Lock()
	kinds := strings.Join(f.kinds, ",")
	f.mu.Unlock()
	if !strings.Contains(kinds, "hook-decision") {
		t.Fatalf("audited %s, want a hook-decision", kinds)
	}
	waitFor(t, "the log line", func() bool { return strings.Contains(log.String(), "approved "+q.ID) })
}

// `deny` denies; any other answer, or a failing hook, decides nothing.
func TestOnlyAnExactAnswerDecides(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"deny", "echo '  deny  '\n", "denied"},
		{"garbage", "echo 'approve please'\n", "pending"},
		{"failing", "echo approve\nexit 3\n", "pending"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			marker := filepath.Join(f.dir, "ran")
			stop := f.run(t, f.script(t, "hook", "cat >/dev/null\ntouch '"+marker+"'\n"+c.body), &syncBuffer{})
			q, err := f.store.File(f.filing)
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the hook to run", func() bool { _, err := os.Stat(marker); return err == nil })
			if c.want == "denied" {
				waitFor(t, "the denial", func() bool { return f.state(t, q.ID) == "denied" })
			}
			stop() // waits for every hook to finish
			if got := f.state(t, q.ID); got != c.want {
				t.Fatalf("state %s, want %s", got, c.want)
			}
		})
	}
}

// The hook gets the event's JSON on stdin and its fields in the environment, and fires only
// for its own broker lifetime's records.
func TestTheHookGetsTheEventAndOnlyItsOwnLifetime(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(f.dir, "events")
	stop := f.run(t, f.script(t, "hook", `{ cat; printf '%s|%s|%s|%s|%s\n' "$YOLO_EVENT" "$YOLO_REQUEST_ID" "$YOLO_SOURCE" "$YOLO_WORKSPACE" "$YOLO_REQUEST_SUMMARY"; } >>'`+out+`'`+"\n"), &syncBuffer{})
	other := f.filing
	other.Start, other.Argv = "another-lifetime", []string{"pr", "comment", "2"}
	if _, err := f.store.File(other); err != nil {
		t.Fatal(err)
	}
	q, err := f.store.File(f.filing)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hook's output", func() bool { b, _ := os.ReadFile(out); return bytes.Count(b, []byte("\n")) >= 2 })
	stop()
	b, _ := os.ReadFile(out)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the hook ran %d times, want once for its own lifetime:\n%s", len(lines)/2, b)
	}
	var ev struct {
		Schema  int    `json:"schema"`
		Event   string `json:"event"`
		Request struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatal(err, lines[0])
	}
	if ev.Schema != 1 || ev.Event != brokerstore.EventRequestFiled || ev.Request.ID != q.ID || ev.Request.State != "pending" {
		t.Fatalf("stdin %+v", ev)
	}
	fields := strings.Split(lines[1], "|")
	if fields[0] != "request-filed" || fields[1] != q.ID || fields[2] != "github" || fields[3] != "/project" ||
		fields[4] != "github "+q.ID+": set read-write on o/r for jail jail, to run: pr comment 1 --body hi" {
		t.Fatalf("env %q", lines[1])
	}
}

// A hook that hangs is killed at its timeout, and decides nothing.
func TestAHungHookIsStoppedAtItsTimeout(t *testing.T) {
	f := newFixture(t)
	var log syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &Runner{Store: f.store, Start: "start", Command: f.script(t, "hook", "sleep 30\necho approve\n"),
		Interval: 5 * time.Millisecond, Timeout: 100 * time.Millisecond, Log: &log}
	go r.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	q, _ := f.store.File(f.filing)
	waitFor(t, "the timeout", func() bool { return strings.Contains(log.String(), "stopped") })
	if got := f.state(t, q.ID); got != "pending" {
		t.Fatal(got)
	}
}
