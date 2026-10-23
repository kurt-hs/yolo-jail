package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
)

// `--json` is one document of pending requests and live grants, with "schema": 1.
func TestApproveJSONListsOpenRecords(t *testing.T) {
	s, r := approvalFixture(t)
	s.Source = "github"
	var out, errs bytes.Buffer
	if rc := approveMain(context.Background(), []string{"--json"}, false, s, &out, &errs); rc != 0 {
		t.Fatal(rc, errs.String())
	}
	var doc struct {
		Schema   int `json:"schema"`
		Requests []struct {
			ID, Source, State, Repo, Command string
		} `json:"requests"`
		Grants []any `json:"grants"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("%v %q", err, out.String())
	}
	if doc.Schema != 1 || len(doc.Requests) != 1 || doc.Requests[0].ID != r.ID || doc.Requests[0].Source != "github" ||
		doc.Requests[0].Command != "pr merge 1" || doc.Grants == nil {
		t.Fatalf("%+v", doc)
	}
	if rc := approveMain(context.Background(), []string{"--json", r.ID}, false, s, &out, &errs); rc != approveExitUsage {
		t.Fatalf("--json with a decision: %d", rc)
	}
}

// The stable exit codes, each message naming a next step.
func TestApproveStableExitCodes(t *testing.T) {
	s, r := approvalFixture(t)
	run := func(args ...string) (int, string) {
		var out, errs bytes.Buffer
		rc := approveMain(context.Background(), args, false, s, &out, &errs)
		return rc, errs.String()
	}
	if rc, msg := run(r.ID, "--for", "2h"); rc != approveExitPolicy || !strings.Contains(msg, "Next: Allow it once or choose a shorter --for") {
		t.Fatalf("policy %d %q", rc, msg)
	}
	if rc, msg := run("r-00000000000000000000000000000000"); rc != approveExitUnknownID || !strings.Contains(msg, "Next: run `yolo approve` to list") {
		t.Fatalf("unknown %d %q", rc, msg)
	}
	if rc, msg := run("--revoke", "g-00000000000000000000000000000000"); rc != approveExitUnknownID || !strings.Contains(msg, "Next:") {
		t.Fatalf("unknown grant %d %q", rc, msg)
	}
	if rc, _ := run(r.ID, "--deny"); rc != 0 {
		t.Fatal(rc)
	}
	if rc, msg := run(r.ID); rc != approveExitAlreadyDecided || !strings.Contains(msg, "already denied") ||
		!strings.Contains(msg, "Next: run `yolo approve` to see its state") {
		t.Fatalf("decided %d %q", rc, msg)
	}
	if rc, _ := run("--nonsense"); rc != approveExitUsage {
		t.Fatal(rc)
	}
	// A request the host can no longer vouch for (its scope file is gone) is its own code,
	// and its next step is not to pick a shorter window.
	stale, err := s.File(brokerstore.Request{Jail: "j", Start: "s", Workspace: "/w", Argv: []string{"pr", "merge", "2"}, Repo: "o/r", StdinDigest: "stdin", Set: "read-write", Policy: brokerstore.Policy{Window: 15 * time.Minute, MaxWindow: time.Hour, Uses: 25}})
	if err != nil {
		t.Fatal(err)
	}
	s.Revalidate = func(brokerstore.Request) (brokerstore.Policy, error) {
		return brokerstore.Policy{}, errors.New("read scope file: no such file or directory")
	}
	if rc, msg := run(stale.ID); rc != approveExitNotConfirmed || strings.Contains(msg, "shorter --for") || !strings.Contains(msg, "Next: deny it") {
		t.Fatalf("not confirmed %d %q", rc, msg)
	}
	// The numbers are the contract a UI keys on.
	if approveExitFailed != 1 || approveExitUsage != 2 || approveExitAlreadyDecided != 3 || approveExitUnknownID != 4 || approveExitPolicy != 5 || approveExitNotConfirmed != 6 {
		t.Fatal("an exit code moved")
	}
}

// waitLines waits until buf holds at least n lines.
func waitLines(t *testing.T, buf *syncBuffer, n int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := buf.String()
		lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
		if s != "" && len(lines) >= n {
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("wanted %d lines, have %q", n, s)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func fastWatch(t *testing.T) {
	old := approveWatchInterval
	approveWatchInterval = 5 * time.Millisecond
	t.Cleanup(func() { approveWatchInterval = old })
}

// `--watch --json` opens with the state, then one object per event, until interrupted.
func TestApproveWatchJSON(t *testing.T) {
	fastWatch(t)
	s, r := approvalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var out, errs syncBuffer
	done := make(chan int)
	go func() { done <- approveMain(ctx, []string{"--watch", "--json"}, false, s, &out, &errs) }()
	waitLines(t, &out, 1)
	g, err := s.Decide(r.ID, "pending", brokerstore.Decision{})
	if err != nil {
		t.Fatal(err)
	}
	waitLines(t, &out, 2)
	if err := s.Revoke(g.ID); err != nil {
		t.Fatal(err)
	}
	lines := waitLines(t, &out, 3)
	cancel()
	if rc := <-done; rc != 0 {
		t.Fatal(rc, errs.String())
	}
	var events []string
	for _, l := range lines {
		var v struct {
			Schema int    `json:"schema"`
			Event  string `json:"event"`
		}
		if err := json.Unmarshal([]byte(l), &v); err != nil || v.Schema != 1 {
			t.Fatalf("%v %q", err, l)
		}
		events = append(events, v.Event)
	}
	if strings.Join(events, ",") != "state,request-decided,grant-revoked" {
		t.Fatalf("events %v", events)
	}
	if !strings.Contains(lines[0], r.ID) {
		t.Fatalf("the opening state misses the pending request: %s", lines[0])
	}
}

// Plain `--watch` prints the list, then one readable line per event.
func TestApproveWatchText(t *testing.T) {
	fastWatch(t)
	s, r := approvalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var out, errs syncBuffer
	done := make(chan int)
	go func() { done <- approveMain(ctx, []string{"--watch"}, false, s, &out, &errs) }()
	waitLines(t, &out, 1)
	if _, err := s.Decide(r.ID, "pending", brokerstore.Decision{Deny: true}); err != nil {
		t.Fatal(err)
	}
	lines := waitLines(t, &out, 2)
	cancel()
	if rc := <-done; rc != 0 {
		t.Fatal(rc)
	}
	if !strings.Contains(lines[1], "request-decided") || !strings.Contains(lines[1], r.ID) ||
		!strings.HasSuffix(lines[1], ": denied") || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("%q", out.String())
	}
	if !strings.Contains(errs.String(), "Ctrl-C") {
		t.Fatalf("%q", errs.String())
	}
}
