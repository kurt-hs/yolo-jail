package supervisor

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
)

// runOnce supervises one restart-"no" daemon to its exit and returns its log.
func runOnce(t *testing.T, name, script string) string {
	t.Helper()
	c := &child{spec: Spec{Name: name, Cmd: []string{"sh", "-c", script}, Restart: "no"},
		backoff: RestartBackoffInitial}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { c.superviseOne(stop); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		close(stop)
		t.Fatal("the daemon did not finish")
	}
	return readDaemonLog(t, name)
}

// A daemon's bare print — the shape the wire bridge and the credential adapters used to log
// in — reaches its log stamped, a line it stamped itself is not stamped twice, an unterminated
// last line survives, and the supervisor's own exit line follows the child's output.
func TestEveryDaemonLineIsStampedOnceAndInOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	self := logstamp.Now() + " self-stamped"
	body := runOnce(t, "d", "echo 'bare line'; echo '"+self+"' >&2; printf 'no newline'; exit 3")
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d: %q", len(lines), body)
	}
	for i, want := range []string{" bare line", " self-stamped", " no newline", " [yolo-jaild] exited with status 3"} {
		if !logstamp.HasStamp([]byte(lines[i])) || !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want a stamped line containing %q", i, lines[i], want)
		}
	}
	if lines[1] != self {
		t.Errorf("a self-stamped line was rewritten: %q, want %q", lines[1], self)
	}
}

// The log rotates while the daemon runs, not only at its next spawn: a request-logging daemon
// lives as long as the jail.
func TestTheDaemonLogRotatesWhileTheDaemonRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old := logMaxBytes
	logMaxBytes = 4096
	t.Cleanup(func() { logMaxBytes = old })
	body := runOnce(t, "r", `i=0; while [ $i -lt 400 ]; do echo "line $i padding padding"; i=$((i+1)); done`)
	archived, err := os.ReadFile(logPath("r") + ".1")
	if err != nil {
		t.Fatalf("no archived generation after the cap was crossed mid-run: %v", err)
	}
	if int64(len(body)) > logMaxBytes+512 || int64(len(archived)) > logMaxBytes+512 {
		t.Errorf("a generation passed the cap: active %d, archived %d, cap %d", len(body), len(archived), logMaxBytes)
	}
	if !strings.Contains(body, "line 399 ") || !strings.Contains(body, "exited with status 0") {
		t.Errorf("the active log lost the newest lines: %q", body[max(0, len(body)-200):])
	}
}
