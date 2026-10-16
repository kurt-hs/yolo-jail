package oauthbroker

import (
	"os"
	"path/filepath"
	"strings"
)

// catchup.go is the AT-LAUNCH CATCH-UP REFRESH of the machine's Claude login
// (docs/design/host-daemon-ownership.md, OQ-HD9, ruled 2026-10-15): nothing of yolo's runs when
// no jail runs, so nothing refreshes the shared credential between sessions, and a launch that
// starts the claude-oauth-broker loophole checks the login's headroom and refreshes it before the
// agent needs it. No timer is installed anywhere; this is the whole of the catch-up.
//
// The decision and the refresh live here, beside the broker's own, so the catch-up takes the
// broker's path: the same refresh flock (withRefreshLock), the same store sync and view writes,
// and the same replaced-token memory (HD-D16). A broker that refreshed first is seen inside the
// lock and answers from the cache, so nothing is spent twice.

// CatchUpOutcome is what a launch's catch-up did.
type CatchUpOutcome int

const (
	// CatchUpNoLogin: no shared credential to refresh (no file, an unreadable one, or no refresh
	// token). Nothing was done, and the launch says nothing.
	CatchUpNoLogin CatchUpOutcome = iota
	// CatchUpNotDue: the login has at least the broker's background lead left. Nothing was done.
	CatchUpNotDue
	// CatchUpRefreshed: the login was due and is fresh now (refreshed, or found refreshed by a
	// broker that took the lock first).
	CatchUpRefreshed
	// CatchUpFailed: the login was due and the refresh failed; Error says how.
	CatchUpFailed
)

// CatchUp is one launch's catch-up: what it did, and the facts its one line reports.
type CatchUp struct {
	Outcome CatchUpOutcome
	// HeadroomMS is the access token's life left when the launch looked; negative once expired.
	// Meaningful for every outcome but CatchUpNoLogin.
	HeadroomMS int64
	// ExpiresInS is the refreshed login's life, from the broker's reply (CatchUpRefreshed).
	ExpiresInS int64
	// Error is the broker's error code (CatchUpFailed): upstream_http, upstream_unreachable,
	// upstream_bad_response, creds_unreadable, no_refresh_token.
	Error string
	// Status is the upstream HTTP status of an upstream_http failure, 0 otherwise.
	Status int64
	// Detail is the failure's message or the start of the upstream body. Never a token: the
	// token endpoint's error bodies carry an error code and a description.
	Detail string
}

// DeadLogin reports a refresh token the provider refused for good (`invalid_grant`), which
// only a new /login cures.
func (c CatchUp) DeadLogin() bool {
	return c.Outcome == CatchUpFailed && c.Error == "upstream_http" &&
		strings.Contains(c.Detail, "invalid_grant")
}

// Transient reports a failure to reach the token endpoint, which the broker's background
// refresher retries on its own (BackgroundRefreshTick's fast retry).
func (c CatchUp) Transient() bool {
	return c.Outcome == CatchUpFailed && c.Error == "upstream_unreachable"
}

// CatchUpRefreshFunc is the refresh a catch-up runs once the login is due: credsPath is the
// shared credentials file, leadSeconds the headroom the refresh must leave. LaunchCatchUpRefresh
// is production's; a test passes a fake, so no test reaches the network.
type CatchUpRefreshFunc func(credsPath string, leadSeconds int) RefreshResult

// LaunchCatchUpRefresh is the production refresh: the background refresher's own
// (doBackgroundRefresh), whose cache floor is the lead, so "due" and "refreshed" are one
// decision taken inside the flock. The store must be configured first (ConfigureStore), which
// also names the flock every broker on the machine takes.
func LaunchCatchUpRefresh(credsPath string, leadSeconds int) RefreshResult {
	logInfo("do_refresh: at-launch catch-up (lead %ds)", leadSeconds)
	return doBackgroundRefresh(credsPath, leadSeconds)
}

