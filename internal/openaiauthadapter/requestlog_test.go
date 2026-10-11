package openaiauthadapter

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
