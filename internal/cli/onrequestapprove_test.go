package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerhook"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/ghbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// onRequestExample is config-ref's worked example, word for word: the three buttons of
// OQ-BB3, each calling plain `yolo approve`, which the broker's private `yolo` link resolves.
const onRequestExample = `a=$(notify-send -A once="Allow once" -A rw="Read-write 15 min" -A deny=Deny "$YOLO_REQUEST_SUMMARY"); case "$a" in once) yolo approve "$YOLO_REQUEST_ID";; rw) yolo approve "$YOLO_REQUEST_ID" --for 15m;; deny) yolo approve "$YOLO_REQUEST_ID" --deny;; esac`

// A click does exactly what the CLI does, because it IS the CLI (BB-D75): the documented
// example, with a stand-in notify-send answering each button in turn, runs the real
// `yolo approve` against the host store, and each answer leaves the state that command
// leaves. An answer that is no button decides nothing.
func TestTheOnRequestExampleDecidesThroughTheCLI(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("the hook runs under /bin/sh")
	}
	quoted, _ := json.Marshal(onRequestExample)
	if !strings.Contains(configRefContent, `"on_request": `+string(quoted)+"\n") {
		t.Fatalf("config-ref's on_request example is not the one this test runs, %s", quoted)
	}
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("YOLO_VERSION", "") // the hook's `yolo approve` runs on the host
	t.Setenv("YOLO_USER_LAYER", "")

	// A live launch's scope, bound to a broker lifetime this process holds.
	ws := filepath.Join(root, "ws")
	sf := brokerscope.File{Source: ghbroker.Source, LaunchID: "cafe", PID: os.Getpid(), Container: "fixture",
		Workspace: ws, Repos: []string{"o/r"}}
	path, err := brokerscope.Write(sf)
	if err != nil {
		t.Fatal(err)
	}
	start, err := brokerstore.NewStartID()
	if err != nil {
		t.Fatal(err)
	}
	a, err := ghbroker.BindAuthority(path, sf, start)
	if err != nil {
		t.Fatal(err)
	}
	s := &brokerstore.Store{Dir: paths.BrokerSourceDir(ghbroker.Source), Source: ghbroker.Source, Revalidate: a.Revalidate}

	// The test binary stands in for yolo, and a stand-in notify-send clicks the button
	// each request's PR number, the summary's last word, names.
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	yolo := filepath.Join(root, "yolo") // off the hook's PATH: only the broker's link reaches it
	write := func(p, body string) {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(yolo, "exec '"+exe+"' "+testAsYoloArg+" \"$@\"\n")
	write(filepath.Join(bin, "notify-send"), `case "$*" in
  *"o/r 1") echo once ;;
  *"o/r 2") echo rw ;;
  *"o/r 3") echo deny ;;
  *) touch "$HOME/asked-4"; echo "maybe later" ;;
esac
`)
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	ctx, cancel := context.WithCancel(context.Background())
	r := &brokerhook.Runner{Store: s, Start: start, Command: onRequestExample, Bin: yolo, Dir: root,
		Interval: 20 * time.Millisecond}
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(100 * time.Millisecond)

	file := func(n string) brokerstore.Request {
		t.Helper()
		argv := []string{"pr", "comment", n, "--body", "hi"}
		d, policy, err := a.Classify(argv, "o/r")
		if err != nil {
			t.Fatal(err)
		}
		q, err := s.File(brokerstore.Request{Jail: paths.JailShortHash(sf.Container), Start: start, Workspace: ws,
			LaunchID: sf.LaunchID, Argv: d.Argv, Repo: "o/r", Set: d.Set, Policy: policy, StdinDigest: "stdin"})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	once, rw, deny := file("1"), file("2"), file("3") // three: the pending limit per jail
	state := func(id string) string {
		rs, _, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range rs {
			if q.ID == id {
				return q.State
			}
		}
		return ""
	}
	deadline := time.Now().Add(60 * time.Second)
	for state(once.ID) == "pending" || state(rw.ID) == "pending" || state(deny.ID) == "pending" {
		if time.Now().After(deadline) {
			t.Fatalf("the clicks did not decide: once %s, rw %s, deny %s", state(once.ID), state(rw.ID), state(deny.ID))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state(once.ID) != "allowed" || state(rw.ID) != "allowed" || state(deny.ID) != "denied" {
		t.Fatalf("once %s, rw %s, deny %s", state(once.ID), state(rw.ID), state(deny.ID))
	}
	_, gs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	byRequest := map[string]brokerstore.Grant{}
	for _, g := range gs {
		byRequest[g.Request] = g
	}
	// Allow once: that one command, one use. Read-write 15 min: the whole set, its use cap.
	if g := byRequest[once.ID]; g.Digest != once.Digest || g.Uses != 1 {
		t.Fatalf("Allow once issued %+v", g)
	}
	if g := byRequest[rw.ID]; g.Digest != "" || g.Uses != rw.Policy.Uses || time.Until(g.Expires) > 15*time.Minute {
		t.Fatalf("Read-write 15 min issued %+v", g)
	}
	garbage := file("4")
	for {
		if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "asked-4")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the hook never asked about the fourth request")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // ten polls after it was answered
	if got := state(garbage.ID); got != "pending" {
		t.Fatalf("an answer that is no button left the request %s", got)
	}
	// The CLI's own audit recorded each decision.
	audit, err := os.ReadFile(paths.BrokerAuditLog())
	if err != nil || bytes.Count(audit, []byte(`"event":"decision"`)) != 3 {
		t.Fatalf("audit: %s %v", audit, err)
	}
}
