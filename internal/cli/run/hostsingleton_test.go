package run

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// hostsingleton_test.go keeps the front-dialing helper of the tests of a MACHINE-WIDE daemon,
// which no launch of this build starts any more (HD-R1, docs/design/host-daemon-ownership.md). The
// tests that pinned yolo ENSURING one (startHostSingleton) went with it; the fixture of an older
// yolo's copy still running is launchcheck_test.go's leaveAnOlderCopyRunning, since a launch names
// one it finds and leaves it running.

// dialFrontLine sends one newline-terminated request through a published endpoint
// and returns the response line. The endpoint is the jail-facing half: pinned
// certificate, bearer token, then bytes.
func dialFrontLine(t *testing.T, endpointPath, req string) string {
	t.Helper()
	conn, err := svcendpoint.DialLocal(endpointPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", endpointPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		t.Fatalf("writing to %s: %v", endpointPath, err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("reading from %s: %v", endpointPath, err)
	}
	return strings.TrimSpace(line)
}
