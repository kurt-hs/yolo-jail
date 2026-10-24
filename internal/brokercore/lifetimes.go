package brokercore

import (
	"fmt"
	"io"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
)

// Lifetimes is the set of jails one broker process has served, so that when the process
// stops it can end every grant those jails hold under its start id. Its zero value is ready.
type Lifetimes struct {
	Mu    sync.Mutex
	Jails map[string]struct{}
}

// Track records that the broker served jail.
func (l *Lifetimes) Track(jail string) {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	if l.Jails == nil {
		l.Jails = make(map[string]struct{})
	}
	l.Jails[jail] = struct{}{}
}

// LifetimeOwner is the broker whose lifetime is ending: the store EndAll ends grants in, the
// start id they were granted under, where it audits them, and where it reports a teardown it
// could not finish. EndAll takes it per call, so it reads the broker's fields as they are then.
type LifetimeOwner struct {
	Name      string // the daemon's name, which prefixes its log lines
	Source    string // the audit events' service
	Workspace string
	Start     string
	Store     *brokerstore.Store
	Audit     *brokeraudit.Log
	Log       io.Writer
}

// EndAll revokes every grant held by a jail served by this broker process. The random start
// id also prevents an uncleanly terminated process from authorizing a later run. A jail whose
// grants the store could not end stays tracked, so a later call retries it.
func (l *Lifetimes) EndAll(o LifetimeOwner) {
	l.Mu.Lock()
	jails := make([]string, 0, len(l.Jails))
	for jail := range l.Jails {
		jails = append(jails, jail)
	}
	l.Mu.Unlock()
	for _, jail := range jails {
		grants, err := o.Store.End(jail, o.Start)
		if err != nil {
			fmt.Fprintf(o.Log, "%s: broker lifetime teardown incomplete: %v; retry stopping the broker on the host\n", o.Name, err)
		} else {
			l.Mu.Lock()
			delete(l.Jails, jail)
			l.Mu.Unlock()
		}
		for _, g := range grants {
			if g.Jail == jail && g.Start == o.Start {
				o.Audit.Append(brokeraudit.Event{Event: "grant-ended", Service: o.Source, Jail: jail,
					Workspace: o.Workspace, Set: g.Set, Grant: g.ID, Outcome: "ended",
					Reason: "broker lifetime ended"})
			}
		}
	}
}
