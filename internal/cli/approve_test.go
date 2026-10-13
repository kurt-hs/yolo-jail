package cli

import (
	"bytes"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/ghbroker"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func approvalFixture(t *testing.T) (*brokerstore.Store, brokerstore.Request) {
	t.Helper()
	s := &brokerstore.Store{Dir: t.TempDir(), Revalidate: func(q brokerstore.Request) (brokerstore.Policy, error) { return q.Policy, nil }}
	r, err := s.File(brokerstore.Request{Jail: "j", Start: "s", Workspace: "/w\x1b[2J", Argv: []string{"pr", "merge", "1"}, Repo: "o/r", StdinDigest: "stdin", Set: "read-write", Policy: brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 25}})
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}
func TestApproveOperatorPaths(t *testing.T) {
	s, r := approvalFixture(t)
	var out, errs bytes.Buffer
	if rc := approveMain(nil, false, s, &out, &errs); rc != 0 || !strings.Contains(out.String(), r.ID) || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("list %d %q %q", rc, out.String(), errs.String())
	}
	out.Reset()
	if rc := approveMain([]string{r.ID, "--for", "2h"}, false, s, &out, &errs); rc == 0 {
		t.Fatal("exceeded ceiling")
	}
	if rc := approveMain([]string{r.ID}, false, s, &out, &errs); rc != 0 || !strings.Contains(out.String(), "expires") {
		t.Fatalf("allow %d %q", rc, errs.String())
	}
	_, gs, _ := s.List()
	if len(gs) != 1 {
		t.Fatal(gs)
	}
	if rc := approveMain([]string{"--revoke", gs[0].ID}, false, s, &out, &errs); rc != 0 {
		t.Fatal(errs.String())
	}
	if _, ok, _ := s.Consume("j", "s", "read-write", r.Digest, s.Revalidate); ok {
		t.Fatal("revoked grant consumed")
	}
	for _, args := range [][]string{{r.ID, "--deny", "--for", "1m"}, {"--for", "1m"}, {"--revoke", "all", r.ID}, {"--for", "0s", r.ID}, {"--nonsense"}} {
		if rc := approveMain(args, false, s, &out, &errs); rc == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestApproveDenyAndJailRefusal(t *testing.T) {
	s, r := approvalFixture(t)
	var out, errs bytes.Buffer
	if rc := approveMain([]string{r.ID, "--deny"}, false, s, &out, &errs); rc != 0 {
		t.Fatal(errs.String())
	}
	rs, gs, _ := s.List()
	if rs[0].State != "denied" || len(gs) != 0 {
		t.Fatal(rs, gs)
	}
	out.Reset()
	errs.Reset()
	if rc := approveMain(nil, true, s, &out, &errs); rc != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "host-only") {
		t.Fatal(rc, out.String(), errs.String())
	}
}
func TestApproveThroughRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	s := &brokerstore.Store{Dir: paths.BrokerSourceDir("github")}
	r, err := s.File(brokerstore.Request{Jail: "j", Start: "s", Workspace: "/w", Argv: []string{"pr", "merge"}, Repo: "o/r", StdinDigest: "stdin", Set: "read-write", Policy: brokerstore.Policy{Window: time.Minute, MaxWindow: time.Hour, Uses: 2}})
	if err != nil {
		t.Fatal(err)
	}
	handler, ok := registry["approve"]
	if !ok {
		t.Fatal("approve not dispatched")
	}
	if rc := handler([]string{"approve", r.ID, "--deny"}); rc != 0 {
		t.Fatal(rc)
	}
	rs, _, err := s.List()
	if err != nil || rs[0].State != "denied" {
		t.Fatal(rs, err)
	}
}

func TestApproveRegistryUsesCurrentHostAuthorityAndRequiredAudit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_USER_LAYER", "")
	sf := brokerscope.File{Source: "github", LaunchID: "registry", PID: os.Getpid(), Container: "registry-jail", Workspace: "/fixture", Repos: []string{"o/r"}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	a, err := ghbroker.BindAuthority(path, sf, "registry-start")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	d, policy, err := a.Classify([]string{"pr", "comment", "1", "--body", "hi"}, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	s := &brokerstore.Store{Dir: paths.BrokerSourceDir("github"), Revalidate: a.Revalidate}
	q, err := s.File(brokerstore.Request{Jail: paths.JailShortHash(sf.Container), Start: "registry-start", Workspace: sf.Workspace, LaunchID: sf.LaunchID, Argv: d.Argv, Repo: "o/r", Set: d.Set, Policy: policy, StdinDigest: "stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if code := registry["approve"]([]string{"approve", q.ID}); code != 0 {
		t.Fatal(code)
	}
	_, gs, err := s.List()
	if err != nil || len(gs) != 1 {
		t.Fatal(gs, err)
	}
	audit, err := os.ReadFile(paths.BrokerAuditLog())
	if err != nil || !bytes.Contains(audit, []byte(`"event":"decision"`)) {
		t.Fatal(string(audit), err)
	}
	// A second filed command cannot be approved when its actual broker lease ended,
	// even if both recorded PIDs are still live (PID reuse cannot grant authority).
	d, policy, err = a.Classify([]string{"pr", "comment", "2", "--body", "hi"}, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	q, err = s.File(brokerstore.Request{Jail: paths.JailShortHash(sf.Container), Start: "registry-start", Workspace: sf.Workspace, LaunchID: sf.LaunchID, Argv: d.Argv, Repo: "o/r", Set: d.Set, Policy: policy, StdinDigest: "stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if code := registry["approve"]([]string{"approve", q.ID}); code == 0 {
		t.Fatal("approved dead lifetime")
	}
}
