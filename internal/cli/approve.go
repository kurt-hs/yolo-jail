package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/ghbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/gitbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

const approveUsage = `Usage: yolo approve [request] [--for <duration|session> | --deny]
       yolo approve --revoke <grant|w-entry|all> [--workspace <path>]
       yolo approve --json
       yolo approve --watch [--json]

Host-only: list requests, grants and each workspace's working set, allow a
request once, grant its requested set for a bounded duration, deny it, or
revoke a grant or a working-set entry. Without --for, Allow once covers that
exact command for one use, for at most 15 minutes and never beyond its set's
ceiling. --for hands over the whole requested set, within its policy ceiling
and use cap. session lasts until the broker ends, capped at 12 hours.

The working set is the branches a workspace's git-broker pushes unasked: each
one the agent created through the broker under a create-branch grant, until
the branch is deleted or the entry revoked. --revoke w-<id> ends one entry.
--revoke all ends every live grant; --revoke all --workspace <path> ends that
workspace's grants and its whole working set.

--json prints pending requests, live grants and live working-set entries as one
JSON document. --watch prints the current state, then one line per event
(request filed, decided or expired; grant used, ended or revoked) until
interrupted; with --json, one JSON object per line. Every JSON object carries
"schema": 1. A request whose on_request command failed says so, in the listing
and as "hook_failure" and "hook_output" in its JSON. This is the surface a UI
built outside yolo uses. Such a UI must never accept approvals from the jails
it approves: a jail can reach a port the host listens on.

Exit codes: 0 done; 1 the store could not be read or written; 2 usage;
3 the request is already decided; 4 no request, grant or entry has that id;
5 refused by policy (beyond the set's ceiling); 6 the host can no longer confirm
the request (its jail's launch is gone or its scope changed), so it cannot be
allowed. A request whose jail stopped normally is already refused, which is 3.

No broker files requests yet: a GitHub write, and a git push that neither a
create-branch grant nor the working set covers, refuse immediately; nothing
waits for approval. The answer ping is not built either; a desktop notification
is the user's own on_request command (see yolo config-ref).

Examples:
  yolo approve
  yolo approve r-<id>
  yolo approve r-<id> --for 15m
  yolo approve r-<id> --deny
  yolo approve --revoke w-<id>
  yolo approve --revoke all
  yolo approve --revoke all --workspace ~/code/app
  yolo approve --watch --json
`

// The stable exit codes of `yolo approve` (boundary-broker.md §6.6). A UI keys on these.
const (
	approveExitFailed         = 1
	approveExitUsage          = 2
	approveExitAlreadyDecided = 3
	approveExitUnknownID      = 4
	approveExitPolicy         = 5
	approveExitNotConfirmed   = 6
)

// approveWatchInterval is how often `--watch` polls the store.
var approveWatchInterval = time.Second

func runApprove(args []string) int {
	if answerHelp("approve", args, os.Stdout) {
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Every registered source's store: the GitHub broker's requests and grants, and the git
	// broker's working set. The git source files no request in this version, so its store
	// needs no approval authority; a decision on it fails closed.
	stores := []*brokerstore.Store{
		{Dir: paths.BrokerSourceDir(ghbroker.Source), Source: ghbroker.Source, Revalidate: ghbroker.ApprovalAuthority, Audit: ghbroker.StoreAudit(brokeraudit.Open(paths.BrokerAuditLog(), nil))},
		{Dir: paths.BrokerSourceDir(gitbroker.Source), Source: gitbroker.Source},
	}
	return approveAll(ctx, args[1:], config.InJail(), stores, os.Stdout, os.Stderr)
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
		code, next = approveExitPolicy, "Allow it once or choose a shorter --for; to widen the set, edit its entry under `brokered` in "+paths.UserConfigPath()
	case errors.Is(err, brokerstore.ErrNotConfirmed):
		code, next = approveExitNotConfirmed, "deny it with `yolo approve <id> --deny`, or let it expire; if its jail still runs, restart it and have the agent re-run its command to file a new request"
	}
	fmt.Fprintf(errs, "yolo approve: %s. Next: %s.\n", termsafe.Visible(err.Error()), next)
	return code
}

// approveMain is approveAll over one store.
func approveMain(ctx context.Context, args []string, inJail bool, s *brokerstore.Store, out, errs io.Writer) int {
	return approveAll(ctx, args, inJail, []*brokerstore.Store{s}, out, errs)
}

