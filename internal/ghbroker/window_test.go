package ghbroker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerlock"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/frameproto"
	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

func windowFixture(t *testing.T) (*Broker, *atomic.Int32) {
	a, _ := authorityFixture(t)
	count := new(atomic.Int32)
	log := brokeraudit.Open(paths.BrokerAuditLog(), nil)
	b := &Broker{authority: a, scope: NewScope([]string{"o/r"}).WithGrants(readAll...), workspace: a.scope.Workspace, startID: a.start, store: &brokerstore.Store{Dir: paths.BrokerSourceDir(Source), Revalidate: a.Revalidate, Audit: StoreAudit(log)}, audit: log, log: io.Discard, now: time.Now, waitLimit: 20 * time.Millisecond}
	b.execute = func(ctx context.Context, argv []string, stdin []byte, out, err func([]byte)) RunResult {
		count.Add(1)
		return RunResult{Exit: 0}
	}
	b.delivery = &fakeDelivery{ready: true}
	return b, count
}

type fakeDelivery struct {
	ready   bool
	request func(brokerstore.Request) error
}

func (f *fakeDelivery) Ready() bool { return f.ready }
func (f *fakeDelivery) Request(ctx context.Context, q brokerstore.Request) error {
	if f.request != nil {
		return f.request(q)
	}
	return nil
}
func writeReq() Request {
	return Request{Argv: []string{"pr", "comment", "1", "--body", "hello"}, Repo: "o/r"}
}
func callWindow(b *Broker, ctx context.Context) int {
	return b.ServeContext(ctx, writeReq(), paths.JailShortHash(b.authority.scope.Container), func([]byte) {}, func([]byte) {})
}
func TestWindowedWriteDoesNotSpendLegacySetWideGrant(t *testing.T) {
	b, executed := windowFixture(t)
	if code := callWindow(b, context.Background()); code != ExitTempFail {
		t.Fatalf("first write did not file a pending request: %d", code)
	}
	requests, _, err := b.store.List()
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests: %+v, %v", requests, err)
	}
	legacy, err := b.store.Decide(requests[0].ID, "pending", brokerstore.Decision{For: time.Minute})
	if err != nil || legacy.Digest != "" {
		t.Fatalf("legacy timed grant: %+v, %v", legacy, err)
	}
	if code := callWindow(b, context.Background()); code != ExitTempFail {
		t.Fatalf("legacy set-wide grant authorized write: %d", code)
	}
	if executed.Load() != 0 {
		t.Fatalf("legacy set-wide grant dispatched %d times", executed.Load())
	}
	requests, grants, err := b.store.List()
	var replacementPending bool
	for _, request := range requests {
		replacementPending = replacementPending || request.ID != legacy.Request && request.State == "pending"
	}
	if err != nil || len(requests) != 2 || !replacementPending || len(grants) != 1 || grants[0].Uses != legacy.Uses {
		t.Fatalf("legacy grant was spent or no replacement request filed: requests=%+v grants=%+v err=%v", requests, grants, err)
	}
}

func TestWindowedGitHubGrantBindsStdinDigest(t *testing.T) {
	b, executed := windowFixture(t)
	jail := paths.JailShortHash(b.authority.scope.Container)
	req := Request{Argv: []string{"pr", "comment", "1", "--body-file", "-"}, Repo: "o/r", Stdin: []byte("approved body"), StdinSent: true}
	call := func() int {
		return b.ServeContext(context.Background(), req, jail, func([]byte) {}, func([]byte) {})
	}
	if code := call(); code != ExitTempFail {
		t.Fatalf("write did not file pending request: %d", code)
	}
	requests, _, err := b.store.List()
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests: %+v, %v", requests, err)
	}
	grant, err := b.store.Decide(requests[0].ID, "pending", brokerstore.Decision{})
	if err != nil || grant.Digest == "" {
		t.Fatalf("exact grant: %+v, %v", grant, err)
	}
	req.Stdin = []byte("changed body")
	if code := call(); code != ExitTempFail {
		t.Fatalf("changed stdin used an exact grant: %d", code)
	}
	requests, grants, err := b.store.List()
	var originalAllowed, changedPending bool
	for _, request := range requests {
		if request.Digest == grant.Digest {
			originalAllowed = request.State == "allowed"
		} else {
			changedPending = request.State == "pending"
		}
	}
	if err != nil || len(requests) != 2 || !originalAllowed || !changedPending || len(grants) != 1 || grants[0].Uses != 1 || executed.Load() != 0 {
		t.Fatalf("changed stdin spent or dispatched the grant: requests=%+v grants=%+v calls=%d err=%v", requests, grants, executed.Load(), err)
	}
}

