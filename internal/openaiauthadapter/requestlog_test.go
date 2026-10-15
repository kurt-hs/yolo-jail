package openaiauthadapter

import (
	"bytes"
	"context"
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
	"github.com/mschulkind-oss/yolo-jail/internal/openauthclient"
)

// EVERY REFRESH IS ONE STAMPED LINE naming its auth outcome and the broker's, and NO TOKEN
// reaches it: not the caller token, not a wrong one, not the access or refresh token the broker
// returned. Drives Handler, the production handler Serve uses.
func TestEveryRefreshIsOneLineAndNoTokenReachesIt(t *testing.T) {
	var log bytes.Buffer
	requestLog.SetOutput(&log)
	t.Cleanup(func() { requestLog.SetOutput(os.Stderr) })
	now := time.Unix(1_700_000_000, 0)
	h := Handler(testToken, func(context.Context, string) (Token, error) {
		return Token{AccessToken: "access-SECRET", IDToken: "id-SECRET", RefreshToken: "yolo-broker:8",
			ExpiresAtMS: now.Add(time.Hour).UnixMilli()}, nil
	}, func() time.Time { return now })
	wrong := strings.Repeat("ef", 32)
	for _, c := range []struct {
		refresh string
		want    []string
	}{
		{"yolo-broker:7", []string{"POST /oauth/token 401", "auth=missing-token", "upstream=-", "the broker was not asked"}},
		{openauthclient.BindCallerToken("yolo-broker:7", wrong), []string{"POST /oauth/token 401", "auth=wrong-token"}},
		{bound("yolo-broker:7"), []string{"POST /oauth/token 200", "auth=ok", "upstream=host-openai-auth-broker",
			"upstream_ms=", "upstream_outcome=ok", "bytes_out=", "elapsed_ms="}},
	} {
		log.Reset()
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(RefreshForm(c.refresh).Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(httptest.NewRecorder(), req)
		line := strings.TrimRight(log.String(), "\n")
		t.Logf("%s", line)
		if strings.Count(line, "\n") != 0 || !logstamp.HasStamp([]byte(line)) || !strings.Contains(line, "openai-auth-adapter: request #") {
			t.Errorf("want one stamped request line, got %q", line)
		}
		for _, w := range c.want {
			if !strings.Contains(line, w) {
				t.Errorf("the request line %q lacks %q", line, w)
			}
		}
		for _, secret := range []string{testToken, wrong, "access-SECRET", "id-SECRET", "yolo-broker:"} {
			if strings.Contains(line, secret) {
				t.Fatalf("%q REACHED THE LOG: %s", secret, line)
			}
		}
	}
}

// A refused refresh, sent over a real loopback connection, names the agent that sent it
// (internal/peercaller): here this test binary, named as an agent by a launcher of its name.
// Drives Serve, so it fails if the adapter's log stops resolving callers.
func TestARefusedRefreshNamesTheAgentThatSentIt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("callers are named through Linux's /proc")
	}
	log := &lockedBuffer{}
	requestLog.SetOutput(log)
	t.Cleanup(func() { requestLog.SetOutput(os.Stderr) })
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
	go func() {
		_ = Serve(ln, testToken, func(context.Context, string) (Token, error) { return Token{}, nil })
	}()
	t.Cleanup(func() { _ = ln.Close() })

	resp, err := http.PostForm("http://"+ln.Addr().String()+"/oauth/token", RefreshForm("yolo-broker:7"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(log.String(), "POST /oauth/token 401") {
		if time.Now().After(deadline) {
			t.Fatalf("no request line:\n%s", log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	want := fmt.Sprintf(" caller=%s caller_pid=%d auth=missing-token", self, os.Getpid())
	if line := log.String(); !strings.Contains(line, want) {
		t.Errorf("the request line %q does not name its caller (%q)", line, want)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
