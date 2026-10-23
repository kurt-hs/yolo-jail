package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/selfupdate"
)

// updateCheckTestHome points HOME at a fresh directory, clears every variable
// that turns the check off (CI is set on every CI runner), and returns the
// home.
func updateCheckTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"CI", "YOLO_VERSION", selfupdate.DisableEnv} {
		t.Setenv(k, "")
	}
	return home
}

// The background check's whole call path: the argv SpawnBackgroundCheck runs,
// through runInternal's dispatch, to the state file and the lock. Renaming the
// verb on either side, or dropping the runInternal case, fails this.
func TestInternalUpdateCheckRunsEndToEnd(t *testing.T) {
	updateCheckTestHome(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v0.11.0"}`))
	}))
	t.Cleanup(srv.Close)
	origCh, origURL := internalCheckChannel, internalCheckReleaseURL
	t.Cleanup(func() { internalCheckChannel, internalCheckReleaseURL = origCh, origURL })
	internalCheckChannel = func() selfupdate.Channel { return testChannel }
	internalCheckReleaseURL = srv.URL

	statePath := filepath.Join(t.TempDir(), "update-check", "state.json")
	lock := selfupdate.LockPath(statePath)
	run := func() int {
		t.Helper()
		// The spawner takes the lock before it starts the child; the child
		// must give it back.
		if !selfupdate.AcquireLock(lock, time.Now()) {
			t.Fatal("could not take the lock the spawner would hold")
		}
		args := selfupdate.BackgroundCheckArgs(statePath)
		if len(args) == 0 || args[0] != "internal" {
			t.Fatalf("background check argv %v does not start with `internal`", args)
		}
		return runInternal(args[1:])
	}

	if rc := run(); rc != 0 {
		t.Fatalf("runInternal exited %d", rc)
	}
	st := selfupdate.LoadState(statePath)
	if st.Identity != testChannel.Identity() || st.Latest != "0.11.0" || !st.Available || st.Error != "" {
		t.Errorf("state after the check = %+v; want 0.11.0 available for %s", st, testChannel.Identity())
	}
	if requests.Load() != 1 {
		t.Errorf("release server saw %d requests, want 1", requests.Load())
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("the lock survived the check (stat err %v)", err)
	}

	// "update_check": false in the user config turns the check off here too,
	// and the lock is still given back.
	writeUserConfig(t, `{"update_check": false}`)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if rc := run(); rc != 0 {
		t.Fatalf("runInternal exited %d with the check off", rc)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("a check ran with update_check false (stat err %v)", err)
	}
	if requests.Load() != 1 {
		t.Errorf("release server saw %d requests with the check off, want still 1", requests.Load())
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("the lock survived a disabled check (stat err %v)", err)
	}
}

// The user config's opt-out reaches the real dependencies the notice and the
// launch offer run with, not only a test's injected gate.
func TestUpdateCheckConfigOptOutReachesDefaultUpdateDeps(t *testing.T) {
	updateCheckTestHome(t)
	if !defaultUpdateDeps().configEnabled() {
		t.Error("with no user config the check must be on")
	}
	writeUserConfig(t, `{
  // turned off by the user
  "update_check": false
}`)
	if defaultUpdateDeps().configEnabled() {
		t.Error(`"update_check": false in the user config did not turn the check off`)
	}
}

// A snapshot install gets no background release check (IP-D1): the detached
// child, reached through the real argv, asks no release server and writes no
// state, and still gives the lock back. Deleting the KindSnapshot guard in
// runInternalUpdateCheck fails this.
func TestInternalUpdateCheckSkipsASnapshot(t *testing.T) {
	updateCheckTestHome(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v0.11.0"}`))
	}))
	t.Cleanup(srv.Close)
	origCh, origURL := internalCheckChannel, internalCheckReleaseURL
	t.Cleanup(func() { internalCheckChannel, internalCheckReleaseURL = origCh, origURL })
	internalCheckChannel = func() selfupdate.Channel { return snapshotTestChannel }
	internalCheckReleaseURL = srv.URL

	statePath := filepath.Join(t.TempDir(), "update-check", "state.json")
	lock := selfupdate.LockPath(statePath)
	if !selfupdate.AcquireLock(lock, time.Now()) {
		t.Fatal("could not take the lock the spawner would hold")
	}
	args := selfupdate.BackgroundCheckArgs(statePath)
	if rc := runInternal(args[1:]); rc != 0 {
		t.Fatalf("runInternal exited %d", rc)
	}
	if requests.Load() != 0 {
		t.Errorf("a snapshot install asked the release server %d times, want 0", requests.Load())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("a snapshot install's background check wrote state (stat err %v)", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("the lock survived the skipped check (stat err %v)", err)
	}
}

// The default `--from` wiring passes the caller's options through: without
// --autostash a dirty checkout is refused before anything runs, and its edits
// stay in the working tree. A default deploy that forced Autostash would stash
// them instead, and fails this.
func TestDefaultDeployWiringRefusesADirtyCheckoutWithoutAutostash(t *testing.T) {
	dir := t.TempDir()
	gitInit := exec.Command("git", "init", "-q", dir)
	gitInit.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	edit := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(edit, []byte("work in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := defaultUpdateDeps().deploy(context.Background(), filepath.Join(t.TempDir(), "yolo"), dir,
		selfupdate.CheckoutOptions{}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("default deploy on a dirty checkout without --autostash = %v; want the uncommitted-changes refusal\nstderr:\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "→ ") {
		t.Errorf("the refused deploy ran a step:\n%s", stderr.String())
	}
	if _, serr := os.Stat(edit); serr != nil {
		t.Errorf("the checkout's edit is gone after a refused deploy: %v", serr)
	}
}
