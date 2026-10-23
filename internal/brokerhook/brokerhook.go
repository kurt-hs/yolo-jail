// Package brokerhook runs the user's on_request (boundary-broker.md §6.6, BB-D75): a
// command named in the user config, run once for each request a jail files, with the
// request-filed event's JSON on stdin and its fields in the environment, so a one-line
// notifier can be the whole approval front end. It is a REQUEST hook: nothing else fires
// it. A tool that wants every event reads `yolo approve --watch --json`.
//
// WHO RUNS IT. Each host broker daemon runs one Runner for its own broker lifetime: it
// watches the store it already holds and fires only for requests carrying its own start
// id. So every request has exactly one runner however many jails are up, and the hook runs
// whenever a request can exist, since a request is only ever filed by a live broker.
//
// WHAT IT CAN DECIDE. A hook whose trimmed stdout is exactly `approve` or `deny` decides its
// request that way: `approve` is Allow once, exactly `yolo approve <id>` with no --for. The
// decision goes through the store, with the broker's own current authority, and is audited
// as a hook decision. Anything else decides nothing.
//
// WHEN IT IS STOPPED. A hook runs until its request leaves `pending` some other way (decided
// with `yolo approve`, expired, or refused because its jail stopped), until the request's
// expiry, or until its broker stops, and is then killed with its whole process group, so a
// notification waiting on a click does not outlive the question it asks.
package brokerhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

// Runner runs one broker lifetime's hook.
type Runner struct {
	Store   *brokerstore.Store
	Start   string // the broker lifetime whose requests fire the hook
	Command string // run as `sh -c Command`
	// Dir is the hook's working directory; empty means the user's home, never the
	// broker's own store directory, which is where the daemon itself runs.
	Dir string
	// Interval is how often the store is polled, and how long a failed first read waits
	// before it is tried again; zero means one second.
	Interval time.Duration
	// Timeout bounds one run; zero means brokerstore.PendingLife, so a hook that waits on a
	// notification can still be answered until its request expires.
	Timeout time.Duration
	// Log takes one line per hook that fails or decides. It names no workspace, repository or
	// command, because a broker's log is shared by every jail on the machine.
	Log io.Writer

	wg       sync.WaitGroup
	mu       sync.Mutex
	running  map[string]context.CancelCauseFunc // by request id
	lastWarn string
}

var (
	errLeftPending = errors.New("its request was decided, expired or refused elsewhere")
	errTimedOut    = errors.New("it ran until its request expired")
)

// Run fires the hook for each request this lifetime files until ctx ends, then waits for
// the hooks still running, which ctx's end also stops. A store it cannot read yet is tried
// again every interval, so a passing fault at the broker's start does not switch the hook
// off for the broker's lifetime.
func (r *Runner) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = time.Second
	}
	opened := func(snap brokerstore.Snapshot) { r.opened(ctx, snap) }
	event := func(ev brokerstore.Event) { r.event(ctx, ev) }
	for {
		err := r.Store.Watch(ctx, interval, opened, event, r.warn)
		if err == nil || ctx.Err() != nil {
			break
		}
		r.warn(err)
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
		if ctx.Err() != nil {
			break
		}
	}
	r.wg.Wait()
}

// opened fires for this lifetime's requests already pending when the watch opens: those
// filed while the store could not be read.
func (r *Runner) opened(ctx context.Context, snap brokerstore.Snapshot) {
	r.clearWarn()
	for _, q := range snap.Requests {
		if q.Start == r.Start && q.State == "pending" {
			r.launch(ctx, q)
		}
	}
}

func (r *Runner) event(ctx context.Context, ev brokerstore.Event) {
	r.clearWarn()
	q := ev.Request
	if q == nil || q.Start != r.Start {
		return
	}
	switch ev.Kind {
	case brokerstore.EventRequestFiled:
		r.launch(ctx, *q)
	case brokerstore.EventRequestDecided, brokerstore.EventRequestExpired:
		r.mu.Lock()
		stop := r.running[q.ID]
		r.mu.Unlock()
		if stop != nil {
			stop(errLeftPending)
		}
	}
}

// warn logs a failed read once, not once per tick, until a read succeeds again.
func (r *Runner) warn(err error) {
	line := fmt.Sprintf("reading the store failed (%v); retrying", err)
	r.mu.Lock()
	repeat := line == r.lastWarn
	r.lastWarn = line
	r.mu.Unlock()
	if !repeat {
		r.logf("%s", line)
	}
}

func (r *Runner) clearWarn() {
	r.mu.Lock()
	r.lastWarn = ""
	r.mu.Unlock()
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, "on_request: "+format+"\n", args...)
	}
}

