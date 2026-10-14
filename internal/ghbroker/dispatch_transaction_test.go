package ghbroker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestHandlerSerializesScopeRemovalThroughStandingAndWindowedDispatch(t *testing.T) {
	for _, standing := range []bool{false, true} {
		t.Run(map[bool]string{false: "windowed", true: "standing"}[standing], func(t *testing.T) {
			b, _ := windowFixture(t)
			var grant brokerstore.Grant
			if standing {
				os.MkdirAll(filepath.Dir(paths.UserConfigPath()), 0700)
				if err := os.WriteFile(paths.UserConfigPath(), []byte(`{"brokered":{"github":{"sets":[{"name":"read-write","mode":"standing"}]}}}`), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if callWindow(b, context.Background()) != ExitTempFail {
					t.Fatal("not pending")
				}
				rs, _, _ := b.store.List()
				var err error
				grant, err = b.store.Decide(rs[0].ID, "pending", brokerstore.Decision{})
				if err != nil {
					t.Fatal(err)
				}
			}
			entered := make(chan error, 1)
			release := make(chan struct{})
			returned := make(chan int, 1)
			b.execute = func(ctx context.Context, _ []string, _ []byte, _, _ func([]byte)) RunResult {
				lock, err := os.OpenFile(filepath.Join(b.store.Dir, ".lock"), os.O_RDWR, 0600)
				if err == nil {
					err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
					lock.Close()
				}
				if err != syscall.EWOULDBLOCK {
					entered <- fmt.Errorf("executor-start has no shared authorization lock: %v", err)
				} else {
					if !standing {
						data, e := os.ReadFile(filepath.Join(b.store.Dir, "grants", grant.ID+".json"))
						var saved brokerstore.Grant
						if e != nil {
							entered <- e
						} else if e = json.Unmarshal(data, &saved); e != nil {
							entered <- e
						} else if saved.Uses != 0 {
							entered <- fmt.Errorf("dispatch before durable spend: %d", saved.Uses)
						} else {
							entered <- nil
						}
					} else {
						entered <- nil
					}
				}
				select {
				case <-release:
				case <-ctx.Done():
				}
				return RunResult{Exit: 0}
			}
			go func() { returned <- callWindow(b, context.Background()) }()
			err := <-entered
			removing := make(chan struct{})
			removed := make(chan error, 1)
			go func() { close(removing); removed <- brokerscope.Remove(b.authority.path) }()
			<-removing
			if err != nil {
				close(release)
				<-returned
				<-removed
				t.Fatal(err)
			}
			select {
			case e := <-removed:
				close(release)
				<-returned
				t.Fatalf("scope removal won before dispatch completed: %v", e)
			case <-time.After(30 * time.Millisecond):
			}
			close(release)
			if code := <-returned; code != 0 {
				t.Fatal(code)
			}
			if e := <-removed; e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestHandlerCanceledBoundedDispatchReleasesAuthorityLock(t *testing.T) {
	b, _ := windowFixture(t)
	b.delivery = &fakeDelivery{ready: true, request: func(q brokerstore.Request) error {
		_, err := b.store.Decide(q.ID, "pending", brokerstore.Decision{})
		return err
	}}
	entered := make(chan struct{})
	b.execute = func(ctx context.Context, _ []string, _ []byte, _, _ func([]byte)) RunResult {
		close(entered)
		<-ctx.Done()
		return RunResult{Exit: ExitTimeout}
	}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan int, 1)
	go func() { returned <- callWindow(b, ctx) }()
	<-entered
	removed := make(chan error, 1)
	go func() { removed <- brokerscope.Remove(b.authority.path) }()
	cancel()
	select {
	case code := <-returned:
		if code != ExitTimeout {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch ignored cancellation")
	}
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("authority lock leaked")
	}
	_, gs, err := b.store.List()
	if err != nil || len(gs) != 1 || gs[0].Uses != 0 {
		t.Fatal("canceled dispatch refunded", gs, err)
	}
}

func TestHandlerPendingLimitAndDenialCooldownNeverNotifyAgain(t *testing.T) {
	b, n := windowFixture(t)
	now := time.Now()
	b.store.Now = func() time.Time { return now }
	var delivered []string
	seen := map[string]bool{}
	b.delivery = &fakeDelivery{ready: true, request: func(q brokerstore.Request) error {
		if !seen[q.ID] {
			seen[q.ID] = true
			delivered = append(delivered, q.ID)
		}
		return nil
	}}
	call := func(body string) (int, string) {
		req := writeReq()
		req.Argv[len(req.Argv)-1] = body
		var msg strings.Builder
		code := b.ServeContext(context.Background(), req, paths.JailShortHash(b.authority.scope.Container), func([]byte) {}, func(p []byte) { msg.Write(p) })
		return code, msg.String()
	}
	for _, body := range []string{"a", "b", "c"} {
		if code, _ := call(body); code != ExitTempFail {
			t.Fatal(code)
		}
	}
	if code, _ := call("a"); code != ExitTempFail || len(delivered) != 3 {
		t.Fatal("duplicate did not join", code, delivered)
	}
	if code, msg := call("fourth"); code != ExitTempFail || len(delivered) != 3 {
		t.Fatal("cap notified", code, msg, delivered)
	} else {
		for _, id := range delivered {
			if !strings.Contains(msg, id) {
				t.Fatal("missing cap id", msg)
			}
		}
	}
	if _, err := b.store.Decide(delivered[0], "pending", brokerstore.Decision{Deny: true}); err != nil {
		t.Fatal(err)
	}
	if code, msg := call("a"); code != ExitNoPerm || len(delivered) != 3 || !strings.Contains(msg, delivered[0]) {
		t.Fatal("denial notified", code, msg, delivered)
	}
	now = now.Add(10 * time.Minute)
	if code, _ := call("a"); code != ExitTempFail || len(delivered) != 4 {
		t.Fatal("cooldown boundary", code, delivered)
	}
	if n.Load() != 0 {
		t.Fatal("limit/cooldown dispatched")
	}
}
