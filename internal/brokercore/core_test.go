package brokercore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// A second source, named "fake", files, is granted, and audits through the same core the
// github source uses, in its own store directory and with its own audit service.
func TestFakeSourceFilesGrantsAndAuditsThroughTheCore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_USER_LAYER", "")
	const source = "fake"
	sf := brokerscope.File{Source: source, LaunchID: "fixture", PID: os.Getpid(),
		Container: "fixture-container", Workspace: "/fixture", Repos: []string{"o/r"}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	if want := paths.BrokerScopeFile(source, "fixture"); path != want {
		t.Fatalf("scope file at %s, want %s", path, want)
	}
	scope, lease, err := BindLease(source, path, sf, "fresh-start")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseLease(source, lease) })
	if _, err := Live(source, path, scope, "fresh-start"); err != nil {
		t.Fatalf("a bound lease is not live: %v", err)
	}
	log := brokeraudit.Open(paths.BrokerAuditLog(), nil)
	store := &brokerstore.Store{Dir: paths.BrokerSourceDir(source), Source: source,
		Revalidate: func(brokerstore.Request) (brokerstore.Policy, error) {
			return brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: 15 * time.Minute, Uses: 1}, nil
		},
		Audit: StoreAudit(log, source)}
	q, err := store.FileContext(context.Background(), brokerstore.Request{
		Jail: paths.JailShortHash(sf.Container), Start: "fresh-start", Workspace: sf.Workspace,
		LaunchID: sf.LaunchID, Argv: []string{"write"}, Repo: "o/r", Set: "fake-write",
		StdinDigest: "none",
		Policy:      brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: 15 * time.Minute, Uses: 1}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Decide(q.ID, "pending", brokerstore.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	if g.ID == "" {
		t.Fatal("allowing the request issued no grant")
	}
	if got := filepath.Dir(filepath.Dir(RunRoot(source))); got != paths.BrokerDir() {
		t.Fatalf("run root %s is not under the broker dir", RunRoot(source))
	}
	events, _, err := brokeraudit.Read(paths.BrokerAuditLog())
	if err != nil {
		t.Fatal(err)
	}
	var filed, allowed bool
	for _, ev := range events {
		if ev.Service != source || ev.Request != q.ID {
			continue
		}
		filed = filed || ev.Event == "request"
		allowed = allowed || (ev.Event == "decision" && ev.Grant == g.ID && ev.Outcome == "allowed")
	}
	if !filed || !allowed {
		t.Fatalf("audit log lacks the fake source's filing (%v) or its allowed decision naming grant %s (%v):\n%+v",
			filed, g.ID, allowed, events)
	}
}
