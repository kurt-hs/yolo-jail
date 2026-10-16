package hostfloor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTheUpstreamProbeClassifiesAsTheJailDoes pins the host floor's probe (PD-NF3) to the jail
// launchers' classification (_yolo_url_reachable, internal/entrypoint): a server error or no answer
// is an unreachable upstream; any other HTTP status is one that answered.
func TestTheUpstreamProbeClassifiesAsTheJailDoes(t *testing.T) {
	for _, tc := range []struct {
		status    int
		reachable bool
	}{{200, true}, {302, true}, {404, true}, {405, true}, {500, false}, {503, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		err := probeUpstreamHEAD(context.Background(), srv.URL+"/install.sh")
		srv.Close()
		if (err == nil) != tc.reachable {
			t.Errorf("status %d: reachable=%v, want %v (err=%v)", tc.status, err == nil, tc.reachable, err)
		}
	}
	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := gone.URL
	gone.Close()
	if err := probeUpstreamHEAD(context.Background(), url); err == nil {
		t.Errorf("a host that does not answer must be unreachable")
	}
}