func TestWindowHandlerRequiredDeliveryAndAuditOrdering(t *testing.T) {
	b, n := windowFixture(t)
	b.delivery = nil
	if got := callWindow(b, context.Background()); got != ExitNoPerm || n.Load() != 0 {
		t.Fatal(got, n.Load())
	}
	rs, _, _ := b.store.List()
	if len(rs) != 0 {
		t.Fatal("filed without required delivery")
	}
	b.delivery = &fakeDelivery{ready: true, request: func(q brokerstore.Request) error {
		_, err := b.store.Decide(q.ID, "pending", brokerstore.Decision{})
		return err
	}}
	var events []string
	b.store.Audit = func(ctx context.Context, kind string, q brokerstore.Request, g brokerstore.Grant) error {
		events = append(events, kind)
		if kind == "grant-used" {
			return errors.New("audit unavailable")
		}
		return nil
	}
	if got := callWindow(b, context.Background()); got != ExitUnavailable || n.Load() != 0 {
		t.Fatal(got, n.Load())
	}
	if len(events) != 3 || events[0] != "request" || events[1] != "decision" || events[2] != "grant-used" {
		t.Fatal(events)
	}
	_, gs, _ := b.store.List()
	if gs[0].Uses != 0 {
		t.Fatal("failed audit refunded use")
	}
}
func TestWindowHandlerWaitTimeoutDisconnectAndStatus(t *testing.T) {
	b, n := windowFixture(t)
	if got := callWindow(b, context.Background()); got != ExitTempFail {
		t.Fatal(got)
	}
	rs, _, _ := b.store.List()
	if len(rs) != 1 || rs[0].State != "pending" {
		t.Fatal(rs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := callWindow(b, ctx); got != ExitTempFail || n.Load() != 0 {
		t.Fatal(got)
	}
	rs2, _, _ := b.store.List()
	if len(rs2) != 1 {
		t.Fatal("duplicate pending not joined")
	}
	q := rs[0]
	if _, err := b.store.Status("other", q.Start, q.ID); err == nil {
		t.Fatal("cross-jail status leak")
	}
	if _, err := b.store.Decide(q.ID, "pending", brokerstore.Decision{}); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 0 {
		t.Fatal("decision dispatched disconnected caller")
	}
	if got := callWindow(b, context.Background()); got != 0 || n.Load() != 1 {
		t.Fatal(got, n.Load())
	}
}
func TestWindowHandlerDuplicateUseAndCurrentNarrowing(t *testing.T) {
	b, n := windowFixture(t)
	if callWindow(b, context.Background()) != ExitTempFail {
		t.Fatal("not pending")
	}
	rs, _, _ := b.store.List()
	if _, err := b.store.Decide(rs[0].ID, "pending", brokerstore.Decision{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); callWindow(b, context.Background()) }()
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatal("duplicate dispatch", n.Load())
	}
}

func TestWindowHandlerNarrowingTeardownExpiryAndRevocation(t *testing.T) {
	for _, change := range []string{"scope", "policy", "lease", "revoke", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b, n := windowFixture(t)
			if callWindow(b, context.Background()) != ExitTempFail {
				t.Fatal("not pending")
			}
			rs, _, _ := b.store.List()
			q := rs[0]
			g, err := b.store.Decide(q.ID, "pending", brokerstore.Decision{})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "scope":
				if err := brokerscope.Remove(b.authority.path); err != nil {
					t.Fatal(err)
				}
			case "policy":
				if err := os.MkdirAll(filepath.Dir(paths.UserConfigPath()), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.UserConfigPath(), []byte(`{"brokered":{"github":{"sets":[{"name":"read-write","mode":"windowed","window":"1m","max_window":"1m","uses":1}]}}}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "lease":
				if err := b.authority.Close(); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				if err := b.store.Revoke(g.ID); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				b.store.Now = func() time.Time { return g.Expires.Add(time.Second) }
			}
			if got := callWindow(b, context.Background()); got == 0 || n.Load() != 0 {
				t.Fatal(change, got, n.Load())
			}
		})
	}
}
func TestHostPolicyExplicitAdmitFailsClosedIncludingEmpty(t *testing.T) {
	for _, admit := range []string{`[]`, `["pr comment"]`, `["api POST repos/{owner}/{repo}/issues"]`} {
		t.Run(admit, func(t *testing.T) {
			b, n := windowFixture(t)
			os.MkdirAll(filepath.Dir(paths.UserConfigPath()), 0700)
			if err := os.WriteFile(paths.UserConfigPath(), []byte(`{"brokered":{"github":{"sets":[{"name":"read-write","mode":"windowed","admit":`+admit+`}]}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			// §5.7 only specifies that no admit keeps defaults, not whether an explicit
			// admit replaces or augments. Neither broader interpretation is assumed.
			if got := callWindow(b, context.Background()); got == 0 || n.Load() != 0 {
				t.Fatal(got)
			}
			rs, _, _ := b.store.List()
			if len(rs) != 0 {
				t.Fatal("unsupported policy filed")
			}
		})
	}
}
func TestWindowThroughRealForwarderHandlerAndStatus(t *testing.T) {
	b, n := windowFixture(t)
	t.Setenv(svcendpoint.AdvertiseHostEnv, "127.0.0.1")
	dir, err := os.MkdirTemp("/tmp", "pile7-front-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	epDir := filepath.Join(dir, paths.JailShortHash(b.authority.scope.Container))
	if err := os.Mkdir(epDir, 0700); err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(epDir, "github-broker.endpoint")
	stop := make(chan struct{})
	daemonDone := make(chan error, 1)
	frontDone := make(chan error, 1)
	go func() { daemonDone <- hostservice.ServeFrontedUnix(b.Handle, sock, stop) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	go func() { frontDone <- svcendpoint.ServeFront(endpoint, "127.0.0.1", sock, stop) }()
	t.Cleanup(func() { close(stop); <-daemonDone; <-frontDone })
	for !svcendpoint.Probe(endpoint) {
		if time.Now().After(deadline) {
			t.Fatal("front unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	env := ForwardEnv{Getenv: func(k string) string {
		if k == EndpointEnv {
			return endpoint
		}
		return ""
	}, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, OriginRepo: func() string { return "o/r" }}
	if got := Forward(writeReq().Argv, env); got != ExitTempFail {
		t.Fatal(got)
	}
	rs, _, _ := b.store.List()
	if len(rs) != 1 {
		t.Fatal(rs)
	}
	if got := Forward([]string{"status", rs[0].ID}, env); got != ExitTempFail {
		t.Fatal(got)
	}
	// Once the user's on_request has failed for it, the agent hears so, and nothing more.
	if b.notifierNote(rs[0].Jail, rs[0].ID) != "" {
		t.Fatal("a notifier note before any hook failed")
	}
	if err := b.store.NoteHookFailure(rs[0].ID, "hook exited 127: sh: notify-send: not found", "sh: notify-send: not found\n"); err != nil {
		t.Fatal(err)
	}
	var agentErr bytes.Buffer
	withErr := env
	withErr.Stderr = &agentErr
	if got := Forward([]string{"status", rs[0].ID}, withErr); got != ExitTempFail ||
		!strings.Contains(agentErr.String(), "notifier failed") || strings.Contains(agentErr.String(), "notify-send") {
		t.Fatalf("status after a failed hook: %d %q", got, agentErr.String())
	}
	if note := b.notifierNote(rs[0].Jail, rs[0].ID); note != NotifierFailed {
		t.Fatalf("notifier note %q", note)
	}
	if _, err := b.store.Decide(rs[0].ID, "pending", brokerstore.Decision{}); err != nil {
		t.Fatal(err)
	}
	if got := Forward([]string{"wait", rs[0].ID}, env); got != 0 || n.Load() != 0 {
		t.Fatal("status dispatched", got, n.Load())
	}
	if got := Forward(writeReq().Argv, env); got != 0 || n.Load() != 1 {
		t.Fatal("handler not dispatched", got, n.Load())
	}

	for _, body := range []string{`{"argv":["pr","comment",1,"--body","hi"],"repo":"o/r"}`, `{"argv":["pr","comment","1","--body-file","-"],"repo":"o/r","stdin":"not-base64"}`} {
		conn, err := svcendpoint.Dial(endpoint, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := frameproto.WriteRequest(conn, []byte(body)); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		for {
			frame, err := frameproto.ReadFrame(conn)
			if err != nil {
				conn.Close()
				t.Fatal(err)
			}
			if frame.StreamID == frameproto.StreamExit {
				if int(binary.BigEndian.Uint32(frame.Payload)) != ExitUsage {
					t.Fatal("malformed frame not refused")
				}
				break
			}
		}
		conn.Close()
	}
	if n.Load() != 1 {
		t.Fatal("malformed frame dispatched")
	}
}

func TestWindowWaitCancelsWhileSharedTransactionIsHeld(t *testing.T) {
	b, n := windowFixture(t)
	if callWindow(b, context.Background()) != ExitTempFail {
		t.Fatal("not pending")
	}
	rs, _, _ := b.store.List()
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- brokerlock.WithTransaction(b.store.Dir, func() error { close(held); <-release; return nil })
	}()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := b.waitStatus(ctx, rs[0].Jail, rs[0].ID); result <- err }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("wait succeeded")
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("wait ignored context while blocked on shared flock")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n.Load() != 0 {
		t.Fatal("canceled wait executed")
	}
}

func TestWindowRequiredAuditLockHonorsCallerCancellation(t *testing.T) {
	b, n := windowFixture(t)
	if err := os.MkdirAll(filepath.Dir(paths.BrokerAuditLog()), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(paths.BrokerAuditLog()), ".audit.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- callWindow(b, ctx) }()
	select {
	case code := <-done:
		if code == 0 || n.Load() != 0 {
			t.Fatal(code, n.Load())
		}
	case <-time.After(time.Second):
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		<-done
		t.Fatal("required/finish audit ignored cancellation")
	}
	syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	rs, _, _ := b.store.List()
	if len(rs) != 0 {
		t.Fatal("failed request audit was persisted")
	}
}

func TestWindowAuditRecordsFailedExecutorAsRan(t *testing.T) {
	b, _ := windowFixture(t)
	b.delivery = &fakeDelivery{ready: true, request: func(q brokerstore.Request) error {
		_, err := b.store.Decide(q.ID, "pending", brokerstore.Decision{})
		return err
	}}
	b.execute = func(context.Context, []string, []byte, func([]byte), func([]byte)) RunResult {
		return RunResult{Exit: 1, BytesOut: 42, Redactions: 1}
	}
	if got := callWindow(b, context.Background()); got != 1 {
		t.Fatal(got)
	}
	ev := lastAudit(t)
	if ev.Outcome != "ran" || ev.BytesOut != 42 || ev.Redactions != 1 {
		t.Fatal(ev)
	}
}
