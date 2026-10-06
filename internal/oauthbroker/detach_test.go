package oauthbroker

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// THE DETACH DISPOSITION, in the real daemon process (docs/design/host-daemon-ownership.md §1.3,
// "detach, do not drain, do not reap"): a jail's end SIGTERMs its broker and does not wait, so
// the broker itself must let a refresh already in flight finish and write before it exits. A
// refresh cut there may already have redeemed the single-use refresh token upstream, and its
// replacement would be lost with the process.
//
// The background refresher's first tick is the in-flight refresh: the credential is due, and the
// fake upstream holds the request until the test releases it. The SIGTERM lands while it is
// held. Delete the wait for the refresher in Main and the child exits at once, before the
// upstream answers, leaving the old token on disk.
func TestASIGTERMedBrokerFinishesTheRefreshInFlight(t *testing.T) {
	got := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(got) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT_after_sigterm","refresh_token":"RT_after_sigterm",` +
			`"expires_in":3600,"scope":"user:inference"}`))
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	creds := seedCreds(t, dir, "RT_before_sigterm")
	sock := filepath.Join(shortSocketDir(t), "yolo-claude-oauth-broker.sock")
	cmd := exec.Command(os.Args[0], "-singleton-child",
		"--socket", sock, "--creds-file", creds, "--log-file", filepath.Join(dir, "daemon.log"))
	cmd.Env = append(os.Environ(),
		"HOME="+dir,
		"XDG_CONFIG_HOME="+filepath.Join(dir, ".config"),
		"YOLO_BROKER_STATE_DIR="+state,
		"YOLO_BROKER_UPSTREAM_URL="+srv.URL,
	)
	var mu sync.Mutex
	var sb strings.Builder
	cmd.Stdout, cmd.Stderr = &lockedWriter{mu: &mu, w: &sb}, &lockedWriter{mu: &mu, w: &sb}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logs := func() string {
		mu.Lock()
		defer mu.Unlock()
		raw, _ := os.ReadFile(filepath.Join(dir, "daemon.log"))
		return sb.String() + string(raw)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()

	select {
	case <-got:
	case err := <-done:
		t.Fatalf("the broker exited (%v) before its background refresh reached the upstream\n%s", err, logs())
	case <-time.After(15 * time.Second):
		t.Fatalf("the broker's background refresh never reached the upstream\n%s", logs())
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("the broker exited (%v) with its refresh still in flight: a SIGTERM must let it "+
			"finish, since the jail's end does not wait for it\n%s", err, logs())
	case <-time.After(time.Second):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the broker exited with %v after its refresh finished, want a clean exit\n%s", err, logs())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("the broker is still running 15s after its refresh finished\n%s", logs())
	}
	var wrote bool
	for _, p := range []string{creds, filepath.Join(state, canonicalName)} {
		if raw, err := os.ReadFile(p); err == nil && strings.Contains(string(raw), "AT_after_sigterm") {
			wrote = true
		}
	}
	if !wrote {
		t.Errorf("the refresh that was in flight at the SIGTERM wrote nothing: neither %s nor the "+
			"canonical under %s holds the new token\n%s", creds, state, logs())
	}
}
