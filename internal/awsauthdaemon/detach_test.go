package awsauthdaemon

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/awsauth"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// THE DETACH DISPOSITION, through the real Main (docs/design/host-daemon-ownership.md §1.3):
// a jail's end SIGTERMs this daemon alone and does not wait for it, so the daemon must let a
// mint already in flight finish and fill the cache before it exits, which leaves the next jail
// warm. The `aws` child is never signaled, which is why the mint can finish at all.
//
// The proactive minter's first mint is the one in flight: the stub `aws` holds its
// export-credentials answer until the test releases it. The SIGTERM lands while it is held.
// Delete the wait for the minter in Main and Main returns at once, with nothing cached.
func TestASIGTERMedDaemonFinishesTheMintInFlight(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // reportStartup reads ~/.aws/config
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	stub := filepath.Join(dir, "aws")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --version ]; then echo aws-cli/2.99.0; exit 0; fi\n" +
		": > " + shquote.Quote(started) + "\n" +
		"while [ ! -e " + shquote.Quote(release) + " ]; do sleep 0.05; done\n" +
		`echo '{"Version":1,"AccessKeyId":"ASIAAFTERSIGTERM","SecretAccessKey":"s",` +
		`"SessionToken":"t","Expiration":"2033-05-18T03:33:20+00:00"}'` + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
	settings := settingsFileWith(t, `{"profile":"bedrock","unnarrowed":true}`)
	sock := filepath.Join(shortTempDir(t), "d.sock")
	statePath := filepath.Join(t.TempDir(), "state", LoopholeName, awsauth.StateFileName)
	rc := make(chan int, 1)
	go func() {
		rc <- Main([]string{"--socket", sock, "--settings", settings, "--aws-binary", stub,
			"--state-file", statePath})
	}()

	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case code := <-rc:
			t.Fatalf("Main returned %d before its proactive mint ran", code)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the proactive mint never ran the stub aws")
		}
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-rc:
		t.Fatalf("Main returned %d with its mint still in flight: a SIGTERM must let it finish, "+
			"since the jail's end does not wait for it", code)
	case <-time.After(time.Second):
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-rc:
		if code != 0 {
			t.Errorf("Main returned %d after its mint finished, want a clean 0", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Main is still running 10s after its mint finished")
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("the mint in flight at the SIGTERM cached nothing: %v", err)
	}
	if !strings.Contains(string(raw), "ASIAAFTERSIGTERM") {
		t.Errorf("the cache at %s does not hold the mint that was in flight at the SIGTERM", statePath)
	}
}
