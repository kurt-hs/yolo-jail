package brokercore

import (
	"fmt"
	"io"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
)

// Lifetimes is the set of jails one broker process has served, so that when the process
// stops it can refuse every request those jails left pending under its start id. Its zero
// value is ready.
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

// LifetimeOwner is the broker whose lifetime is ending: the store EndAll refuses requests in,
// the start id they were filed under, and where it reports a teardown it could not finish.
// EndAll takes it per call, so it reads the broker's fields as they are then.
type LifetimeOwner struct {
	Name  string // the daemon's name, which prefixes its log lines
	Start string
	Store *brokerstore.Store
	Log   io.Writer
}

// EndAll refuses every request still pending from a jail served by this broker process, since
// the call waiting on it is gone (brokerstore.Store.End). It ends no grant: a grant is held by
// its workspace and outlives the broker (boundary-broker.md BB-D99). A jail whose requests the
// store could not refuse stays tracked, so a later call retries it.
func (l *Lifetimes) EndAll(o LifetimeOwner) {
	l.Mu.Lock()
	jails := make([]string, 0, len(l.Jails))
	for jail := range l.Jails {
		jails = append(jails, jail)
	}
	l.Mu.Unlock()
	for _, jail := range jails {
		if err := o.Store.End(jail, o.Start); err != nil {
			fmt.Fprintf(o.Log, "%s: broker lifetime teardown incomplete: %v; retry stopping the broker on the host\n", o.Name, err)
			continue
		}
		l.Mu.Lock()
		delete(l.Jails, jail)
		l.Mu.Unlock()
	}
}
