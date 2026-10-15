package awscredadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mschulkind-oss/yolo-jail/internal/peercaller"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
	"github.com/mschulkind-oss/yolo-jail/internal/reqlog"
)

// syncBuffer is a log sink a test reads while the handler writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func captureRequestLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	requestLog.SetOutput(buf)
	t.Cleanup(func() { requestLog.SetOutput(os.Stderr) })
	return buf
}

// lastLine is the log's last line, checked for the stamp.
func lastLine(t *testing.T, log string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	line := lines[len(lines)-1]
	t.Logf("%s", line)
	if !logstamp.HasStamp([]byte(line)) {
		t.Errorf("the request line carries no timestamp: %q", line)
	}
	return line
}

// EVERY REQUEST IS ONE STAMPED LINE naming its auth outcome and what the host said, and NO
// PRESENTED VALUE reaches it: the real token and a wrong one, both ways, never appear. These
// drive Handler, the one handler both Serve paths (the jail's and the doorway's) use, so
// deleting its request log fails them.
func TestEveryCredentialRequestIsOneLineAndNoTokenReachesIt(t *testing.T) {
	log := captureRequestLog(t)
	wrong := strings.Repeat("ef", 32)
	ok := Answer{OK: true, Body: json.RawMessage(`{"AccessKeyId":"AKIDSECRETISH","SecretAccessKey":"sek","Token":"tok-value","Expiration":"2026-10-11T18:00:00Z"}`)}
	for _, c := range []struct {
		what   string
		auth   string
		fetch  Fetch
		status int
		want   []string
	}{
		{"no token", "", nil, 401, []string{"GET /credentials 401", "auth=missing-token", "upstream=-", "the host service was not asked"}},
		{"a wrong token", wrong, nil, 401, []string{"GET /credentials 401", "auth=wrong-token", "upstream=-"}},
		{"served", "Bearer " + testToken, func() (Answer, error) { return ok, nil }, 200,
			[]string{"GET /credentials 200", "auth=ok", "upstream=host-aws-auth", "upstream_ms=", "upstream_outcome=ok", "bytes_out=", "elapsed_ms="}},
		{"a host refusal", testToken, func() (Answer, error) {
			return Answer{Body: json.RawMessage(`{"Code":"SessionExpired","Message":"run aws sso login --profile x"}`)}, nil
		}, 400, []string{"GET /credentials 400", "upstream_outcome=refused", "upstream_code=SessionExpired"}},
		{"an unreachable host", testToken, func() (Answer, error) {
			return Answer{}, errors.New("dial tcp 127.0.0.1:9: connect: connection refused")
		}, 400,
			[]string{"upstream_outcome=unreachable", `upstream_error="dial tcp 127.0.0.1:9: connect: connection refused"`}},
	} {
		req := httptest.NewRequest(http.MethodGet, CredentialsPath, nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		Handler(testToken, c.fetch).ServeHTTP(rec, req)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.what, rec.Code, c.status)
		}
		line := lastLine(t, log.String())
		for _, w := range c.want {
			if !strings.Contains(line, "aws-credential-adapter: request #") || !strings.Contains(line, w) {
				t.Errorf("%s: the request line %q lacks %q", c.what, line, w)
			}
		}
	}
	for _, secret := range []string{testToken, wrong, "AKIDSECRETISH", "tok-value", "sso login"} {
		if strings.Contains(log.String(), secret) {
			t.Fatalf("%q REACHED THE LOG:\n%s", secret, log.String())
		}
	}
}

// A request the host service has not answered is visible while it waits.
func TestACredentialRequestWaitingOnTheHostSaysSo(t *testing.T) {
	log := captureRequestLog(t)
	old := reqlog.StillWaitingInterval
	reqlog.StillWaitingInterval = 20 * time.Millisecond
	t.Cleanup(func() { reqlog.StillWaitingInterval = old })
	release := make(chan struct{})
	fetch := func() (Answer, error) { <-release; return Answer{}, errors.New("gave up") }
	req := httptest.NewRequest(http.MethodGet, CredentialsPath, nil)
	req.Header.Set("Authorization", testToken)
	done := make(chan struct{})
	go func() { Handler(testToken, fetch).ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), "still waiting on the host aws-auth credential service") {
		if time.Now().After(deadline) {
			t.Fatalf("no still-waiting line:\n%s", log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	<-done
	if !strings.Contains(log.String(), "still in flight after") {
		t.Errorf("the waiting line does not say how long:\n%s", log.String())
	}
}

// THE LINE NAMES THE AGENT THAT ASKED, the 2026-10-11 incident's missing fact: a request with
// no token, sent over a real loopback connection, is attributed through /proc to the agent
// whose process holds the other end — here this test binary, named as an agent by a launcher
// of its name in the home. Drives Serve, so it fails if the adapter's log stops resolving
// callers.
func TestARefusedRequestNamesTheAgentThatSentIt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("callers are named through Linux's /proc")
	}
	log := captureRequestLog(t)
	// A loaded CI runner can take longer than the production budget to walk /proc; this test
	// asserts that the caller is named, not how fast, so it gives the lookup room.
	requestLog.ResolveCallers(5*time.Second, peercaller.Lookup)
	t.Cleanup(func() { requestLog.ResolveCallers(peercaller.Budget, peercaller.Lookup) })
	home := t.TempDir()
	self := filepath.Base(os.Args[0])
	if err := os.MkdirAll(filepath.Join(home, ".yolo/bin/launch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".yolo/bin/launch", self), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = Serve(ln, testToken, nil) }()
	t.Cleanup(func() { _ = ln.Close() })

	resp, err := http.Get("http://" + ln.Addr().String() + CredentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), "GET /credentials 401") {
		if time.Now().After(deadline) {
			t.Fatalf("no request line:\n%s", log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	line := lastLine(t, log.String())
	want := fmt.Sprintf(" caller=%s caller_pid=%d auth=missing-token", self, os.Getpid())
	if !strings.Contains(line, want) {
		t.Errorf("the request line %q does not name its caller (%q)", line, want)
	}
}
