package reqlog

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func serve(t *testing.T, l *Logger) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(l.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		From(r.Context()).Set("auth", "ok")
		w.WriteHeader(http.StatusUnauthorized)
	})))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

// The resolver is handed the connection's two ends, and its fields land right after peer=.
func TestTheCallerFollowsThePeer(t *testing.T) {
	var out syncBuffer
	l := New("t", &out)
	var gotLocal, gotPeer string
	l.ResolveCallers(time.Second, func(_ context.Context, local, peer string) [][2]string {
		gotLocal, gotPeer = local, peer
		return [][2]string{{"caller", "claude"}, {"caller_pid", "136"}}
	})
	srv := serve(t, l)
	get(t, srv.URL)
	line := out.String()
	t.Log(line)
	if !strings.Contains(line, " 401 peer="+gotPeer+" caller=claude caller_pid=136 auth=ok ") {
		t.Errorf("caller fields not after peer: %q", line)
	}
	if gotLocal != srv.Listener.Addr().String() {
		t.Errorf("the resolver's local address = %q, want the listener's %q", gotLocal, srv.Listener.Addr())
	}
}

// A lookup that runs past its budget costs the response at most the budget, and the line says
// why it names no caller.
func TestASlowLookupIsBoundedAndSaysSo(t *testing.T) {
	var out syncBuffer
	l := New("t", &out)
	release := make(chan struct{})
	defer close(release)
	l.ResolveCallers(30*time.Millisecond, func(context.Context, string, string) [][2]string {
		<-release // ignores its context: the bound must not depend on the resolver honoring it
		return [][2]string{{"caller", "late"}}
	})
	srv := serve(t, l)
	start := time.Now()
	get(t, srv.URL)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the response took %s behind a stuck lookup", took)
	}
	line := out.String()
	t.Log(line)
	if !strings.Contains(line, `caller=unknown caller_why="the lookup ran past its 30ms budget"`) || strings.Contains(line, "late") {
		t.Errorf("want the budget's reason: %q", line)
	}
}
