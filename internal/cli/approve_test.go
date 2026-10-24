package cli

import (
	"bytes"
	"context"
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
	if rc := approveMain(context.Background(), nil, false, s, &out, &errs); rc != 0 || !strings.Contains(out.String(), r.ID) || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("list %d %q %q", rc, out.String(), errs.String())
	}
	out.Reset()
	if rc := approveMain(context.Background(), []string{r.ID, "--for", "2h"}, false, s, &out, &errs); rc == 0 {
		t.Fatal("exceeded ceiling")
	}
	if rc := approveMain(context.Background(), []string{r.ID}, false, s, &out, &errs); rc != 0 || !strings.Contains(out.String(), "expires") {
		t.Fatalf("allow %d %q", rc, errs.String())
	}
	_, gs, _ := s.List()
	if len(gs) != 1 {
		t.Fatal(gs)
	}
	if rc := approveMain(context.Background(), []string{"--revoke", gs[0].ID}, false, s, &out, &errs); rc != 0 {
		t.Fatal(errs.String())
	}
	if _, ok, _ := s.Consume("j", "s", "read-write", r.Digest, s.Revalidate); ok {
		t.Fatal("revoked grant consumed")
	}
	for _, args := range [][]string{{r.ID, "--deny", "--for", "1m"}, {"--for", "1m"}, {"--revoke", "all", r.ID}, {"--for", "0s", r.ID}, {"--nonsense"}} {
		if rc := approveMain(context.Background(), args, false, s, &out, &errs); rc == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestApproveDenyAndJailRefusal(t *testing.T) {
	s, r := approvalFixture(t)
	var out, errs bytes.Buffer
	if rc := approveMain(context.Background(), []string{r.ID, "--deny"}, false, s, &out, &errs); rc != 0 {
		t.Fatal(errs.String())
	}
	rs, gs, _ := s.List()
	if rs[0].State != "denied" || len(gs) != 0 {
		t.Fatal(rs, gs)
	}
	out.Reset()
	errs.Reset()
	if rc := approveMain(context.Background(), nil, true, s, &out, &errs); rc != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "host-only") {
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

// A request whose on_request failed says so wherever it is listed: the plain listing, with
// the one line, and the JSON, with the line and the output.
func TestAFailedHookIsListedWithItsRequest(t *testing.T) {
	s, r := approvalFixture(t)
	if err := s.NoteHookFailure(r.ID, "hook exited 127: sh: notify-send: not found", "sh: notify-send: not found\n"); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if rc := approveMain(context.Background(), nil, false, s, &out, &errs); rc != 0 ||
		!strings.Contains(out.String(), "your on_request failed, so you may not have been notified: hook exited 127: sh: notify-send: not found") {
		t.Fatalf("list %d %q %q", rc, out.String(), errs.String())
	}
	out.Reset()
	if rc := approveMain(context.Background(), []string{"--json"}, false, s, &out, &errs); rc != 0 ||
		!strings.Contains(out.String(), `"hook_failure":"hook exited 127: sh: notify-send: not found"`) ||
		!strings.Contains(out.String(), `"hook_output":"sh: notify-send: not found\n"`) {
		t.Fatalf("json %d %q %q", rc, out.String(), errs.String())
	}
}

// --- the working set in `yolo approve` (docs/design/git-broker-plan.md task 9, BB-D86) ---

func workingStore(dir string) *brokerstore.Store {
	return &brokerstore.Store{Dir: dir, Source: "git"}
}

func addEntry(t *testing.T, s *brokerstore.Store, ws, ref string) brokerstore.WorkingEntry {
	t.Helper()
	e, _, err := s.AddWorking(brokerstore.WorkingEntry{Workspace: ws, Repo: "github.com/o/r", Kind: brokerstore.WorkingBranch,
		Resource: ref, By: "create-branch"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestApproveListsWorkingSet(t *testing.T) {
	gh, _ := approvalFixture(t)
	ws := workingStore(t.TempDir())
	e := addEntry(t, ws, "/home/u/code/app", "refs/heads/agent/x")
	stores := []*brokerstore.Store{gh, ws}
	var out, errs bytes.Buffer
	if rc := approveAll(context.Background(), nil, false, stores, &out, &errs); rc != 0 {
		t.Fatalf("list %d %q", rc, errs.String())
	}
	t.Logf("listing:\n%s", out.String())
	var line string
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(l, e.ID) {
			line = l
		}
	}
	for _, want := range []string{"git", "/home/u/code/app", "github.com/o/r", "refs/heads/agent/x", "create-branch"} {
		if !strings.Contains(line, want) {
			t.Fatalf("working-set line %q lacks %q in:\n%s", line, want, out.String())
		}
	}
	if !strings.Contains(out.String(), "w-entry") {
		t.Fatalf("the listing does not say how to revoke an entry:\n%s", out.String())
	}
	out.Reset()
	if rc := approveAll(context.Background(), []string{"--json"}, false, stores, &out, &errs); rc != 0 ||
		!strings.Contains(out.String(), `"working":[{`) || !strings.Contains(out.String(), `"id":"`+e.ID+`"`) ||
		!strings.Contains(out.String(), `"resource":"refs/heads/agent/x"`) {
		t.Fatalf("json %d %s %s", rc, out.String(), errs.String())
	}
	// An ended entry covers nothing, and is not listed.
	if err := ws.EndWorking(e.ID, brokerstore.EndDeleted); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	approveAll(context.Background(), nil, false, stores, &out, &errs)
	if strings.Contains(out.String(), e.ID) {
		t.Fatalf("an ended entry is listed:\n%s", out.String())
	}
}

// Through the registered verb, so the git store reaching `yolo approve` is pinned at its call
// site (runApprove).
func TestApproveRevokeWorkingID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_USER_LAYER", "")
	ws := workingStore(paths.BrokerSourceDir("git"))
	e := addEntry(t, ws, "/w", "refs/heads/agent/x")
	keep := addEntry(t, ws, "/w", "refs/heads/agent/y")
	if rc := registry["approve"]([]string{"approve", "--revoke", e.ID}); rc != 0 {
		t.Fatalf("revoke %s: %d", e.ID, rc)
	}
	if _, ok, _ := ws.LiveWorking("/w", e.Repo, e.Resource); ok {
		t.Fatal("revoked entry still live")
	}
	if _, ok, _ := ws.LiveWorking("/w", keep.Repo, keep.Resource); !ok {
		t.Fatal("revoking one entry ended another")
	}
	if rc := registry["approve"]([]string{"approve", "--revoke", "w-00000000000000000000000000000000"}); rc != approveExitUnknownID {
		t.Fatalf("unknown entry: %d, want %d", rc, approveExitUnknownID)
	}
}

func TestApproveRevokeAllWorkspace(t *testing.T) {
	gh := &brokerstore.Store{Dir: t.TempDir(), Source: "github", Revalidate: func(q brokerstore.Request) (brokerstore.Policy, error) { return q.Policy, nil }}
	grantIn := func(ws string) brokerstore.Grant {
		r, err := gh.File(brokerstore.Request{Jail: "j" + ws, Start: "s", Workspace: ws, Argv: []string{"pr", "merge"}, Repo: "o/r",
			StdinDigest: "d", Set: "read-write", Policy: brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 5}})
		if err != nil {
			t.Fatal(err)
		}
		g, err := gh.Decide(r.ID, "pending", brokerstore.Decision{For: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	root := t.TempDir()
	a, b := root+"/a", root+"/b"
	gA, gB := grantIn(a), grantIn(b)
	ws := workingStore(t.TempDir())
	eA, eB := addEntry(t, ws, a, "refs/heads/x"), addEntry(t, ws, b, "refs/heads/x")
	stores := []*brokerstore.Store{gh, ws}
	var out, errs bytes.Buffer
	if rc := approveAll(context.Background(), []string{"--revoke", "all", "--workspace", a + "/"}, false, stores, &out, &errs); rc != 0 {
		t.Fatalf("revoke all --workspace: %d %s", rc, errs.String())
	}
	_, gs, _ := gh.List()
	for _, g := range gs {
		if g.ID == gA.ID && g.EndedBy() != brokerstore.EndRevoked {
			t.Fatalf("workspace a's grant: %+v", g)
		}
		if g.ID == gB.ID && !g.Ended.IsZero() {
			t.Fatalf("workspace b's grant ended: %+v", g)
		}
	}
	if _, ok, _ := ws.LiveWorking(a, eA.Repo, eA.Resource); ok {
		t.Fatal("workspace a's entry still live")
	}
	if _, ok, _ := ws.LiveWorking(b, eB.Repo, eB.Resource); !ok {
		t.Fatal("workspace b's entry ended")
	}
	for _, args := range [][]string{{"--revoke", eB.ID, "--workspace", b}, {"--workspace", b}, {"--revoke", "all", "--workspace"}} {
		if rc := approveAll(context.Background(), args, false, stores, &out, &errs); rc != approveExitUsage {
			t.Fatalf("%v: %d, want usage", args, rc)
		}
	}
	if _, ok, _ := ws.LiveWorking(b, eB.Repo, eB.Resource); !ok {
		t.Fatal("a refused revoke ended an entry")
	}
}
