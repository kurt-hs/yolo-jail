package gitbroker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// refusedPushCommands runs a real git push of HEAD to main through the fixture's broker with
// no grant covering it, and returns the ref commands the broker read from it, as its audit
// line records them.
func refusedPushCommands(t *testing.T, f *fixture) []gitproto.Command {
	t.Helper()
	f.commit("one")
	before := f.upstreamRef("refs/heads/main")
	if out, err := f.git("push", "origin", "HEAD:main"); err == nil {
		t.Fatalf("an unapproved push to main went through:\n%s", out)
	}
	if f.upstreamRef("refs/heads/main") != before {
		t.Fatal("main moved")
	}
	var cmds []gitproto.Command
	for _, r := range lastPush(t).Refs {
		cmds = append(cmds, gitproto.Command{Old: r.Old, New: r.New, Ref: r.Ref})
	}
	if len(cmds) != 1 || cmds[0].Ref != "refs/heads/main" || cmds[0].New != mustGit(t, f.ws, "rev-parse", "HEAD") {
		t.Fatalf("commands %+v", cmds)
	}
	return cmds
}

// pendingID is the request id a refusal reason names.
func pendingID(t *testing.T, reason string) string {
	t.Helper()
	id, ok := strings.CutPrefix(reason, "needs approval: ")
	if !ok || !strings.HasPrefix(id, "r-") || len(id) < 34 {
		t.Fatalf("refusal %q names no request", reason)
	}
	return id[:34]
}

func TestAskForPushSpendsExactGrantOnce(t *testing.T) {
	f := newFixture(t, "")
	f.broker.waitLimit = 50 * time.Millisecond
	cmds := refusedPushCommands(t, f)
	repo, _ := gitproto.Identity(origin)
	ctx := context.Background()

	ev := &brokeraudit.Event{}
	_, reason, ok := f.broker.askForPush(ctx, repo, cmds, nil, []string{"refs/heads/main"}, ev)
	if ok {
		t.Fatal("a push nobody approved was allowed")
	}
	id := pendingID(t, reason.reason)
	if ev.Request != id || ev.CommandDigest == "" {
		t.Fatalf("audit event %+v", ev)
	}
	if !strings.Contains(f.log.String(), "request "+id+" is pending; run `yolo approve "+id+"` on the host") {
		t.Fatalf("the broker log has no notice for %s:\n%s", id, f.log.String())
	}
	if strings.Contains(f.log.String(), "refs/heads/main") || strings.Contains(f.log.String(), f.ws) {
		t.Fatalf("the shared broker log names a ref or the workspace:\n%s", f.log.String())
	}
	reqs, _, err := ApprovalStore().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0].ID != id || len(reqs[0].NeedsApproval) != 1 || reqs[0].NeedsApproval[0] != "refs/heads/main" {
		t.Fatalf("filed requests %+v", reqs)
	}

	g, err := ApprovalStore().Decide(id, "pending", brokerstore.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	ev = &brokeraudit.Event{}
	spent, reason, ok := f.broker.askForPush(ctx, repo, cmds, nil, []string{"refs/heads/main"}, ev)
	if !ok || spent.ID != g.ID || spent.Uses != 0 || ev.Grant != g.ID || ev.Request != id {
		t.Fatalf("the approved push: ok %v, reason %q, grant %+v, event %+v", ok, reason, spent, ev)
	}

	// Spent: the same push again files a new request.
	_, reason, ok = f.broker.askForPush(ctx, repo, cmds, nil, []string{"refs/heads/main"}, &brokeraudit.Event{})
	if ok || pendingID(t, reason.reason) == id {
		t.Fatalf("a spent Allow once covered a second push: ok %v, %q", ok, reason)
	}

	// A different push (another new id) is not covered by an exact grant on the first.
	other := []gitproto.Command{{Old: cmds[0].Old, New: cmds[0].Old, Ref: cmds[0].Ref}}
	g2id := pendingID(t, reason.reason)
	if _, err := ApprovalStore().Decide(g2id, "pending", brokerstore.Decision{}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := f.broker.askForPush(ctx, repo, other, nil, []string{"refs/heads/main"}, &brokeraudit.Event{}); ok {
		t.Fatal("an Allow once covered a push it did not name")
	}
}

func TestAskForPushWaitsForTheAnswer(t *testing.T) {
	f := newFixture(t, "")
	f.broker.waitLimit = 10 * time.Second
	cmds := refusedPushCommands(t, f)
	repo, _ := gitproto.Identity(origin)
	for _, deny := range []bool{false, true} {
		decided := make(chan error, 1)
		go func() {
			for {
				reqs, _, err := ApprovalStore().List()
				if err != nil {
					decided <- err
					return
				}
				for _, q := range reqs {
					if q.State == "pending" {
						_, err := ApprovalStore().Decide(q.ID, "pending", brokerstore.Decision{Deny: deny})
						decided <- err
						return
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
		start := time.Now()
		push := cmds
		if deny {
			push = []gitproto.Command{{Old: cmds[0].Old, New: cmds[0].Old, Ref: cmds[0].Ref}}
		}
		_, reason, ok := f.broker.askForPush(context.Background(), repo, push, nil, []string{"refs/heads/main"}, &brokeraudit.Event{})
		if err := <-decided; err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("the answer took %v to reach the push", time.Since(start))
		}
		if ok == deny {
			t.Fatalf("deny %v: ok %v, %q", deny, ok, reason)
		}
		if deny && !strings.Contains(reason.reason, "denied") {
			t.Fatalf("a denied push says %q", reason)
		}
	}
}
