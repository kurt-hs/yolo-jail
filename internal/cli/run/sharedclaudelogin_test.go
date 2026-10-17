package run

// sharedclaudelogin_test.go pins, through Run(), the container launch's one line about a shared
// Claude login file that does not parse: a jail whose Claude reads that file starts signed out,
// and nothing else in the launch says why. Its control is the empty placeholder every machine
// has before a first /login, which the launch leaves unsaid.

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// brokenLoginHeadline is the line's headline, spelled once for both tests.
const brokenLoginHeadline = "Claude's shared login file does not parse"

// claudeContainerLaunch runs a fresh podman launch selecting the claude pack, with the shared
// credentials file holding body, to its container's start, and returns everything it printed.
// The broker loophole is off: the line is about the file every Claude jail reads, which the
// launch links whether or not the broker runs.
func claudeContainerLaunch(t *testing.T, body []byte) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("a podman launch")
	}
	home := packHome(t)
	cfgDir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.jsonc"),
		[]byte(`{"packs": ["claude"], "loopholes": {"claude-oauth-broker": {"enabled": false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(paths.GlobalHome(), claudeSharedDirRel, ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, body, 0o600); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	o := dispatchOptions(t, ws, "podman", new(bytes.Buffer), new(bytes.Buffer), nil)
	var stdout, stderr lockedBuffer
	o.Stdout, o.Stderr = &stdout, &stderr
	repo, _ := o.RepoRoot()
	o.PathExists = func(p string) bool {
		return p == filepath.Join(prebuiltBinDir(repo.Root), "yolo-entrypoint")
	}
	o.Exec = func([]string, string, []string, time.Duration) ExecResult {
		return ExecResult{Ran: true, RC: 0}
	}
	o.autoLoad = func(image.AutoLoadOptions) image.LoadResult {
		return image.LoadResult{OK: true, Ref: goldenImageRef}
	}
	done := make(chan int, 1)
	go func() { done <- Run(*o) }()
	select {
	case <-done:
	case <-time.After(sealedLaunchBound):
		t.Fatalf("the launch had not returned after %s\nstdout:\n%s\nstderr:\n%s",
			sealedLaunchBound, stdout.String(), stderr.String())
	}
	if after, err := os.ReadFile(shared); err != nil || !bytes.Equal(after, body) {
		t.Errorf("the launch changed the shared credentials file (err %v)", err)
	}
	return stdout.String() + stderr.String()
}

func TestAContainerLaunchSaysTheSharedClaudeLoginDoesNotParse(t *testing.T) {
	out := claudeContainerLaunch(t, []byte(`{"claudeAiOauth": {"accessToken": "sk-ant-oat-SECRET`))
	if !strings.Contains(out, brokenLoginHeadline) {
		t.Fatalf("the launch did not say the shared Claude login does not parse:\n%s", out)
	}
	if strings.Count(out, brokenLoginHeadline) != 1 {
		t.Errorf("the launch said it more than once:\n%s", out)
	}
	if !strings.Contains(out, "/login") {
		t.Errorf("the line names no next step:\n%s", out)
	}
	if !strings.Contains(out, claudeSharedDirRel) {
		t.Errorf("the line does not name the file:\n%s", out)
	}
	if strings.Contains(out, "SECRET") {
		t.Errorf("the launch echoed the file's contents:\n%s", out)
	}
}

func TestAContainerLaunchLeavesTheEmptyPlaceholderUnsaid(t *testing.T) {
	out := claudeContainerLaunch(t, nil)
	// Reached runContainer's disclosure, beside which the notice is printed: not a vacuous pass.
	if !strings.Contains(out, "Pack environment this launch:") {
		t.Fatalf("the launch never reached the point the notice is printed at:\n%s", out)
	}
	if strings.Contains(out, brokenLoginHeadline) {
		t.Errorf("the empty placeholder, every machine's state before a first /login, was reported "+
			"as a broken login:\n%s", out)
	}
}
