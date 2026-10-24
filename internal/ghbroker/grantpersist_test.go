package ghbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
// windowFixture set). container names the jail; workspace the host workspace; repos the
// approved scope, o/r when none is given.
func relaunchBroker(t *testing.T, launchID, container, workspace string, repos ...string) (*Broker, *atomic.Int32) {
	if len(repos) == 0 {
		repos = []string{"o/r"}
	}
	t.Helper()
	sf := brokerscope.File{Source: Source, LaunchID: launchID, PID: os.Getpid(), Container: container, Workspace: workspace, Repos: repos}
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
	b := &Broker{authority: a, scope: NewScope(repos), workspace: workspace, startID: start,
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

// callOn is one `gh pr comment` on repo through b, as its own jail.
func callOn(b *Broker, repo string) int {
	req := writeReq()
	req.Repo = repo
	return b.ServeContext(context.Background(), req, paths.JailShortHash(b.authority.scope.Container), func([]byte) {}, func([]byte) {})
}

// NARROWING STILL INVALIDATES A HELD GRANT (boundary-broker.md BB-D99(c)). A set grant approved
// for a request on o/r covers o/other too while both are in scope; once o/r leaves the scope,
// the grant covers nothing, in the broker that holds it and across a relaunch, because the
// gate revalidates the grant's own request under the live launch, not only the new command.
func TestASetGrantEndsCoverageWhenItsOwnRepositoryLeavesTheScope(t *testing.T) {
	for _, mode := range []string{"same broker", "relaunch"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", resolvedDir(t))
			t.Setenv("YOLO_USER_LAYER", "")
			first, ran := relaunchBroker(t, "first", "fixture-container", "/fixture", "o/r", "o/other")
			if got := callOn(first, "o/r"); got != ExitTempFail {
				t.Fatalf("first call %d, want pending", got)
			}
			rs, _, _ := first.store.List()
			if _, err := first.store.Decide(rs[0].ID, "pending", brokerstore.Decision{For: 15 * time.Minute}); err != nil {
				t.Fatal(err)
			}
			// Control: while o/r is in scope, the set grant covers a write on o/other.
			if got := callOn(first, "o/other"); got != 0 || ran.Load() != 1 {
				t.Fatalf("control: exit %d, ran %d; the set grant should cover o/other", got, ran.Load())
			}
			next, nextRan := first, ran
			if mode == "same broker" {
				if err := brokerscope.ReplaceRepos(first.authority.path, first.authority.scope, []string{"o/other"}); err != nil {
					t.Fatal(err)
				}
			} else {
				first.endLifetimes()
				if err := first.authority.Close(); err != nil {
					t.Fatal(err)
				}
				next, nextRan = relaunchBroker(t, "second", "fixture-container", "/fixture", "o/other")
			}
			before := nextRan.Load()
			if got := callOn(next, "o/other"); got == 0 || nextRan.Load() != before {
				t.Fatalf("a set grant whose own repository left the scope still ran a write: exit %d, ran %d", got, nextRan.Load()-before)
			}
		})
	}
}

// A SYMLINKED WORKSPACE IS ONE HOLDER (boundary-broker.md BB-D99). A grant approved for a launch
// through a symlink to the workspace serves a relaunch through the real path, and the other way
// round, as the jail id already does (runtime.ResolveWorkspace).
func TestAGrantServesTheWorkspaceUnderEitherSpelling(t *testing.T) {
	t.Setenv("HOME", resolvedDir(t))
	t.Setenv("YOLO_USER_LAYER", "")
	real := resolvedDir(t)
	link := filepath.Join(resolvedDir(t), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	first, _ := relaunchBroker(t, "via-link", "fixture-container", link)
	if got := callOn(first, "o/r"); got != ExitTempFail {
		t.Fatalf("first call %d, want pending", got)
	}
	rs, _, _ := first.store.List()
	g, err := first.store.Decide(rs[0].ID, "pending", brokerstore.Decision{For: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if g.Workspace != real {
		t.Fatalf("grant holder %q, want the resolved workspace %q", g.Workspace, real)
	}
	first.endLifetimes()
	if err := first.authority.Close(); err != nil {
		t.Fatal(err)
	}
	next, ran := relaunchBroker(t, "via-real", "fixture-container", real)
	if got := callOn(next, "o/r"); got != 0 || ran.Load() != 1 {
		t.Fatalf("relaunch through the real path: exit %d, ran %d", got, ran.Load())
	}
}
