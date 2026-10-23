package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
       yolo approve --json
       yolo approve --watch [--json]

Host-only: list requests and grants, allow a request once, grant its requested
set for a bounded duration, deny it, or revoke a grant. Without --for, Allow once
covers that exact command for one use, for at most 15 minutes and never beyond
its set's ceiling. --for hands over the whole requested set, within its policy
ceiling and use cap. session lasts until the broker ends, capped at 12 hours.

--json prints pending requests and live grants as one JSON document. --watch
prints the current state, then one line per event (request filed, decided or
expired; grant used, ended or revoked) until interrupted; with --json, one JSON
object per line. Every JSON object carries "schema": 1. This is the surface a
UI built outside yolo uses. Such a UI must never accept approvals from the jails
it approves: a jail can reach a port the host listens on.

Exit codes: 0 done; 1 the store could not be read or written; 2 usage;
3 the request is already decided; 4 no request or grant has that id;
5 refused by policy (beyond the set's ceiling, or its jail no longer runs).

The host store is built, but the GitHub broker does not file requests or execute
writes yet. Its writes still refuse immediately; nothing waits for approval.
The notification and answer ping are not built either.

Examples:
  yolo approve
  yolo approve r-<id>
  yolo approve r-<id> --for 15m
  yolo approve r-<id> --deny
  yolo approve --revoke all
  yolo approve --watch --json
`

// The stable exit codes of `yolo approve` (boundary-broker.md §6.6). A UI keys on these.
const (
	approveExitFailed         = 1
	approveExitUsage          = 2
	approveExitAlreadyDecided = 3
	approveExitUnknownID      = 4
	approveExitPolicy         = 5
)

// approveWatchInterval is how often `--watch` polls the store.
var approveWatchInterval = time.Second

func runApprove(args []string) int {
	if answerHelp("approve", args, os.Stdout) {
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return approveMain(ctx, args[1:], config.InJail(), &brokerstore.Store{Dir: paths.BrokerSourceDir(ghbroker.Source), Source: ghbroker.Source, Revalidate: ghbroker.ApprovalAuthority, Audit: ghbroker.StoreAudit(brokeraudit.Open(paths.BrokerAuditLog(), nil))}, os.Stdout, os.Stderr)
}

// approveFailure maps a store error to its stable exit code and the next step its message
// names.
func approveFailure(err error, errs io.Writer) int {
	code, next := approveExitFailed, "check that the host store under "+paths.BrokerDir()+" is readable and writable, then retry"
	switch {
	case errors.Is(err, brokerstore.ErrAlreadyDecided):
		code, next = approveExitAlreadyDecided, "run `yolo approve` to see its state; the agent re-runs its command to file a new request"
	case errors.Is(err, brokerstore.ErrUnknownID):
		code, next = approveExitUnknownID, "run `yolo approve` to list the current ids; an ended record is kept 7 days"
	case errors.Is(err, brokerstore.ErrRefusedByPolicy):
		code, next = approveExitPolicy, "Allow it once or choose a shorter --for; to widen the set, edit its entry under `brokered` in "+paths.UserConfigPath()+"; a request whose jail stopped cannot be approved"
	}
	fmt.Fprintf(errs, "yolo approve: %s. Next: %s.\n", termsafe.Visible(err.Error()), next)
	return code
}

func approveMain(ctx context.Context, args []string, inJail bool, s *brokerstore.Store, out, errs io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(errs, "yolo approve: %v. See `yolo approve --help`; run `yolo approve` on the host to list requests and grants.\n", err)
		return approveExitUsage
	}
	if inJail {
		fmt.Fprintln(errs, "yolo approve is host-only: the request store is never mounted into a jail. Run `yolo approve` in a terminal on the host.")
		return approveExitUsage
	}
	var request, revoke string
	var d brokerstore.Decision
	forSeen, asJSON, watch := false, false, false
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
		case "--json", "--watch":
			if eq {
				return fail(fmt.Errorf("%s takes no value", name))
			}
			if name == "--json" {
				asJSON = true
			} else {
				watch = true
			}
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
	if (asJSON || watch) && (request != "" || revoke != "") {
		return fail(fmt.Errorf("decide or revoke without --json or --watch, which only list or follow the store"))
	}
	if watch {
		return approveWatch(ctx, s, asJSON, out, errs)
	}
	if revoke != "" {
		if err := s.Revoke(revoke); err != nil {
			return approveFailure(err, errs)
		}
		fmt.Fprintf(out, "Revoked %s. Run `yolo approve` to list requests and grants.\n", termsafe.Visible(revoke))
		return 0
	}
	if request != "" {
		g, err := s.Decide(request, "pending", d)
		if err != nil {
			return approveFailure(err, errs)
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
	snap, err := s.Snapshot()
	if err != nil {
		return approveFailure(err, errs)
	}
	if asJSON {
		return approveJSON(out, s.StateView(snap), errs)
	}
	printApproveList(out, snap)
	if len(snap.Requests) == 0 && len(snap.Grants) == 0 {
		fmt.Fprintln(out, "No requests or grants. GitHub broker writes still refuse without filing a request; broker approval integration is not built yet. Run `yolo approve --help` for the host store commands.")
	} else {
		fmt.Fprintln(out, "Decide with `yolo approve <request>` (once), `--for <duration>` (whole set), or `--deny`; revoke with `yolo approve --revoke <grant|all>`.")
	}
	return 0
}

func printApproveList(out io.Writer, snap brokerstore.Snapshot) {
	for _, r := range snap.Requests {
		fmt.Fprintf(out, "%s  %s  jail %s  set %s  [%s]  repo %s: %s\n", termsafe.Visible(r.ID), termsafe.Visible(r.State), termsafe.Visible(r.Jail), termsafe.Visible(r.Set), termsafe.Visible(r.Workspace), termsafe.Visible(r.Repo), shquote.JoinDisplay(r.Argv))
	}
	for _, g := range snap.Grants {
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
}

// approveJSON writes one object as one line.
func approveJSON(out io.Writer, v any, errs io.Writer) int {
	b, err := json.Marshal(v)
	if err != nil {
		return approveFailure(err, errs)
	}
	if _, err = out.Write(append(b, '\n')); err != nil {
		return approveExitFailed
	}
	return 0
}

// approveWatch is `--watch`: the current state, then each event, until ctx ends. It polls
// the store (brokerstore.Store.Watch says why).
func approveWatch(ctx context.Context, s *brokerstore.Store, asJSON bool, out, errs io.Writer) int {
	code := 0
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	emit := func(v any, line string) {
		if code != 0 {
			return
		}
		if asJSON {
			code = approveJSON(out, v, errs)
		} else if _, err := fmt.Fprintln(out, line); err != nil {
			code = approveExitFailed
		}
		if code != 0 {
			cancel() // the reader went away
		}
	}
	warned := false
	err := s.Watch(ctx, approveWatchInterval, func(snap brokerstore.Snapshot) {
		if asJSON {
			v := s.StateView(snap)
			v.Event = "state"
			emit(v, "")
			return
		}
		printApproveList(out, snap)
		fmt.Fprintln(errs, "Watching the store; one line per event. Press Ctrl-C to stop.")
	}, func(ev brokerstore.Event) {
		now := time.Now().UTC()
		emit(s.EventView(ev, now), now.Format(time.RFC3339)+" "+s.Summary(ev))
	}, func(err error) {
		if !warned {
			fmt.Fprintf(errs, "yolo approve: reading the store failed: %s; still watching, and retrying every %s.\n", termsafe.Visible(err.Error()), approveWatchInterval)
			warned = true
		}
	})
	if err != nil {
		return approveFailure(err, errs)
	}
	return code
}
