package oauthbroker

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// catchup_test.go pins the at-launch catch-up's DECISION (OQ-HD9, catchup.go): nothing without a
// login, nothing while the login has the lead left, and otherwise one refresh whose answer is
// classified for the launch's line. The refresh is a fake, so nothing here reaches the network,
// except TestTheCatchUpRefreshIsACacheHitWhenAnotherBrokerRefreshedFirst, which proves the
// production refresh does NOT reach it.

var catchUpNow = time.Unix(1_800_000_000, 0)

func writeCatchUpLogin(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func loginLeft(t *testing.T, left time.Duration) string {
	t.Helper()
	return writeCatchUpLogin(t, `{"claudeAiOauth":{"accessToken":"at","refreshToken":"rt","expiresAt":`+
		strconv.FormatInt(catchUpNow.Add(left).UnixMilli(), 10)+`}}`)
}

func answering(calls *int, pairs ...any) CatchUpRefreshFunc {
	return func(string, int) RefreshResult {
		*calls++
		m := jsonx.NewOrderedMap()
		for i := 0; i+1 < len(pairs); i += 2 {
			m.Set(pairs[i].(string), pairs[i+1])
		}
		return m
	}
}

func TestTheCatchUpDecision(t *testing.T) {
	saved := CanonicalPath
	CanonicalPath = "" // single-file store: the fixture IS the login
	t.Cleanup(func() { CanonicalPath = saved })
	lead := BackgroundRefreshLeadSeconds
	ok := []any{"access_token", "a2", "refresh_token", "r2", "expires_in", jsonx.IntValue(28800)}

	for _, tc := range []struct {
		name        string
		creds       func(t *testing.T) string
		answer      []any
		want        CatchUpOutcome
		wantCalls   int
		dead, trans bool
	}{
		{"no file", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.json") },
			ok, CatchUpNoLogin, 0, false, false},
		{"unreadable JSON", func(t *testing.T) string { return writeCatchUpLogin(t, "{") },
			ok, CatchUpNoLogin, 0, false, false},
		{"signed out", func(t *testing.T) string { return writeCatchUpLogin(t, `{"claudeAiOauth":{}}`) },
			ok, CatchUpNoLogin, 0, false, false},
		{"no expiry to judge", func(t *testing.T) string {
			return writeCatchUpLogin(t, `{"claudeAiOauth":{"refreshToken":"rt"}}`)
		}, ok, CatchUpNotDue, 0, false, false},
		{"exactly the lead left", func(t *testing.T) string { return loginLeft(t, time.Duration(lead)*time.Second) },
			ok, CatchUpNotDue, 0, false, false},
		{"hours left", func(t *testing.T) string { return loginLeft(t, 5*time.Hour) },
			ok, CatchUpNotDue, 0, false, false},
		{"inside the lead", func(t *testing.T) string { return loginLeft(t, 20*time.Minute) },
			ok, CatchUpRefreshed, 1, false, false},
		{"expired a week ago", func(t *testing.T) string { return loginLeft(t, -7*24*time.Hour) },
			ok, CatchUpRefreshed, 1, false, false},
		{"dead login", func(t *testing.T) string { return loginLeft(t, -time.Hour) },
			[]any{"error", "upstream_http", "status", jsonx.IntValue(400),
				"body", `{"error": "invalid_grant", "error_description": "Invalid refresh token"}`},
			CatchUpFailed, 1, true, false},
		{"network down", func(t *testing.T) string { return loginLeft(t, time.Minute) },
			[]any{"error", "upstream_unreachable", "message", "dial tcp: i/o timeout"},
			CatchUpFailed, 1, false, true},
		{"server error", func(t *testing.T) string { return loginLeft(t, time.Minute) },
			[]any{"error", "upstream_http", "status", jsonx.IntValue(503), "body", "unavailable"},
			CatchUpFailed, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := CatchUpAtLaunch(tc.creds(t), lead, catchUpNow.UnixMilli(), answering(&calls, tc.answer...))
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %v, want %v (%+v)", got.Outcome, tc.want, got)
			}
			if calls != tc.wantCalls {
				t.Errorf("refreshed %d times, want %d", calls, tc.wantCalls)
			}
			if got.DeadLogin() != tc.dead || got.Transient() != tc.trans {
				t.Errorf("DeadLogin=%v Transient=%v, want %v %v (%+v)",
					got.DeadLogin(), got.Transient(), tc.dead, tc.trans, got)
			}
			if tc.want == CatchUpRefreshed && got.ExpiresInS != 28800 {
				t.Errorf("ExpiresInS = %d, want the reply's 28800", got.ExpiresInS)
			}
			if tc.name == "server error" && (got.Status != 503 || got.Detail != "unavailable") {
				t.Errorf("the failure lost its status or body: %+v", got)
			}
		})
	}
}

// TestTheCatchUpRefreshIsACacheHitWhenAnotherBrokerRefreshedFirst: the production refresh re-reads
// the login inside the flock with the lead as its floor, so a launch whose unlocked look found the
// login due, behind a broker that refreshed it meanwhile, spends nothing. The upstream URL is one
// nothing listens on: a request would fail the test as upstream_unreachable.
func TestTheCatchUpRefreshIsACacheHitWhenAnotherBrokerRefreshedFirst(t *testing.T) {
	saved, savedNow, savedLock := CanonicalPath, nowFunc, RefreshLockPath
	CanonicalPath = ""
	RefreshLockPath = filepath.Join(t.TempDir(), "refresh.lock")
	nowFunc = func() int64 { return catchUpNow.UnixMilli() }
	t.Cleanup(func() { CanonicalPath, nowFunc, RefreshLockPath = saved, savedNow, savedLock })
	t.Setenv("YOLO_BROKER_UPSTREAM_URL", "http://127.0.0.1:1/v1/oauth/token")

	p := loginLeft(t, 10*time.Minute)
	refreshedByAnother := func(credsPath string, lead int) RefreshResult {
		// The other broker's write lands between the launch's look and its turn at the lock.
		body := `{"claudeAiOauth":{"accessToken":"at2","refreshToken":"rt2","expiresAt":` +
			strconv.FormatInt(catchUpNow.Add(8*time.Hour).UnixMilli(), 10) + `}}`
		if err := os.WriteFile(credsPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return LaunchCatchUpRefresh(credsPath, lead)
	}
	got := CatchUpAtLaunch(p, BackgroundRefreshLeadSeconds, catchUpNow.UnixMilli(), refreshedByAnother)
	if got.Outcome != CatchUpRefreshed || got.ExpiresInS != 8*3600 {
		t.Fatalf("want a cache hit with the other broker's eight hours, got %+v", got)
	}
}
