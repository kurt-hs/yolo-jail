package gitbroker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// approval.go is a push asking for approval (docs/design/git-broker.md §5, GB-D16 to GB-D23):
// a push no grant or working-set entry covers whole, whose every ref could be approved, spends
// an Allow once the host already gave for exactly that push, or files one request for it,
// writes its notice to the broker's background log, and waits for the answer a bounded time
// (boundary-broker.md §3.2). Allowed in that time, the grant is spent and the push goes on;
// still pending or denied, every ref the push needed approval for is refused naming the
// request, and the agent pushes again once the host allowed it.

// pushWait is how long a push waits for its request's answer before it is refused as pending.
const pushWait = 30 * time.Second

// reasonNeedsApprovalPrefix starts the refusal of a ref whose push filed a request: then the
// request's id, and, once the host answered, how.
const reasonNeedsApprovalPrefix = "needs approval: "

// reasonGrantGone ends the refusal of a push allowed during its wait whose grant then covered
// nothing (revoked, or the scope changed before it was spent).
const reasonGrantGone = ", whose grant no longer covers this push"

// requestIDLen is the length of a request id, "r-" and 32 hex digits.
const requestIDLen = 34

// revalidate is this broker's push revalidation, bound to its launch and lifetime.
func (b *Broker) revalidate(q brokerstore.Request) (brokerstore.Policy, error) {
	return revalidatePush(b.scopeFile, b.bound, b.start, q)
}

// askForPush asks for approval of the push of cmds with the push options opts to repo, in
// which the refs needs are those
// nothing else covered. It returns the grant it spent and true when an Allow once covers the
// push: the use is spent and audited under the store's lock, which is released before the push
// is forwarded (GB-D19). Otherwise it returns the reason each ref in needs is refused with.
func (b *Broker) askForPush(ctx context.Context, repo string, cmds []gitproto.Command, opts, needs []string, ev *brokeraudit.Event) (brokerstore.Grant, string, bool) {
	q := b.pushRequest(repo, cmds, opts)
	q.NeedsApproval = needs
	ev.CommandDigest, ev.StdinDigest = q.Digest, q.StdinDigest
	// A grant is held by its workspace (BB-D99), so its request may come from a jail or broker
	// lifetime that is gone: the parent must be this workspace's, and both it, held by this
	// launch, and this push must pass the live scope now.
	gate := func(parent brokerstore.Request) (brokerstore.Policy, error) {
		if err := ctx.Err(); err != nil {
			return brokerstore.Policy{}, err
		}
		if !brokerstore.SameWorkspace(parent.Workspace, q.Workspace) {
			return brokerstore.Policy{}, errors.New("the grant belongs to another workspace")
		}
		held := parent
		held.Jail, held.Start, held.LaunchID, held.Workspace = q.Jail, q.Start, q.LaunchID, q.Workspace
		if _, err := b.revalidate(held); err != nil {
			return brokerstore.Policy{}, fmt.Errorf("the grant's own request no longer passes this launch's scope: %w", err)
		}
		return b.revalidate(q)
	}
	spend := func() (brokerstore.Grant, bool, string) {
		g, ok, err := b.store.ConsumeExactDispatchContext(ctx, q.Workspace, q.Jail, setPush, q.Digest, gate, nil)
		if err != nil {
			return g, false, "needs approval, and the host could not check its grants (" + err.Error() + ")"
		}
		if ok {
			ev.Request, ev.Grant = g.Request, g.ID
		}
		return g, ok, ""
	}
	if g, ok, why := spend(); ok || why != "" {
		return g, why, ok
	}
	filed, err := b.store.FileContext(ctx, q)
	if err != nil {
		var limit *brokerstore.PendingLimitError
		var denial *brokerstore.DenialCooldownError
		switch {
		case errors.As(err, &limit):
			return brokerstore.Grant{}, "needs approval, and this jail already has 3 requests waiting (" + limit.Error() + ")", false
		case errors.As(err, &denial):
			return brokerstore.Grant{}, "needs approval, and the host denied this same push (" + denial.Error() + ")", false
		}
		return brokerstore.Grant{}, "needs approval, and the request could not be filed (" + err.Error() + ")", false
	}
	ev.Request = filed.ID
	// No notifier: the background log only (GB-D10, BB-D98). It names the request and nothing
	// else, since every jail on the machine shares the log.
	fmt.Fprintf(b.log, "%s: request %s is pending; run `yolo approve %s` on the host\n", Name, filed.ID, filed.ID)
	status, err := b.waitStatus(ctx, q.Jail, filed.ID)
	switch {
	case err != nil || status.State == "pending":
		return brokerstore.Grant{}, reasonNeedsApprovalPrefix + filed.ID, false
	case status.State != "allowed":
		return brokerstore.Grant{}, reasonNeedsApprovalPrefix + filed.ID + ", " + status.State + " on the host", false
	}
	if g, ok, why := spend(); ok || why != "" {
		return g, why, ok
	}
	return brokerstore.Grant{}, reasonNeedsApprovalPrefix + filed.ID + reasonGrantGone, false
}

// waitStatus polls request id until it is no longer pending, for at most the push wait.
func (b *Broker) waitStatus(ctx context.Context, jail, id string) (brokerstore.Request, error) {
	limit := pushWait
	if b.waitLimit > 0 && b.waitLimit < limit {
		limit = b.waitLimit
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		q, err := b.store.StatusContext(ctx, jail, b.start, id)
		if err != nil || q.State != "pending" {
			return q, err
		}
		select {
		case <-ctx.Done():
			return q, ctx.Err()
		case <-tick.C:
		}
	}
}