// CatchUpAtLaunch decides and runs one launch's catch-up: nothing without a login, nothing
// while the login has leadSeconds or more left (nowMS is the clock, in epoch milliseconds), and
// otherwise refresh. The headroom is read unlocked, as the background tick reads it; the refresh
// re-reads it under the lock.
func CatchUpAtLaunch(credsPath string, leadSeconds int, nowMS int64, refresh CatchUpRefreshFunc) CatchUp {
	readPath := storeFor(credsPath).readPath()
	oauth, err := oauthFromCreds(readPath)
	if err != nil {
		logInfo("at_launch: no login to catch up (%s)", describeCreds(readPath))
		return CatchUp{Outcome: CatchUpNoLogin}
	}
	if rt, _ := stringField(oauth, "refreshToken"); rt == "" {
		logInfo("at_launch: no login to catch up (no refresh token) shared=%s", describeCreds(readPath))
		return CatchUp{Outcome: CatchUpNoLogin}
	}
	v, ok := oauth.Get("expiresAt")
	if !ok {
		// No expiry to judge by: the background refresher does not refresh such a login either
		// (RefreshDue), and Claude refreshes on its own 401.
		logInfo("at_launch: skip (no expiresAt) shared=%s", describeCreds(readPath))
		return CatchUp{Outcome: CatchUpNotDue}
	}
	expiresAtMS, ok := asInt64(v)
	if !ok {
		return CatchUp{Outcome: CatchUpNotDue}
	}
	out := CatchUp{HeadroomMS: expiresAtMS - nowMS}
	if out.HeadroomMS >= int64(leadSeconds)*1000 {
		logInfo("at_launch: skip (not due: %ds left, lead %ds)", out.HeadroomMS/1000, leadSeconds)
		out.Outcome = CatchUpNotDue
		return out
	}
	logInfo("at_launch: due (%ds left, lead %ds) shared=%s", out.HeadroomMS/1000, leadSeconds,
		describeCreds(readPath))
	result := refresh(credsPath, leadSeconds)
	if result == nil {
		out.Outcome, out.Error = CatchUpFailed, "no_result"
		return out
	}
	if e, isErr := result.Get("error"); isErr {
		out.Outcome = CatchUpFailed
		out.Error = stringOf(e)
		if s, ok := result.Get("status"); ok {
			out.Status, _ = asInt64(s)
		}
		if m, ok := result.Get("message"); ok {
			out.Detail = stringOf(m)
		} else if b, ok := result.Get("body"); ok {
			out.Detail = stringOf(b)
		}
		logWarn("at_launch: refresh failed error=%s status=%d message=%s", out.Error, out.Status, out.Detail)
		return out
	}
	out.Outcome = CatchUpRefreshed
	if v, ok := result.Get("expires_in"); ok {
		out.ExpiresInS, _ = asInt64(v)
	}
	logInfo("at_launch: ok expires_in=%d", out.ExpiresInS)
	return out
}

// CatchUpAtLaunchLogged is CatchUpAtLaunch with the broker's forensic lines (the decision, and
// the refresh path's own cache-hit / refreshed / upstream lines) appended to logPath, the log
// the claude-oauth-broker daemon writes (broker.BrokerLogPath), so a later diagnosis reads a
// launch's refresh beside the daemons' own. The file is opened O_APPEND, as every daemon's copy
// of it is, so these lines interleave whole with a running daemon's; every line names
// "at_launch" or follows the "do_refresh: at-launch catch-up" line. A log that cannot be opened
// costs the lines and nothing else.
//
// The package logger is swapped for the call and restored after it: the launch process has no
// broker log of its own, and the catch-up runs on the launch's one goroutine before anything else
// in that process uses this package's log.
func CatchUpAtLaunchLogged(logPath, credsPath string, leadSeconds int, nowMS int64,
	refresh CatchUpRefreshFunc) CatchUp {
	if logPath != "" {
		_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			savedLogger, savedVerbose := logger, logVerbose
			setupLogWriter(f, false)
			defer func() {
				logger, logVerbose = savedLogger, savedVerbose
				_ = f.Close()
			}()
			logInfo("at_launch: catch-up by launch pid %d", os.Getpid())
		}
	}
	return CatchUpAtLaunch(credsPath, leadSeconds, nowMS, refresh)
}