// launch starts the hook for q, once per request; parent ending stops it.
func (r *Runner) launch(parent context.Context, q brokerstore.Request) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = brokerstore.PendingLife
	}
	ctx, cancel := context.WithCancelCause(parent)
	r.mu.Lock()
	if r.running == nil {
		r.running = map[string]context.CancelCauseFunc{}
	}
	if r.running[q.ID] != nil {
		r.mu.Unlock()
		cancel(nil)
		return
	}
	r.running[q.ID] = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.running, q.ID)
			r.mu.Unlock()
			cancel(nil)
		}()
		timer := time.AfterFunc(timeout, func() { cancel(errTimedOut) })
		defer timer.Stop()
		r.fire(ctx, q)
	}()
}

// Env is the request's fields as the hook's environment: what a notifier that takes its
// text as arguments, such as notify-send, needs. Every value is safe to show on a terminal.
func Env(s *brokerstore.Store, q brokerstore.Request) []string {
	env := []string{"YOLO_EVENT=" + brokerstore.EventRequestFiled, "YOLO_SOURCE=" + s.SourceName()}
	add := func(k, v string) { env = append(env, k+"="+termsafe.Visible(v)) }
	add("YOLO_REQUEST_ID", q.ID)
	add("YOLO_REQUEST_SUMMARY", s.RequestSummary(q))
	add("YOLO_REQUEST_EXPIRES", q.Expires().Format(time.RFC3339))
	add("YOLO_WORKSPACE", q.Workspace)
	add("YOLO_JAIL", q.Jail)
	add("YOLO_SET", q.Set)
	add("YOLO_REPO", q.Repo)
	return env
}

// fire runs the hook for one request and acts on its answer.
func (r *Runner) fire(ctx context.Context, q brokerstore.Request) {
	q.State, q.Decided = "pending", time.Time{}
	body, err := json.Marshal(r.Store.EventView(brokerstore.Event{Kind: brokerstore.EventRequestFiled, Request: &q}, time.Now()))
	if err != nil {
		r.logf("%s: %v", q.ID, err)
		return
	}
	cmd := exec.Command("/bin/sh", "-c", r.Command)
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	cmd.Env = append(os.Environ(), Env(r.Store, q)...)
	cmd.Dir = r.Dir
	if cmd.Dir == "" {
		cmd.Dir, _ = os.UserHomeDir()
	}
	var out limitedBuffer
	cmd.Stdout = &out
	// A grandchild that left the hook's process group can hold stdout open after the hook
	// exits or is killed; stop waiting on it after KillWait.
	cmd.WaitDelay = KillWait
	cmd.SysProcAttr = hookProcAttr()
	// On Linux the hook dies with the broker (hookProcAttr's Pdeathsig), which the kernel
	// ties to the THREAD that started it, so this goroutine keeps its thread until the hook
	// is reaped.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if ctx.Err() != nil {
		return // its request left pending before the hook could start
	}
	if err := cmd.Start(); err != nil {
		r.logf("the hook for %s could not start (%v); it decided nothing", q.ID, err)
		return
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err = <-waited:
	case <-ctx.Done():
		// Its own process group, so this ends whatever it started too.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-waited
		why := context.Cause(ctx)
		if errors.Is(why, context.Canceled) {
			why = errors.New("the broker stopped")
		}
		r.logf("the hook for %s was stopped because %v; it decided nothing", q.ID, why)
		return
	}
	if err != nil {
		r.logf("the hook for %s failed (%v); it decided nothing. Run the command by hand to see why", q.ID, err)
		return
	}
	var d brokerstore.Decision
	switch strings.TrimSpace(out.String()) {
	case "approve":
		d = brokerstore.Decision{Hook: true}
	case "deny":
		d = brokerstore.Decision{Deny: true, Hook: true}
	default:
		return
	}
	if _, err := r.Store.Decide(q.ID, "pending", d); err != nil {
		r.logf("could not decide %s: %s; decide it with `yolo approve`", q.ID, termsafe.Visible(err.Error()))
		return
	}
	verb := "approved"
	if d.Deny {
		verb = "denied"
	}
	r.logf("%s %s", verb, q.ID)
}

// KillWait is how long a stopped hook's output is waited on after its process group is
// killed. A broker stopping waits at least this long for its runner.
const KillWait = 5 * time.Second

// limitedBuffer keeps the first 4 KiB a hook prints: an answer is one word.
type limitedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := 4096 - b.b.Len(); room > 0 {
		b.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
