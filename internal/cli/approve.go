package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/ghbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

const approveUsage = `Usage: yolo approve [request] [--for <duration|session> | --deny]
       yolo approve --revoke <grant|all>

Host-only: list requests and grants, allow a request once, grant its requested
set for a bounded duration, deny it, or revoke a grant. Without --for, Allow once
covers that exact command for one use, for at most 15 minutes and never beyond
its set's ceiling. --for hands over the whole requested set, within its policy
ceiling and use cap. session lasts until the broker ends, capped at 12 hours.

The host store is built, but the GitHub broker does not file requests or execute
writes yet. Its writes still refuse immediately; nothing waits for approval.
The notification and answer ping are not built either.

Examples:
  yolo approve
  yolo approve r-<id>
  yolo approve r-<id> --for 15m
  yolo approve r-<id> --deny
  yolo approve --revoke all
`

func runApprove(args []string) int {
	if answerHelp("approve", args, os.Stdout) {
		return 0
	}
	return approveMain(args[1:], config.InJail(), &brokerstore.Store{Dir: paths.BrokerSourceDir("github"), Revalidate: ghbroker.ApprovalAuthority, Audit: ghbroker.StoreAudit(brokeraudit.Open(paths.BrokerAuditLog(), nil))}, os.Stdout, os.Stderr)
}
func approveMain(args []string, inJail bool, s *brokerstore.Store, out, errs io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(errs, "yolo approve: %v. See `yolo approve --help`; run `yolo approve` on the host to list requests and grants.\n", err)
		return 2
	}
	if inJail {
		fmt.Fprintln(errs, "yolo approve is host-only: the request store is never mounted into a jail. Run `yolo approve` in a terminal on the host.")
		return 2
	}
	var request, revoke string
	var d brokerstore.Decision
	forSeen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, eq := strings.Cut(a, "=")
		switch name {
		case "--for", "--revoke":
			if !eq {
				i++
				if i >= len(args) {
					return fail(fmt.Errorf("%s needs a value", name))
				}
				value = args[i]
			}
			if name == "--revoke" {
				if revoke != "" || value == "" {
					return fail(fmt.Errorf("name one grant or all for --revoke"))
				}
				revoke = value
				continue
			}
			if forSeen {
				return fail(fmt.Errorf("specify --for only once"))
			}
			forSeen = true
			if value == "session" {
				d.Session = true
			} else {
				duration, err := time.ParseDuration(value)
				if err != nil || duration <= 0 {
					return fail(fmt.Errorf("give --for a positive duration or session"))
				}
				d.For = duration
			}
		case "--deny":
			if eq || d.Deny {
				return fail(fmt.Errorf("specify --deny once, without a value"))
			}
			d.Deny = true
		default:
			if strings.HasPrefix(a, "-") || request != "" {
				return fail(fmt.Errorf("unexpected argument %q", a))
			}
			request = a
		}
	}
	if (d.Deny && forSeen) || (revoke != "" && (request != "" || d.Deny || forSeen)) || ((d.Deny || forSeen) && request == "") {
		return fail(fmt.Errorf("choose one request decision, or --revoke"))
	}
	if revoke != "" {
		if err := s.Revoke(revoke); err != nil {
			return fail(err)
		}
		fmt.Fprintf(out, "Revoked %s. Run `yolo approve` to list requests and grants.\n", termsafe.Visible(revoke))
		return 0
	}
	if request != "" {
		g, err := s.Decide(request, "pending", d)
		if err != nil {
			return fail(err)
		}
		if d.Deny {
			fmt.Fprintf(out, "Denied %s. Run `yolo approve` to list requests and grants.\n", termsafe.Visible(request))
		} else {
			coverage := "one exact command"
			if g.Digest == "" {
				coverage = "whole set " + termsafe.Visible(g.Set)
			}
			fmt.Fprintf(out, "Allowed %s: %s, %d use(s), expires %s (%s). Run `yolo approve` to list or `yolo approve --revoke %s` to revoke.\n", termsafe.Visible(request), coverage, g.Uses, g.Expires.Format(time.RFC3339), g.ID, g.ID)
		}
		return 0
	}
	rs, gs, err := s.List()
	if err != nil {
		return fail(err)
	}
	for _, r := range rs {
		fmt.Fprintf(out, "%s  %s  jail %s  set %s  [%s]  repo %s: %s\n", termsafe.Visible(r.ID), termsafe.Visible(r.State), termsafe.Visible(r.Jail), termsafe.Visible(r.Set), termsafe.Visible(r.Workspace), termsafe.Visible(r.Repo), shquote.JoinDisplay(r.Argv))
	}
	for _, g := range gs {
		state := "live"
		if !g.Ended.IsZero() {
			state = "ended"
		}
		coverage := g.Set
		if g.Digest != "" {
			coverage += " (once)"
		}
		fmt.Fprintf(out, "%s  %s  jail %s  set %s  %d use(s)  expires %s\n", termsafe.Visible(g.ID), state, termsafe.Visible(g.Jail), termsafe.Visible(coverage), g.Uses, g.Expires.Format(time.RFC3339))
	}
	if len(rs) == 0 && len(gs) == 0 {
		fmt.Fprintln(out, "No requests or grants. GitHub broker writes still refuse without filing a request; broker approval integration is not built yet. Run `yolo approve --help` for the host store commands.")
	} else {
		fmt.Fprintln(out, "Decide with `yolo approve <request>` (once), `--for <duration>` (whole set), or `--deny`; revoke with `yolo approve --revoke <grant|all>`.")
	}
	return 0
}
