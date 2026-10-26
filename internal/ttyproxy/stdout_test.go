//go:build linux

package ttyproxy

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestObserverStdoutTakesTheChildsStdoutOnBothPaths pins Observer.Stdout: with it set, the child's
// standard output goes to that writer and never to this process's stdout — on a terminal too, where
// the proxy would otherwise merge both of the child's streams into one pty and copy it to this
// process's stdout. Its stderr stays this process's unless Observer.Stderr is also named.
func TestObserverStdoutTakesTheChildsStdoutOnBothPaths(t *testing.T) {
	cmd := []string{"sh", "-c", `echo JAIL_STDOUT; echo JAIL_STDERR >&2`}
	check := func(t *testing.T, got *bytes.Buffer, rc int, err error) {
		t.Helper()
		if err != nil || rc != 0 {
			t.Fatalf("RunWithProxyObserved = %d, %v", rc, err)
		}
		if s := got.String(); s != "JAIL_STDOUT\n" {
			t.Errorf("Observer.Stdout received %q, want the child's stdout alone (JAIL_STDOUT)", s)
		}
	}

	t.Run("terminal", func(t *testing.T) {
		master, _ := fakeHostTTY(t)
		var got bytes.Buffer
		errR, errW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		origErr := os.Stderr
		os.Stderr = errW
		t.Cleanup(func() { os.Stderr = origErr })
		done := make(chan struct{})
		var rc int
		var runErr error
		go func() {
			defer close(done)
			rc, runErr = RunWithProxyObserved(cmd, nil, nil, Observer{Stdout: &got})
		}()
		select {
		case <-done:
		case <-time.After(childDeadline):
			t.Fatal("the child never exited")
		}
		os.Stderr = origErr
		_ = errW.Close()
		stderr := new(bytes.Buffer)
		_, _ = stderr.ReadFrom(errR)
		check(t, &got, rc, runErr)
		if !strings.Contains(stderr.String(), "JAIL_STDERR") {
			t.Errorf("the child's stderr did not reach this process's stderr: %q", stderr.String())
		}
		// Nothing the child printed came back out of the terminal: the proxy never ran.
		if err := unix.SetNonblock(master, true); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 4096)
		if n, _ := unix.Read(master, buf); n > 0 {
			t.Errorf("the terminal received %q; with Observer.Stdout set the child runs off the proxy", buf[:n])
		}
	})

	t.Run("plain", func(t *testing.T) {
		devnull, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		origIn := os.Stdin
		os.Stdin = devnull
		t.Cleanup(func() { os.Stdin = origIn; _ = devnull.Close() })
		var got bytes.Buffer
		rc, err := RunWithProxyObserved(cmd, nil, nil, Observer{Stdout: &got})
		check(t, &got, rc, err)
	})
}

// TestObserverStderrTakesTheChildsStderr pins the named stderr path without sending either stream
// to this process's stderr. Capture jails use it for installer output the entrypoint relays to stderr.
func TestObserverStderrTakesTheChildsStderr(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	origIn := os.Stdin
	os.Stdin = devnull
	t.Cleanup(func() { os.Stdin = origIn; _ = devnull.Close() })

	var stdout, stderr bytes.Buffer
	cmd := []string{"sh", "-c", `printf JAIL_STDOUT; printf JAIL_STDERR >&2`}
	rc, err := RunWithProxyObserved(cmd, nil, nil, Observer{Stdout: &stdout, Stderr: &stderr})
	if err != nil || rc != 0 {
		t.Fatalf("RunWithProxyObserved = %d, %v", rc, err)
	}
	if got := stdout.String(); got != "JAIL_STDOUT" {
		t.Errorf("Observer.Stdout received %q, want JAIL_STDOUT", got)
	}
	if got := stderr.String(); got != "JAIL_STDERR" {
		t.Errorf("Observer.Stderr received %q, want JAIL_STDERR", got)
	}
}
