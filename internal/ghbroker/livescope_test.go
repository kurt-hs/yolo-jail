package ghbroker

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// livescope_test.go pins design §7 of docs/design/live-config-apply.md (LA-D10, LA-D11): a
// bound broker classifies every request against the scope file it re-reads, so a `yolo config
// apply` that rewrites the file's repositories widens or narrows a running broker at its next
// request, and the bound file says the broker takes live scope.

// liveBroker is a production-shaped broker bound to a host authority, with no host gh: a
// standing read the scope admits answers 69 (no gh), one outside it answers 64.
func liveBroker(t *testing.T) *Broker {
	t.Helper()
	a, _ := authorityFixture(t)
	log := brokeraudit.Open(paths.BrokerAuditLog(), nil)
	return &Broker{authority: a, scope: NewScope(a.scope.Repos), workspace: a.scope.Workspace,
		startID: a.start, store: &brokerstore.Store{Dir: paths.BrokerSourceDir(Source), Revalidate: a.Revalidate,
			Audit: StoreAudit(log)}, audit: log, log: io.Discard, now: time.Now}
}

func serveRead(b *Broker, repo string) (int, string) {
	var errOut bytes.Buffer
	code := b.Serve(Request{Argv: []string{"pr", "view", "1", "-R", repo}}, paths.JailShortHash(b.authority.scope.Container),
		func([]byte) {}, func(p []byte) { errOut.Write(p) })
	return code, strings.Join(strings.Fields(errOut.String()), " ")
}

func TestTheBoundScopeFileSaysTheBrokerTakesLiveScope(t *testing.T) {
	a, _ := authorityFixture(t)
	f, err := brokerscope.ReadFile(a.path)
	if err != nil {
		t.Fatal(err)
	}
	if !f.LiveScope {
		t.Fatalf("the bound scope file does not carry live_scope: %+v", f)
	}
}

func TestAWidenedScopeFileAdmitsANewRepositoryAtTheNextRequest(t *testing.T) {
	b := liveBroker(t)
	if code, msg := serveRead(b, "o/new"); code != ExitUsage || !strings.Contains(msg, "outside this jail's repository scope") {
		t.Fatalf("before the widening: code %d, want 64 out of scope: %s", code, msg)
	}
	if err := brokerscope.ReplaceRepos(b.authority.path, b.authority.scope, []string{"o/r", "o/new"}); err != nil {
		t.Fatal(err)
	}
	if code, msg := serveRead(b, "o/new"); code != ExitUnavailable {
		t.Fatalf("after the widening: code %d, want the read admitted (69: no host gh): %s", code, msg)
	}
}

func TestANarrowedScopeFileRefusesARemovedRepositoryAtTheNextRequest(t *testing.T) {
	b := liveBroker(t)
	if code, msg := serveRead(b, "o/r"); code != ExitUnavailable {
		t.Fatalf("before the narrowing: code %d, want the read admitted (69: no host gh): %s", code, msg)
	}
	if err := brokerscope.ReplaceRepos(b.authority.path, b.authority.scope, []string{"o/other"}); err != nil {
		t.Fatal(err)
	}
	code, msg := serveRead(b, "o/r")
	if code != ExitUsage || !strings.Contains(msg, "o/r is outside this jail's repository scope (o/other)") {
		t.Fatalf("after the narrowing: code %d, want 64 naming the live scope: %s", code, msg)
	}
}

// Where the broker takes live scope, the agent asks the user for `yolo config apply` on the
// host, not a restart (design §6; the substance of WW-D5 and WW-D6, that the agent asks the
// human, holds).
func TestTheLiveBrokersOutOfScopeAdviceNamesConfigApply(t *testing.T) {
	b := liveBroker(t)
	code, msg := serveRead(b, "o/new")
	for _, want := range []string{"run `yolo check --no-build`",
		"ask the user to run `yolo config apply` on the host, in this workspace, and approve the repository scope there",
		"Nothing sent through gh adds a repository"} {
		if code != ExitUsage || !strings.Contains(msg, want) {
			t.Errorf("code %d, want 64 saying %q:\n%s", code, want, msg)
		}
	}
	if strings.Contains(msg, "restart") {
		t.Errorf("a live broker's refusal still says to restart:\n%s", msg)
	}
}
