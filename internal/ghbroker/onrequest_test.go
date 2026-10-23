package ghbroker

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// The broker's own process runs the user's on_request (BB-D75): with the key in the user
// config, a request this broker lifetime holds is answered by a hook that prints `approve`,
// with no `yolo approve` and no watch kept open. Deleting startOnRequest's call in Main
// fails this test.
func TestTheBrokerRunsTheUsersOnRequest(t *testing.T) {
	dir := resolvedDir(t)
	stdin := filepath.Join(dir, "stdin.json")
	hook := filepath.Join(dir, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ncat >'"+stdin+"'\necho approve\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := startBrokerProcessWith(t, `{"on_request": `+strconv.Quote(hook)+`}`)
	f, err := brokerscope.ReadFile(p.scopeFile)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the broker to bind its lifetime", 10*time.Second, func() bool {
		f, err = brokerscope.ReadFile(p.scopeFile)
		return err == nil && f.BrokerStart != ""
	})
	a := &Authority{path: p.scopeFile, scope: f, start: f.BrokerStart}
	d, policy, err := a.Classify([]string{"pr", "comment", "1", "--body", "hi"}, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	s := &brokerstore.Store{Dir: paths.BrokerSourceDir(Source)}
	q, err := s.File(brokerstore.Request{Jail: paths.JailShortHash(f.Container), Start: f.BrokerStart,
		Workspace: p.workspace, LaunchID: f.LaunchID, Argv: d.Argv, Repo: "o/r", Set: d.Set, Policy: policy,
		StdinDigest: "stdin"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hook to approve the request", 20*time.Second, func() bool {
		got, err := s.Status(q.Jail, q.Start, q.ID)
		return err == nil && got.State == "allowed"
	})
	b, err := os.ReadFile(stdin)
	if err != nil || !bytes.Contains(b, []byte(`"event":"request-filed"`)) || !bytes.Contains(b, []byte(q.ID)) {
		t.Fatalf("the hook's stdin: %q %v", b, err)
	}
	audit, err := os.ReadFile(paths.BrokerAuditLog())
	if err != nil || !bytes.Contains(audit, []byte(`"event":"hook-decision"`)) || !bytes.Contains(audit, []byte(`"outcome":"allowed"`)) {
		t.Fatalf("the audit has no hook decision: %s %v", audit, err)
	}
}

// With no on_request set, the broker runs nothing for a request.
func TestNoOnRequestRunsNothing(t *testing.T) {
	var log bytes.Buffer
	b := &Broker{store: &brokerstore.Store{Dir: t.TempDir()}, startID: "s", log: &log}
	done := b.startOnRequest(t.Context(), "")
	select {
	case <-done:
	default:
		t.Fatal("an unset hook started a runner")
	}
	if log.Len() != 0 {
		t.Fatalf("%q", log.String())
	}
}
