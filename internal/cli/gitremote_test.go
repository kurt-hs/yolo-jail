package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/gitremote"
)

// `yolo git-remote` speaks git's remote-helper protocol on stdout, so Main routes it
// before the front door: no banner, no update notice, nothing but the helper's reply.
// This fails if the early route in Main is deleted, because the registry path prints
// the startup banner to stderr, which the test requires to be empty.
func TestGitRemoteRoutedBeforeTheFrontDoor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(gitremote.EndpointEnv, "")
	t.Chdir(t.TempDir())

	in := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(in, []byte("capabilities\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inF, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer inF.Close()
	oldIn := os.Stdin
	os.Stdin = inF
	defer func() { os.Stdin = oldIn }()

	rc, out, errOut := runMainCaptured(t, "git-remote", "--")
	if rc != 0 || out != "connect\n\n" || errOut != "" {
		t.Fatalf("rc %d stdout %q stderr %q", rc, out, errOut)
	}
}

func TestGitRemoteHelp(t *testing.T) {
	rc, out, errOut := runMainCaptured(t, "git-remote", "--help")
	if rc != 0 || strings.TrimSpace(out) != strings.TrimSpace(gitRemoteUsage) || errOut != "" {
		t.Fatalf("rc %d out %q err %q", rc, out, errOut)
	}
}
