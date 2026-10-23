package brokerhook

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
)

// alive reports whether pid still runs: a zombie, which a container's init may never reap,
// counts as gone.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // no /proc to ask (macOS); the signal answered
	}
	if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) {
		return stat[i+2] != 'Z'
	}
	return true
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, "the hook's pid file", func() bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil && pid > 0
	})
	return pid
}

// A hook still waiting when its request leaves pending some other way is killed with its
// whole process group, child included: decided with `yolo approve`, or refused because its
// jail stopped.
func TestAHookIsKilledWhenItsRequestLeavesPending(t *testing.T) {
	for _, c := range []struct {
		name  string
		leave func(f *fixture, q brokerstore.Request) error
	}{
		{"decided", func(f *fixture, q brokerstore.Request) error {
			_, err := f.store.Decide(q.ID, "pending", brokerstore.Decision{Deny: true})
			return err
		}},
		{"jail stopped", func(f *fixture, q brokerstore.Request) error {
			_, err := f.store.End(q.Jail, q.Start)
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			var log syncBuffer
			child := filepath.Join(f.dir, "child")
			stop := f.run(t, f.script(t, "hook", "cat >/dev/null\nsleep 300 &\necho $! >'"+child+"'\nwait\necho approve\n"), &log)
			defer stop()
			q, err := f.store.File(f.filing)
			if err != nil {
				t.Fatal(err)
			}
			pid := readPid(t, child)
			if err := c.leave(f, q); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the hook to be stopped", func() bool {
				return strings.Contains(log.String(), "stopped because its request is no longer pending")
			})
			waitFor(t, "the hook's child to die", func() bool { return !alive(pid) })
			if got := f.state(t, q.ID); got == "allowed" || got == "pending" {
				t.Fatalf("state %s after the hook was stopped", got)
			}
		})
	}
}

// The hook is a request hook: a decision, a grant's use and its end fire nothing.
func TestOnlyAFiledRequestRunsTheHook(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(f.dir, "runs")
	stop := f.run(t, f.script(t, "hook", "cat >/dev/null\necho \"$YOLO_EVENT\" >>'"+out+"'\n"), &syncBuffer{})
	q, err := f.store.File(f.filing)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hook to run", func() bool { b, _ := os.ReadFile(out); return len(b) > 0 })
	if _, err := f.store.Decide(q.ID, "pending", brokerstore.Decision{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.store.Consume(q.Jail, q.Start, q.Set, q.Digest, f.store.Revalidate); err != nil || !ok {
		t.Fatal(ok, err)
	}
	time.Sleep(100 * time.Millisecond) // twenty ticks
	stop()
	if b, _ := os.ReadFile(out); string(b) != "request-filed\n" {
		t.Fatalf("the hook ran for %q, want only the filing", b)
	}
}

// A store that cannot be read when the broker starts is tried again rather than switching
// the hook off, its failure is logged once rather than once per tick, and a request filed
// meanwhile still runs the hook.
func TestAnUnreadableStoreIsRetriedAndLoggedOnce(t *testing.T) {
	f := newFixture(t)
	blocker := filepath.Join(f.dir, "blocked")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	good := f.store
	bad := *good
	bad.Dir = filepath.Join(blocker, "github") // under a regular file: no read can succeed
	f.store = &bad
	marker := filepath.Join(f.dir, "ran")
	var log syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Store: f.store, Start: "start", Command: f.script(t, "hook", "cat >/dev/null\ntouch '"+marker+"'\n"),
		Interval: 5 * time.Millisecond, Log: &log}
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor(t, "the failure to be logged", func() bool { return strings.Contains(log.String(), "reading the store failed") })
	time.Sleep(50 * time.Millisecond) // ten more failed reads
	if n := strings.Count(log.String(), "reading the store failed"); n != 1 {
		t.Fatalf("logged %d times, want once:\n%s", n, log.String())
	}
	// The store becomes readable, with a request filed while it was not.
	if _, err := good.File(f.filing); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(good.Dir), blocker); err != nil { // bad.Dir now names good.Dir
		t.Fatal(err)
	}
	waitFor(t, "the hook to run once the store reads", func() bool { _, err := os.Stat(marker); return err == nil })
}

// The hook only notifies: nothing it prints, an old `approve` answer included, decides its
// request, and the store records no decision.
func TestTheHookDecidesNothingByItself(t *testing.T) {
	for _, body := range []string{"echo approve\n", "echo deny\n", "echo 'approve please'\n", "echo approve\nexit 3\n"} {
		f := newFixture(t)
		marker := filepath.Join(f.dir, "ran")
		stop := f.run(t, f.script(t, "hook", "cat >/dev/null\ntouch '"+marker+"'\n"+body), &syncBuffer{})
		q, err := f.store.File(f.filing)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the hook to run", func() bool { _, err := os.Stat(marker); return err == nil })
		time.Sleep(50 * time.Millisecond) // ten ticks
		stop()
		if got := f.state(t, q.ID); got != "pending" {
			t.Fatalf("%q left the request %s", body, got)
		}
		f.mu.Lock()
		kinds := strings.Join(f.kinds, ",")
		f.mu.Unlock()
		if strings.Contains(kinds, "decision") {
			t.Fatalf("%q was audited as a decision: %s", body, kinds)
		}
	}
}

// YOLO_BIN names the yolo to call, so `"$YOLO_BIN" approve` resolves even when the hook's
// PATH has no yolo.
func TestTheHookGetsYoloBin(t *testing.T) {
	f := newFixture(t)
	binDir := filepath.Join(f.dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(f.dir, "calls")
	bin := filepath.Join(binDir, "yolo")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$*\" >>'"+calls+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	hook := f.script(t, "hook", "cat >/dev/null\n[ \"$YOLO_BIN\" = '"+bin+"' ] || exit 9\n\"$YOLO_BIN\" approve \"$YOLO_REQUEST_ID\"\n")
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Store: f.store, Start: "start", Command: hook, Bin: bin, Interval: 5 * time.Millisecond}
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(50 * time.Millisecond)
	q, err := f.store.File(f.filing)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hook to call yolo", func() bool { b, _ := os.ReadFile(calls); return len(b) > 0 })
	if b, _ := os.ReadFile(calls); string(b) != "approve "+q.ID+"\n" {
		t.Fatalf("yolo was called with %q", b)
	}
}

// The hook starts in the given directory, not wherever the broker runs.
func TestTheHookRunsInItsDirectory(t *testing.T) {
	f := newFixture(t)
	home := t.TempDir()
	out := filepath.Join(f.dir, "cwd")
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Store: f.store, Start: "start", Command: f.script(t, "hook", "cat >/dev/null\npwd >'"+out+"'\n"),
		Dir: home, Interval: 5 * time.Millisecond}
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(50 * time.Millisecond)
	if _, err := f.store.File(f.filing); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hook to run", func() bool { b, _ := os.ReadFile(out); return len(b) > 0 })
	b, _ := os.ReadFile(out)
	want, _ := filepath.EvalSymlinks(home)
	if got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(b))); got != want {
		t.Fatalf("the hook ran in %s, want %s", got, want)
	}
}
