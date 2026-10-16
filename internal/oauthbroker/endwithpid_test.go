package oauthbroker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A JAIL-LESS LAUNCH'S OWN BROKER ENDS WITH THE LAUNCH (HD-D23 in
// docs/design/host-daemon-ownership.md; internal/cli's startHostViewBroker, which keeps a
// `yolo host -- claude` credential view current): with --end-with-pid the daemon serves while that
// process lives, exits once it is gone, a launch that exec'd Claude in its place included, and
// removes the socket that was that launch's alone. Delete the WatchProcess call in Main and it
// serves on past the deadline.
func TestABrokerEndingWithItsLaunchExits(t *testing.T) {
	launch := exec.Command("sleep", "30")
	if err := launch.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = launch.Process.Kill() })
	launchGone := make(chan struct{})
	go func() { _ = launch.Wait(); close(launchGone) }()

	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	creds := seedCreds(t, dir, "RT_end_with_pid")
	sock := filepath.Join(shortSocketDir(t), "yolo-claude-host.sock")
	cmd := exec.Command(os.Args[0], "-singleton-child", "--socket", sock, "--creds-file", creds,
		"--no-background-refresh", "--log-file", filepath.Join(dir, "daemon.log"),
		"--end-with-pid", strconv.Itoa(launch.Process.Pid))
	cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, ".config"),
		"YOLO_BROKER_STATE_DIR="+state)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	logs := func() string { raw, _ := os.ReadFile(filepath.Join(dir, "daemon.log")); return string(raw) }

	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("the broker exited (%v) before serving\n%s", err, logs())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the broker never bound its socket\n%s", logs())
		}
	}
	select {
	case err := <-done:
		t.Fatalf("the broker exited (%v) while the launch it serves was still running\n%s", err, logs())
	case <-time.After(300 * time.Millisecond):
	}
	if err := launch.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-launchGone
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the broker exited with %v once its launch was gone, want a clean exit\n%s", err, logs())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("the broker is still serving 15s after the launch it serves exited\n%s", logs())
	}
	if _, err := os.Lstat(sock); err == nil {
		t.Errorf("the broker left its socket %s behind", sock)
	}
}
