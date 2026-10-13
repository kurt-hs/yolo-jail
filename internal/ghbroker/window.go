package ghbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// RequiredDelivery is deliberately not provided by production yet. Ready means
// both request delivery (including the no-notifier launch notice) and later answer
// delivery are available. OQ-BB10 and yolo notify are not waived by manual discovery.
type RequiredDelivery interface {
	Ready() bool
	Request(context.Context, brokerstore.Request) error
}

func StoreAudit(log *brokeraudit.Log) func(context.Context, string, brokerstore.Request, brokerstore.Grant) error {
	return func(ctx context.Context, kind string, q brokerstore.Request, g brokerstore.Grant) error {
		outcome := "pending"
		if kind == "decision" {
			outcome = "denied"
			if g.ID != "" {
				outcome = "allowed"
			}
		}
		if kind == "grant-used" {
			outcome = "spent-before-dispatch"
		}
		return log.RequiredContext(ctx, brokeraudit.Event{Event: kind, Service: Source, Jail: q.Jail, Workspace: q.Workspace, Argv: q.Argv, Repo: q.Repo, Set: q.Set, Request: q.ID, Grant: g.ID, Outcome: outcome})
	}
}
func (b *Broker) window(ctx context.Context, req Request, jail string, d Decision, stdout, stderr func([]byte)) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if b.delivery == nil || !b.delivery.Ready() {
		return ExitNoPerm, "required request notice and answer delivery are unavailable; ask the user to run the command on the host"
	}
	if b.authority == nil || b.store == nil || b.store.Audit == nil || (b.runner == nil && b.execute == nil) {
		return ExitUnavailable, "host authority, audit or executor unavailable; restart the host broker"
	}
	d, p, err := b.authority.Classify(req.Argv, req.Repo)
	if err != nil {
		return ExitNoPerm, err.Error()
	}
	if d.Outcome == OutcomeStanding {
		err := brokerlock.WithTransactionContext(ctx, b.store.Dir, func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if jail != paths.JailShortHash(b.authority.scope.Container) || b.startID != b.authority.start {
				return errors.New("standing holder mismatch")
			}
			current, _, err := b.authority.Classify(req.Argv, req.Repo)
			if err != nil {
				return err
			}
			if current.Outcome != OutcomeStanding || current.Set != d.Set {
				return errors.New("standing policy changed; retry")
			}
			return b.audit.RequiredContext(ctx, brokeraudit.Event{Event: "standing-authorized", Service: Source, Jail: jail, Workspace: b.workspace, Argv: current.Argv, Repo: strings.Join(current.Repos, ","), Set: current.Set, Outcome: "authorized-before-dispatch"})
		})
		if err != nil {
			return ExitUnavailable, "standing authorization failed: " + err.Error() + "; repair the host policy/audit and retry"
		}
		if ctx.Err() != nil {
			return ExitTempFail, "caller disconnected; nothing ran; retry if still wanted"
		}
		return b.dispatch(ctx, req, d, stdout, stderr), ""
	}
	sum := sha256.Sum256(req.Stdin)
	q := brokerstore.Request{Jail: jail, Start: b.startID, Workspace: b.workspace, LaunchID: b.authority.scope.LaunchID, Argv: d.Argv, Repo: strings.Join(d.Repos, ","), Set: d.Set, Policy: p, StdinDigest: hex.EncodeToString(sum[:])}
	q.Digest = brokerstore.CommandDigest(q)
	gate := func(parent brokerstore.Request) (brokerstore.Policy, error) {
		if err := ctx.Err(); err != nil {
			return brokerstore.Policy{}, err
		}
		if _, err := b.authority.Revalidate(parent); err != nil {
			return brokerstore.Policy{}, err
		}
		return b.authority.Revalidate(q) // actual argv/repo, not only a set grant's parent
	}
	run := func() (bool, int, string) {
		if ctx.Err() != nil {
			return true, ExitTempFail, "caller disconnected or broker stopped; nothing ran; use `yolo gh status <request>` before retrying"
		}
		_, ok, err := b.store.ConsumeContext(ctx, jail, b.startID, d.Set, q.Digest, gate)
		if err != nil {
			return true, ExitUnavailable, "authorization or required audit failed: " + err.Error() + "; retry after repairing the host authority"
		}
		if !ok {
			return false, 0, ""
		}
		if ctx.Err() != nil {
			return true, ExitTempFail, "caller left after authorization; use was spent, nothing ran; retry if still wanted"
		}
		code := b.dispatch(ctx, req, d, stdout, stderr)
		if code == ghExitAuth {
			return true, ExitUnavailable, noHostLogin
		}
		return true, code, ""
	}
	if ok, code, msg := run(); ok {
		return code, msg
	}
	q, err = b.store.FileContext(ctx, q)
	if err != nil {
		return ExitUnavailable, "request filing failed: " + err.Error() + "; repair the host policy/audit and retry"
	}
	if err = b.delivery.Request(ctx, q); err != nil {
		return ExitTempFail, fmt.Sprintf("%s remains pending; delivery failed; run `yolo approve %s` on the host", q.ID, q.ID)
	}
	status, err := b.waitStatus(ctx, jail, q.ID)
	if err != nil {
		return ExitTempFail, fmt.Sprintf("%s remains pending; nothing ran; check `yolo gh status %s` and re-run only if still wanted", q.ID, q.ID)
	}
	if status.State != "allowed" {
		return ExitNoPerm, fmt.Sprintf("%s is %s; nothing ran; ask the host user before retrying", q.ID, status.State)
	}
	if ok, code, msg := run(); ok {
		return code, msg
	}
	return ExitNoPerm, "no live grant covers this command; refile and approve it on the host"
}
func (b *Broker) waitStatus(ctx context.Context, jail, id string) (brokerstore.Request, error) {
	limit := 30 * time.Second
	if b.waitLimit > 0 && b.waitLimit < limit {
		limit = b.waitLimit
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return brokerstore.Request{}, err
		}
		q, err := b.store.StatusContext(ctx, jail, b.startID, id)
		if err != nil {
			return q, err
		}
		if q.State != "pending" {
			return q, nil
		}
		select {
		case <-ctx.Done():
			return q, ctx.Err()
		case <-tick.C:
		}
	}
}
func (b *Broker) requestStatus(ctx context.Context, req Request, jail string, stdout, stderr func([]byte)) int {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if b.store == nil || b.authority == nil {
		stderr([]byte("github-broker: status unavailable; restart the broker on the host\n"))
		return ExitUnavailable
	}
	if _, err := b.authority.live(); err != nil {
		stderr([]byte("github-broker: " + err.Error() + "; relaunch the jail\n"))
		return ExitNoPerm
	}
	var q brokerstore.Request
	var err error
	if req.Operation == "wait" {
		q, err = b.waitStatus(ctx, jail, req.RequestID)
	} else {
		q, err = b.store.StatusContext(ctx, jail, b.startID, req.RequestID)
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return ExitTempFail
		}
		stderr([]byte("github-broker: request not found for this jail; run `yolo approve` on the host\n"))
		return ExitNoPerm
	}
	stdout([]byte(q.ID + " " + q.State + "\n"))
	switch q.State {
	case "allowed":
		return 0
	case "pending":
		return ExitTempFail
	default:
		return ExitNoPerm
	}
}

func (b *Broker) dispatch(ctx context.Context, req Request, d Decision, stdout, stderr func([]byte)) int {
	var stdin []byte
	if d.ReadsStdin {
		stdin = req.Stdin
		if stdin == nil {
			stdin = []byte{}
		}
	}
	if b.execute != nil {
		return b.execute(ctx, d.Argv, stdin, stdout, stderr).Exit
	}
	return b.runner.Run(d.Argv, stdin, stdout, stderr).Exit
}
