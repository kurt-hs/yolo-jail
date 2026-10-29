package gitbroker

import (
	"context"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

const (
	oldID = "1111111111111111111111111111111111111111"
	newID = "2222222222222222222222222222222222222222"
)

// pushRequest is the request the fixture's broker files for cmds, as askForPush builds it.
func (f *fixture) pushRequest(cmds ...gitproto.Command) brokerstore.Request {
	f.t.Helper()
	repo, err := gitproto.Identity(origin)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.broker.pushRequest(repo, cmds, nil)
}

func TestApprovalAuthorityAcceptsTheLiveLaunchsPush(t *testing.T) {
	f := newFixture(t, "")
	q := f.pushRequest(gitproto.Command{Old: oldID, New: newID, Ref: "refs/heads/main"})
	if q.Jail != paths.JailShortHash("fixture-container") || q.Set != setPush || len(q.Argv) != 2 ||
		q.Argv[0] != serviceReceivePack || q.Argv[1] != oldID+" "+newID+" refs/heads/main" {
		t.Fatalf("request %+v", q)
	}
	p, err := ApprovalAuthority(q)
	if err != nil {
		t.Fatal(err)
	}
	if p != pushPolicy || p.Uses != 1 || p.Window != p.MaxWindow || p.Session {
		t.Fatalf("policy %+v", p)
	}
}

func TestApprovalAuthorityRefusesRepoLeftScope(t *testing.T) {
	f := newFixture(t, "")
	q := f.pushRequest(gitproto.Command{Old: oldID, New: newID, Ref: "refs/heads/main"})
	if _, err := ApprovalAuthority(q); err != nil {
		t.Fatal(err)
	}
	if err := brokerscope.ReplaceScopeContext(context.Background(), f.scopeFile, f.broker.bound,
		[]string{"git@fixture.test:o/other.git"}, []brokerscope.Grant{readGrant}); err != nil {
		t.Fatal(err)
	}
	_, err := ApprovalAuthority(q)
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("a repository that left the scope: %v", err)
	}
}

func TestApprovalAuthorityRefusesWithdrawnReadGrant(t *testing.T) {
	f := newFixture(t, "")
	q := f.pushRequest(gitproto.Command{Old: oldID, New: newID, Ref: "refs/heads/main"})
	sf, err := brokerscope.ReadFile(f.scopeFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := brokerscope.ReplaceScopeContext(context.Background(), f.scopeFile, f.broker.bound, sf.Repos, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ApprovalAuthority(q); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("a withdrawn read grant: %v", err)
	}
}

func TestApprovalAuthorityRefusesForeignRefsAndHolders(t *testing.T) {
	f := newFixture(t, "")
	for name, mutate := range map[string]func(*brokerstore.Request){
		"a ref outside heads and tags": func(q *brokerstore.Request) { q.Argv[1] = oldID + " " + newID + " refs/notes/x" },
		"another program":              func(q *brokerstore.Request) { q.Argv[0] = serviceUploadPack },
		"no ref command":               func(q *brokerstore.Request) { q.Argv = q.Argv[:1] },
		"a malformed command":          func(q *brokerstore.Request) { q.Argv[1] = "main" },
		"another set":                  func(q *brokerstore.Request) { q.Set = "read-write" },
		"another broker start":         func(q *brokerstore.Request) { q.Start = "0123456789abcdef0123456789abcdef" },
		"another jail":                 func(q *brokerstore.Request) { q.Jail = "deadbeef" },
		"another workspace":            func(q *brokerstore.Request) { q.Workspace = "/elsewhere" },
		"no launch id":                 func(q *brokerstore.Request) { q.LaunchID = "" },
		"a launch id naming a path":    func(q *brokerstore.Request) { q.LaunchID = "../x" },
	} {
		q := f.pushRequest(gitproto.Command{Old: oldID, New: newID, Ref: "refs/heads/main"})
		mutate(&q)
		if _, err := ApprovalAuthority(q); err == nil {
			t.Errorf("%s: approved", name)
		}
	}
}

// The CLI's store, ApprovalStore, decides a request the broker filed: Allow once is one use
// of that exact push for 15 minutes, held by the workspace.
func TestApprovalStoreDecidesAFiledPush(t *testing.T) {
	f := newFixture(t, "")
	q := f.pushRequest(gitproto.Command{Old: oldID, New: newID, Ref: "refs/tags/v1"})
	s := ApprovalStore()
	filed, err := s.File(q)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.Decide(filed.ID, "pending", brokerstore.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Uses != 1 || g.Digest != filed.Digest || g.Set != setPush || g.Expires.Sub(f.broker.now()) > pushPolicy.Window {
		t.Fatalf("grant %+v", g)
	}
}
