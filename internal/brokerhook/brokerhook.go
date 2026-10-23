// Package brokerhook runs the user's approval_hook (boundary-broker.md §6.6, BB-D75): a
// command named in the user config, run once per approval event with the event's JSON on
// stdin and its fields in the environment, so a one-line notifier can be the whole approval
// front end.
//
// WHO RUNS IT. Each host broker daemon runs one Runner for its own broker lifetime: it
// watches the store it already holds and fires only for records carrying its own start id.
// So every record has exactly one runner however many jails are up, the hook fires with no
// watch kept open by hand, and it runs whenever a request can exist, since a request is only
// ever filed by a live broker.
//
// WHAT IT CAN DECIDE. For a request-filed event, a hook whose trimmed stdout is exactly
// `approve` or `deny` decides the request that way: `approve` is Allow once, exactly
// `yolo approve <id>` with no --for. The decision goes through the store, with the broker's
// own current authority, and is audited as a hook decision. Anything else decides nothing.
package brokerhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	Start   string // the broker lifetime whose records fire the hook
	Command string // run as `sh -c Command`
	// Interval is how often the store is polled; zero means one second.
	Interval time.Duration
	// Timeout bounds one run; zero means brokerstore.PendingLife, so a hook that waits on a
	// notification can still be answered until its request expires.
	Timeout time.Duration
	// Log takes one line per hook that fails or decides. It names no workspace, repository or
	// command, because a broker's log is shared by every jail on the machine.
	Log io.Writer

	wg sync.WaitGroup
}

// Run fires the hook for each of this lifetime's events until ctx ends, then waits for the
// hooks still running, which ctx's end also stops.
func (r *Runner) Run(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = time.Second
	}
	err := r.Store.Watch(ctx, interval, nil, func(ev brokerstore.Event) {
		if !r.ours(ev) {
			return
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.fire(ctx, ev)
		}()
	}, func(err error) { r.logf("reading the store failed (%v); retrying", err) })
	r.wg.Wait()
	return err
}

func (r *Runner) ours(ev brokerstore.Event) bool {
	if ev.Grant != nil {
		return ev.Grant.Start == r.Start
	}
	return ev.Request != nil && ev.Request.Start == r.Start
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, "approval_hook: "+format+"\n", args...)
	}
}

// Env is the event's fields as the hook's environment: what a notifier that takes its text
// as arguments, such as notify-send, needs. Every value is safe to show on a terminal.
func Env(s *brokerstore.Store, ev brokerstore.Event) []string {
	env := []string{"YOLO_EVENT=" + ev.Kind, "YOLO_SOURCE=" + s.SourceName(), "YOLO_EVENT_SUMMARY=" + s.Summary(ev)}
	add := func(k, v string) { env = append(env, k+"="+termsafe.Visible(v)) }
	if q := ev.Request; q != nil {
		add("YOLO_REQUEST_ID", q.ID)
		add("YOLO_REQUEST_STATE", q.State)
		add("YOLO_REQUEST_SUMMARY", s.RequestSummary(*q))
		add("YOLO_REQUEST_EXPIRES", q.Expires().Format(time.RFC3339))
		add("YOLO_WORKSPACE", q.Workspace)
		add("YOLO_JAIL", q.Jail)
		add("YOLO_SET", q.Set)
		add("YOLO_REPO", q.Repo)
	}
	if g := ev.Grant; g != nil {
		add("YOLO_GRANT_ID", g.ID)
		add("YOLO_GRANT_USES_LEFT", fmt.Sprint(g.Uses))
		add("YOLO_GRANT_EXPIRES", g.Expires.Format(time.RFC3339))
		if ev.Request == nil {
			add("YOLO_JAIL", g.Jail)
			add("YOLO_SET", g.Set)
		}
	}
	return env
}

// fire runs the hook for one event and acts on its answer.
func (r *Runner) fire(ctx context.Context, ev brokerstore.Event) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = brokerstore.PendingLife
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(r.Store.EventView(ev, time.Now()))
	if err != nil {
		r.logf("%s: %v", ev.Kind, err)
		return
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", r.Command)
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	cmd.Env = append(os.Environ(), Env(r.Store, ev)...)
	var out limitedBuffer
	cmd.Stdout = &out
	// Its own process group, so a timeout or the broker stopping ends whatever it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			r.logf("%s hook stopped (timed out or the broker stopped); it decided nothing", ev.Kind)
		} else {
			r.logf("%s hook failed (%v); it decided nothing. Run the command by hand to see why", ev.Kind, err)
		}
		return
	}
	if ev.Kind != brokerstore.EventRequestFiled || ev.Request == nil {
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
	if _, err := r.Store.Decide(ev.Request.ID, "pending", d); err != nil {
		r.logf("could not decide %s: %s; decide it with `yolo approve`", ev.Request.ID, termsafe.Visible(err.Error()))
		return
	}
	verb := "approved"
	if d.Deny {
		verb = "denied"
	}
	r.logf("%s %s", verb, ev.Request.ID)
}

// limitedBuffer keeps the first 4 KiB a hook prints: an answer is one word.
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := 4096 - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
