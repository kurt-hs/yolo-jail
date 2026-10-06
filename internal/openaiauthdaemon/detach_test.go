package openaiauthdaemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
)

// THE DETACH DISPOSITION, through the real Main (docs/design/host-daemon-ownership.md §1.3,
// "detach, do not drain, do not reap"): a jail's end SIGTERMs this daemon and does not wait, so
// the daemon itself must let a refresh already in flight finish and write before it exits. A
// refresh cut there may already have redeemed the refresh token upstream, and its replacement
// would be lost with the process.
//
// The proactive refresher is the one in flight: the grant is past its expiry, and the fake
// token endpoint holds the request until the test releases it. The SIGTERM lands while it is
// held. Delete the wait for the refresher in Main and Main returns at once, before the endpoint
// answers, leaving the old generation on disk.
func TestASIGTERMedDaemonFinishesTheRefreshInFlight(t *testing.T) {
	got := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	access := jwt(`{"exp":4102444800,"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(got) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":%q,"id_token":"id-after","refresh_token":"rt-after",`+
			`"expires_in":3600}`, access)
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	statePath := filepath.Join(t.TempDir(), "state", "openai-auth-broker", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Minute).UnixMilli()
	if err := os.WriteFile(statePath, []byte(`{"version":1,"access_token":"at-before",`+
		`"id_token":"id-before","refresh_token":"rt-before","expires_at":`+
		strconv.FormatInt(expired, 10)+`,"account_id":"acct-123","generation":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(shortSocketDir(t), "fronted.sock")
	rc := make(chan int, 1)
	go func() {
		rc <- Main([]string{"--socket", sock, "--state-file", statePath,
			"--token-url", srv.URL, "--refresh-interval", "50ms"})
	}()

	select {
	case <-got:
	case code := <-rc:
		t.Fatalf("Main returned %d before its proactive refresh reached the token endpoint", code)
	case <-time.After(10 * time.Second):
		t.Fatal("the proactive refresh never reached the token endpoint")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-rc:
		t.Fatalf("Main returned %d with its refresh still in flight: a SIGTERM must let it finish, "+
			"since the jail's end does not wait for it", code)
	case <-time.After(time.Second):
	}
	close(release)
	select {
	case code := <-rc:
		if code != 0 {
			t.Errorf("Main returned %d after its refresh finished, want a clean 0", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Main is still running 10s after its refresh finished")
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "rt-after") {
		t.Errorf("the refresh in flight at the SIGTERM wrote nothing: %s still holds the old grant", statePath)
	}
}

// A JAIL-LESS LAUNCH'S OWN DAEMON ENDS WITH THE LAUNCH (HD-R1; openaiauthhost's startPrivate):
// with --end-with-pid, Main exits once that process is gone, a launch that exec'd its agent in
// its own place included, and removes the two sockets that were that launch's alone. Delete the
// WatchProcess call in Main and it serves on past the deadline.
func TestADaemonEndingWithItsLaunchExitsAndRemovesItsSockets(t *testing.T) {
	prev := hostservice.ProcessPollInterval
	hostservice.ProcessPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { hostservice.ProcessPollInterval = prev })

	launch := exec.Command("sleep", "30")
	if err := launch.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = launch.Process.Kill() })
	exited := make(chan struct{})
	go func() { _ = launch.Wait(); close(exited) }()

	sock := filepath.Join(shortSocketDir(t), "fronted.sock")
	statePath := filepath.Join(t.TempDir(), "state", "openai-auth-broker", "credentials.json")
	rc := make(chan int, 1)
	go func() {
		rc <- Main([]string{"--socket", sock, "--state-file", statePath, "--no-background-refresh",
			"--end-with-pid", strconv.Itoa(launch.Process.Pid)})
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(HostSocketPath(sock)); err == nil {
			break
		}
		select {
		case code := <-rc:
			t.Fatalf("Main returned %d before serving", code)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Main never bound its private socket")
		}
	}
	select {
	case code := <-rc:
		t.Fatalf("Main returned %d while the launch it serves was still running", code)
	case <-time.After(200 * time.Millisecond):
	}
	if err := launch.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-exited
	select {
	case code := <-rc:
		if code != 0 {
			t.Errorf("Main returned %d once its launch was gone, want a clean 0", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Main is still serving 10s after the launch it serves exited")
	}
	for _, p := range []string{sock, HostSocketPath(sock)} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("the daemon left its socket %s behind", p)
		}
	}
}
