package ghbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// relaunchBroker is a fresh broker for a new launch, as a jail restart or relaunch makes one:
// its own launch id, scope file, random start and lease, over the same host store (the HOME
// windowFixture set). container names the jail; workspace the host workspace.
func relaunchBroker(t *testing.T, launchID, container, workspace string) (*Broker, *atomic.Int32) {
	t.Helper()
	sf := brokerscope.File{Source: Source, LaunchID: launchID, PID: os.Getpid(), Container: container, Workspace: workspace, Repos: []string{"o/r"}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	start, err := brokerstore.NewStartID()
	if err != nil {
		t.Fatal(err)
	}
	a, err := BindAuthority(path, sf, start)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	count := new(atomic.Int32)
	log := brokeraudit.Open(paths.BrokerAuditLog(), nil)
	b := &Broker{authority: a, scope: NewScope([]string{"o/r"}), workspace: workspace, startID: start,
		store: &brokerstore.Store{Dir: paths.BrokerSourceDir(Source), Revalidate: a.Revalidate, Audit: StoreAudit(log)},
		audit: log, log: io.Discard, now: time.Now, waitLimit: 20 * time.Millisecond}
	b.execute = func(context.Context, []string, []byte, func([]byte), func([]byte)) RunResult {
		count.Add(1)
		return RunResult{Exit: 0}
	}
	b.delivery = &fakeDelivery{ready: true}
	return b, count
}

// A TIMED GRANT OUTLIVES ITS BROKER (boundary-broker.md BB-D99). A grant approved for one
// launch's request is still honored after that broker stops (its teardown runs) and a new
// broker starts for a relaunch of the same workspace, and by a second jail of that workspace;
// a broker of another workspace never sees it.
func TestATimedGrantOutlivesItsBrokerForItsWorkspaceOnly(t *testing.T) {
	first, ran := windowFixture(t)
	jail := paths.JailShortHash(first.authority.scope.Container)
	first.trackJail(jail)
	if got := callWindow(first, context.Background()); got != ExitTempFail {
		t.Fatalf("first call %d, want pending", got)
	}
	rs, _, _ := first.store.List()
	g, err := first.store.Decide(rs[0].ID, "pending", brokerstore.Decision{For: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	// The broker stops: its teardown runs and its lease is released.
	first.endLifetimes()
	if err := first.authority.Close(); err != nil {
		t.Fatal(err)
	}
	if ran.Load() != 0 {
		t.Fatal("ran before the relaunch")
	}

	// Another workspace's broker files its own request; the grant covers nothing there.
	other, otherRan := relaunchBroker(t, "elsewhere", "other-container", "/elsewhere")
	if got := callWindow(other, context.Background()); got != ExitTempFail || otherRan.Load() != 0 {
		t.Fatalf("another workspace used the grant: exit %d, ran %d", got, otherRan.Load())
	}

	// A relaunch of the same jail, then a second jail of the same workspace.
	for _, c := range []struct{ launch, container string }{{"relaunch", "fixture-container"}, {"second", "second-container"}} {
		next, nextRan := relaunchBroker(t, c.launch, c.container, first.workspace)
		if got := callWindow(next, context.Background()); got != 0 || nextRan.Load() != 1 {
			t.Fatalf("%s: exit %d, ran %d; want the grant from the stopped broker honored", c.launch, got, nextRan.Load())
		}
	}
	_, gs, err := first.store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range gs {
		if h.ID != g.ID {
			continue
		}
		if !h.Ended.IsZero() || h.Uses != g.Uses-2 || h.UsedBy != paths.JailShortHash("second-container") {
			t.Fatalf("grant after two uses from two launches: %+v", h)
		}
		// The audit names the jail that spent each use, not only the one that filed.
		audit, err := os.ReadFile(paths.BrokerAuditLog())
		if err != nil {
			t.Fatal(err)
		}
		usedBy := map[string]bool{}
		for _, line := range bytes.Split(audit, []byte("\n")) {
			var ev brokeraudit.Event
			if json.Unmarshal(line, &ev) == nil && ev.Event == "grant-used" && ev.Grant == g.ID {
				usedBy[ev.Jail] = true
			}
		}
		if !usedBy[jail] || !usedBy[paths.JailShortHash("second-container")] {
			t.Fatalf("grant-used audit events name jails %v, want %s and the second jail's", usedBy, jail)
		}
		return
	}
	t.Fatalf("grant %s missing: %+v", g.ID, gs)
}