// approveAll is `yolo approve` over every store in stores: it lists them all, and finds a
// request, grant or working-set entry by its id in whichever store holds it.
func approveAll(ctx context.Context, args []string, inJail bool, stores []*brokerstore.Store, out, errs io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(errs, "yolo approve: %v. See `yolo approve --help`; run `yolo approve` on the host to list requests, grants and working sets.\n", err)
		return approveExitUsage
	}
	if inJail {
		fmt.Fprintln(errs, "yolo approve is host-only: the request store is never mounted into a jail. Run `yolo approve` in a terminal on the host.")
		return approveExitUsage
	}
	var request, revoke, workspace string
	var d brokerstore.Decision
	forSeen, asJSON, watch, workspaceSeen := false, false, false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, eq := strings.Cut(a, "=")
		switch name {
		case "--for", "--revoke", "--workspace":
			if !eq {
				i++
				if i >= len(args) {
					return fail(fmt.Errorf("%s needs a value", name))
				}
				value = args[i]
			}
			switch name {
			case "--revoke":
				if revoke != "" || value == "" {
					return fail(fmt.Errorf("name one grant, one working-set entry, or all for --revoke"))
				}
				revoke = value
				continue
			case "--workspace":
				if workspaceSeen || value == "" {
					return fail(fmt.Errorf("give --workspace one workspace path"))
				}
				workspaceSeen, workspace = true, value
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
	if workspaceSeen && revoke != "all" {
		return fail(fmt.Errorf("the --workspace flag narrows only --revoke all; revoke one grant or entry by its id alone"))
	}
	if (asJSON || watch) && (request != "" || revoke != "") {
		return fail(fmt.Errorf("decide or revoke without --json or --watch, which only list or follow the store"))
	}
	if watch {
		return approveWatch(ctx, stores, asJSON, out, errs)
	}
	if revoke != "" {
		return approveRevoke(stores, revoke, workspace, out, errs)
	}
	if request != "" {
		var g brokerstore.Grant
		err := eachStore(stores, func(s *brokerstore.Store) error {
			var err error
			g, err = s.Decide(request, "pending", d)
			return err
		})
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
	snaps := make([]brokerstore.Snapshot, len(stores))
	for i, s := range stores {
		snap, err := s.Snapshot()
		if err != nil {
			return approveFailure(err, errs)
		}
		snaps[i] = snap
	}
	if asJSON {
		v := brokerstore.StateView{Schema: brokerstore.Schema, Requests: []brokerstore.RequestView{}, Grants: []brokerstore.GrantView{}, Working: []brokerstore.WorkingView{}}
		for i, s := range stores {
			sv := s.StateView(snaps[i])
			v.Requests = append(v.Requests, sv.Requests...)
			v.Grants = append(v.Grants, sv.Grants...)
			v.Working = append(v.Working, sv.Working...)
		}
		return approveJSON(out, v, errs)
	}
	empty := true
	for i, s := range stores {
		printApproveList(out, snaps[i])
		printWorkingSet(out, s, snaps[i])
		empty = empty && len(snaps[i].Requests) == 0 && len(snaps[i].Grants) == 0 && !anyLive(snaps[i].Working)
	}
	if empty {
		fmt.Fprintln(out, "No requests, grants or working-set entries. Brokers do not file requests yet: a GitHub write, and a git push nothing covers, refuse without one. Run `yolo approve --help` for the host store commands.")
	} else {
		fmt.Fprintln(out, "Decide with `yolo approve <request>` (once), `--for <duration>` (whole set), or `--deny`; revoke with `yolo approve --revoke <grant|w-entry|all> [--workspace <path>]`.")
	}
	return 0
}

// eachStore runs fn on each store in turn until one holds the id: the first result that is not
// ErrUnknownID, or the last unknown-id error when none holds it.
func eachStore(stores []*brokerstore.Store, fn func(*brokerstore.Store) error) error {
	var err error
	for _, s := range stores {
		if err = fn(s); !errors.Is(err, brokerstore.ErrUnknownID) {
			return err
		}
	}
	return err
}

// approveRevoke is `--revoke <id|all> [--workspace <path>]`.
func approveRevoke(stores []*brokerstore.Store, revoke, workspace string, out, errs io.Writer) int {
	if revoke != "all" {
		if err := eachStore(stores, func(s *brokerstore.Store) error { return s.Revoke(revoke) }); err != nil {
			return approveFailure(err, errs)
		}
		fmt.Fprintf(out, "Revoked %s. Run `yolo approve` to list requests, grants and working sets.\n", termsafe.Visible(revoke))
		return 0
	}
	if workspace == "" {
		for _, s := range stores {
			if err := s.Revoke("all"); err != nil {
				return approveFailure(err, errs)
			}
		}
		fmt.Fprintln(out, "Revoked every live grant; working sets are unchanged (`--revoke all --workspace <path>` ends one workspace's). Run `yolo approve` to list requests, grants and working sets.")
		return 0
	}
	// A workspace is recorded by its host path as the launch resolved it; the path given here
	// is matched both as written (made absolute) and with its symlinks resolved.
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return approveFailure(err, errs)
	}
	forms := []string{filepath.Clean(abs)}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != forms[0] {
		forms = append(forms, real)
	}
	for _, s := range stores {
		for _, ws := range forms {
			if err := s.RevokeIn("all", ws); err != nil {
				return approveFailure(err, errs)
			}
		}
	}
	fmt.Fprintf(out, "Revoked every live grant and working-set entry of %s. Run `yolo approve` to list requests, grants and working sets.\n", termsafe.Visible(forms[0]))
	return 0
}

func anyLive(ws []brokerstore.WorkingEntry) bool {
	for _, e := range ws {
		if e.Live() {
			return true
		}
	}
	return false
}

func printApproveList(out io.Writer, snap brokerstore.Snapshot) {
	for _, r := range snap.Requests {
		fmt.Fprintf(out, "%s  %s  jail %s  set %s  [%s]  repo %s: %s\n", termsafe.Visible(r.ID), termsafe.Visible(r.State), termsafe.Visible(r.Jail), termsafe.Visible(r.Set), termsafe.Visible(r.Workspace), termsafe.Visible(r.Repo), shquote.JoinDisplay(r.Argv))
		if r.HookFailure != "" {
			fmt.Fprintf(out, "    your on_request failed, so you may not have been notified: %s (its output: `yolo approve --json`)\n", termsafe.Visible(r.HookFailure))
		}
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

// printWorkingSet lists the store's live working-set entries, one line each: what a workspace
// may push unasked. An ended entry covers nothing and is not listed.
func printWorkingSet(out io.Writer, s *brokerstore.Store, snap brokerstore.Snapshot) {
	for _, e := range snap.Working {
		if !e.Live() {
			continue
		}
		fmt.Fprintf(out, "%s  working set  %s  [%s]  repo %s  %s %s  by %s  since %s\n", termsafe.Visible(e.ID), termsafe.Visible(s.SourceName()), termsafe.Visible(e.Workspace), termsafe.Visible(e.Repo), termsafe.Visible(e.Kind), termsafe.Visible(e.Resource), termsafe.Visible(e.By), e.Added.Format(time.RFC3339))
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

// approveWatch is `--watch`: each store's current state, then each event, until ctx ends. It
// polls the stores, each on its own (brokerstore.Store.Watch says why); with --json each
// store's opening state is its own "state" object.
func approveWatch(ctx context.Context, stores []*brokerstore.Store, asJSON bool, out, errs io.Writer) int {
	var mu sync.Mutex
	code := 0
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	emit := func(v any, line string) {
		mu.Lock()
		defer mu.Unlock()
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
	var wg sync.WaitGroup
	var firstErr error
	started := 0
	for _, s := range stores {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			warned := false
			err := s.Watch(ctx, approveWatchInterval, func(snap brokerstore.Snapshot) {
				if asJSON {
					v := s.StateView(snap)
					v.Event = "state"
					emit(v, "")
					return
				}
				mu.Lock()
				printApproveList(out, snap)
				printWorkingSet(out, s, snap)
				started++
				if started == len(stores) {
					fmt.Fprintln(errs, "Watching the store; one line per event. Press Ctrl-C to stop.")
				}
				mu.Unlock()
			}, func(ev brokerstore.Event) {
				now := time.Now().UTC()
				emit(s.EventView(ev, now), now.Format(time.RFC3339)+" "+s.Summary(ev))
			}, func(err error) {
				mu.Lock()
				defer mu.Unlock()
				if !warned {
					fmt.Fprintf(errs, "yolo approve: reading the store failed: %s; still watching, and retrying every %s.\n", termsafe.Visible(err.Error()), approveWatchInterval)
					warned = true
				}
			})
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				cancel()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return approveFailure(firstErr, errs)
	}
	return code
}
