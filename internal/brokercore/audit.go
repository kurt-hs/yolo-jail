package brokercore

import (
	"context"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
)

// StoreAudit is a request store's required audit hook for a source: each request filed, each
// decision and each grant spent becomes an audit event with `service` set to the source,
// written before the store's transaction commits.
func StoreAudit(log *brokeraudit.Log, service string) func(context.Context, string, brokerstore.Request, brokerstore.Grant) error {
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
		return log.RequiredContext(ctx, brokeraudit.Event{Event: kind, Service: service, Jail: q.Jail, Workspace: q.Workspace, Argv: q.Argv, Repo: q.Repo, Set: q.Set, Request: q.ID, Grant: g.ID, Outcome: outcome})
	}
}
