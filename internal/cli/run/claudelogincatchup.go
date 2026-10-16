package run

import (
	"fmt"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/oauthbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/progress"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// claudelogincatchup.go is the launch's half of OQ-HD9 (docs/design/host-daemon-ownership.md,
// ruled 2026-10-15: "At launch, for sure. We're not running a timer or anything like that when
// we're not running."). Nothing of yolo's runs while no jail runs, so a machine idle past its
// access token's life comes back with a stale login; the launch that starts a jail's
// claude-oauth-broker catches it up, synchronously, before the agent's command runs.
//
// WHERE: startPlannedLoopholes, after the host services started and the launch check answered,
// and only when neither refused the launch. After the spawn because the broker's start makes its
// state directory (EnsureCAAndLeaf), where the refresh flock lives and which the flock does not
// create (withRefreshLock); and because a launch refused at that boundary never runs an agent,
// so it has no login to catch up. The one boundary both launch arms cross (the keeper's start,
// and run.go's macos-user arm through startLoopholesDisclosed), so neither can lose it.
//
// THE THRESHOLD is the broker's own background lead (oauthbroker.BackgroundRefreshLeadSeconds):
// a launch leaves the login at least as fresh as a running broker keeps it. Above it the launch
// does nothing and prints nothing.
//
// THE WAIT is not cut short. The refresh takes the machine's refresh flock and makes at most one
// upstream request, bounded by the broker's own upstream deadline (oauthbroker's httpClient), as
// is every request a lock holder ahead of it makes. Abandoning it would be worse than waiting: a
// launch that exits right after (a later refusal) could end the process between the provider
// redeeming the single-use refresh token and the write of its replacement, which is the burn the
// broker's detach exists to prevent (HD-R1 §1.3). A wait past progress.DefaultGrace shows a
// progress line.
//
// A FAILURE DOES NOT REFUSE THE LAUNCH: the jail's broker keeps trying in the background, and the
// line names the next step.

// claudeCatchUpNow and claudeCatchUpRefresh are the catch-up's clock and refresh, package seams
// so a test drives the whole call site with a fixture login and never reaches the network.
var (
	claudeCatchUpNow                                    = func() time.Time { return time.Now() }
	claudeCatchUpRefresh oauthbroker.CatchUpRefreshFunc = oauthbroker.LaunchCatchUpRefresh
	claudeCatchUpLead                                   = oauthbroker.BackgroundRefreshLeadSeconds
)

// catchUpClaudeLogin runs the catch-up when this launch started the claude-oauth-broker loophole.
func (o *Options) catchUpClaudeLogin(started []loopholeDaemon) {
	starts := false
	for _, h := range started {
		if h.name == broker.BrokerLoopholeName {
			starts = true
			break
		}
	}
	if !starts {
		return
	}
	// The broker's own store: the canonical login, the legacy shared file, the views and the
	// flock every broker on the machine takes (as brokercerts.go and the view registration
	// configure it in this process).
	oauthbroker.ConfigureStore()
	var res oauthbroker.CatchUp
	o.withProgressLine("Checking Claude's login", func(*progress.Line) (bool, string) {
		res = oauthbroker.CatchUpAtLaunch(oauthbroker.LegacyCredsPath(), claudeCatchUpLead,
			claudeCatchUpNow().UnixMilli(), claudeCatchUpRefresh)
		if res.Outcome == oauthbroker.CatchUpFailed {
			return false, "failed"
		}
		return true, "done"
	})
	if line := claudeCatchUpLine(res); line != "" {
		o.pr(o.Stderr).print(line)
	}
}

// claudeCatchUpLine is the one line a catch-up prints, "" when it did nothing.
func claudeCatchUpLine(res oauthbroker.CatchUp) string {
	switch res.Outcome {
	case oauthbroker.CatchUpRefreshed:
		now := ""
		if res.ExpiresInS > 0 {
			now = ", and has " + catchUpSpan(time.Duration(res.ExpiresInS)*time.Second) + " now"
		}
		return "[dim]Refreshed this machine's Claude login at launch: it " + catchUpHad(res.HeadroomMS) +
			now + ".[/dim]"
	case oauthbroker.CatchUpFailed:
		lead := "[yellow]Could not refresh this machine's Claude login at launch (it " +
			catchUpHad(res.HeadroomMS) + "): "
		switch {
		case res.DeadLogin():
			return lead + "the provider no longer accepts it (invalid_grant). Run /login inside " +
				"Claude in this jail to sign the machine in again.[/yellow]"
		case res.Transient():
			return lead + "the token endpoint could not be reached (" + richtext.Escape(res.Detail) +
				"). The jail's broker keeps retrying in the background; nothing to do unless " +
				"Claude reports it is logged out.[/yellow]"
		default:
			what := res.Error
			if res.Status != 0 {
				what += fmt.Sprintf(" %d", res.Status)
			}
			if res.Detail != "" {
				what += ": " + res.Detail
			}
			return lead + richtext.Escape(what) + ". The jail's broker keeps retrying in the " +
				"background; `yolo claude-auth status` on the host shows the login, and " +
				"`yolo claude-auth refresh` retries it now.[/yellow]"
		}
	}
	return ""
}

// catchUpHad says how much life the login had when the launch looked.
func catchUpHad(headroomMS int64) string {
	if headroomMS <= 0 {
		return "had expired " + catchUpSpan(time.Duration(-headroomMS)*time.Millisecond) + " before"
	}
	return "had " + catchUpSpan(time.Duration(headroomMS)*time.Millisecond) + " left"
}

// catchUpSpan is a duration at the precision a person reads a login's life in.
func catchUpSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		h, m := int(d/time.Hour), int((d%time.Hour)/time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}
