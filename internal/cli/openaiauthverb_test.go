package cli

// openaiauthverb_test.go pins the PROMOTION of the OpenAI credential verb out of
// the hidden `internal` namespace: the public address, and the retained alias.
//
// The four registry-walking tests in this package (subhelp_test.go, help_test.go)
// already cover everything that is true of EVERY command — that `openai-auth` is
// registered, listed in `yolo --help`, answers `--help` to stdout with exit 0 and
// no side effect, and documents the flags it parses. What they cannot see is the
// thing that is specific to this verb: that TWO spellings reach ONE handler.
// Nothing about the registry row implies the hidden arm still resolves, and
// nothing about the hidden arm implies the public one exists.
//
// Both tests below are written to fail when their CALL SITE is deleted, not when
// some callee changes: revert the registry row and the first fails, revert the
// `case "openai-auth"` arm in runInternal and the second does.
//
// ONE SUBCOMMAND IS EXERCISED, and only since HD-R1 (docs/design/host-daemon-ownership.md):
// `status` starts the verb's OWN daemon (openaiauthhost's startPrivate), under the test's
// temp HOME and at a socket no other launch uses, and ends it with the verb. It used to
// ensure the machine-wide daemon, which no unit test may start. The two mutations stay
// unexercised here: they act on a credential, and openaiauthhost's operator tests drive them
// against a daemon of their own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/openaiauth"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// THE VERB'S OWN DAEMON, START TO END (HD-R1): `yolo openai-auth status` on a machine with no
// login starts a daemon of its own, which answers, and is gone with its sockets once the verb
// returns; nothing appears at the machine-wide rendezvous an older yolo's daemon used. Hand the
// verb a daemon that is never stopped and its private socket outlives it.
func TestOpenAIAuthStatusStartsAndEndsItsOwnDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	rc, stdout, stderr := captureDispatchRC(t, []string{"openai-auth", "status"})
	if rc != 0 || !strings.Contains(stdout, "logged_in") {
		t.Fatalf("`yolo openai-auth status` rc = %d, want 0 and a status\nstdout: %s\nstderr: %s",
			rc, stdout, stderr)
	}
	pattern := filepath.Join(paths.HostSingletonDir, "yolo-openai-host-*")
	var left []string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		left, _ = filepath.Glob(pattern)
		if len(left) == 0 || time.Now().After(deadline) {
			break
		}
	}
	if len(left) > 0 {
		t.Errorf("the verb's own daemon left %v after the verb returned: it was not ended with it", left)
	}
	if _, err := os.Lstat(paths.HostSingletonSocket(openaiauth.LoopholeName)); err == nil {
		t.Errorf("the verb created the machine-wide %s, which only an older yolo's daemon uses",
			paths.HostSingletonSocket(openaiauth.LoopholeName))
	}
}

// TestOpenAIAuthIsAPublicVerb pins the registry row itself, through the front door
// every other dispatch test in this package uses. Without the row, dispatchNative
// falls through to "unimplemented command" and returns 1.
func TestOpenAIAuthIsAPublicVerb(t *testing.T) {
	rc, stdout, stderr := captureDispatchRC(t, []string{"openai-auth"})
	if rc != 2 {
		t.Fatalf("`yolo openai-auth` rc = %d, want the verb's own usage refusal 2\n"+
			"stdout: %s\nstderr: %s", rc, stdout, stderr)
	}
	// The public SPELLING, not merely some usage: the operator's own text still
	// heads itself `usage: yolo internal openai-auth`, so a bare invocation answering
	// with THAT would send a user who just found this command in `yolo --help` back
	// into the hidden namespace. Both halves are asserted — the right header present
	// and the wrong one absent — because either one alone passes for the wrong reason.
	// (stderr also carries the startup banner dispatchNative writes first.)
	if !strings.Contains(stderr, "Usage: yolo openai-auth <status|import|logout>") {
		t.Errorf("`yolo openai-auth` does not answer in its own spelling:\n%s", stderr)
	}
	if strings.Contains(stderr, "usage: yolo internal openai-auth") {
		t.Errorf("`yolo openai-auth` answers in the HIDDEN spelling:\n%s", stderr)
	}
}

// TestOpenAIAuthHiddenSpellingIsTheSameVerb pins the retained alias: `yolo internal
// openai-auth` must reach the SAME handler — not a copy of it — and must say where
// the verb moved to.
//
// `--help` is the probe because it is the one argv that proves the handler was
// entered while touching nothing: answerHelp prints the registered usage and
// returns before any socket is resolved.
func TestOpenAIAuthHiddenSpellingIsTheSameVerb(t *testing.T) {
	stdout, stderr := captureBoth(t, func() {
		if rc := runInternal([]string{"openai-auth", "--help"}); rc != 0 {
			t.Errorf("`yolo internal openai-auth --help` rc = %d, want 0", rc)
		}
	})
	if got, want := strings.TrimSpace(stdout), strings.TrimSpace(openaiAuthUsage); got != want {
		t.Errorf("the hidden spelling prints something other than the public verb's "+
			"registered usage — it is a copy, not an alias\n--- got ---\n%s", got)
	}
	if !strings.Contains(stderr, "yolo openai-auth") {
		t.Errorf("the hidden spelling does not name the public one:\n%s", stderr)
	}
}
