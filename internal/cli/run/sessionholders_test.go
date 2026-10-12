package run

// sessionholders_test.go pins the diagnostics a jail that stays up because sessions remain prints
// (docs/design/jail-lifetime-last-session-wins.md JL-P3): a count that could not be read is never a
// number, and never the phantom "1 other session" a failed environment read used to fabricate. The
// Linux-only /proc half is in sessionholders_linux_test.go.

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestAStaysUpLineSaysTheCountIsUnknownWhenNobodyHoldsTheLock: with no runtime answer and no holder
// of the session lock, the jail is still said to stay up, and the line says the count is unknown —
// never a fabricated number.
func TestAStaysUpLineSaysTheCountIsUnknownWhenNobodyHoldsTheLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	o := goldenOptions("/ws", t.TempDir())
	var errBuf bytes.Buffer
	o.Stderr = &errBuf
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		if strings.Contains(strings.Join(argv, " "), "ExecIDs") {
			return ExecResult{Ran: true, Stdout: "0\n"}
		}
		return ExecResult{Ran: true, RC: 125} // the environment read failed
	}
	o.noteJailStaysUp("yolo-no-holder", "podman")
	got := errBuf.String()
	if strings.Contains(got, "1 other session") {
		t.Errorf("a failed count fabricated a session:\n%s", got)
	}
	if !strings.Contains(got, couldNotDetermineSessions) {
		t.Errorf("the line must say the count could not be determined:\n%s", got)
	}
}
