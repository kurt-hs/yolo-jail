package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/reporoot"
	"github.com/mschulkind-oss/yolo-jail/internal/version"
)

// TestVersionFullAddsCommitAndBinaryDigest pins `yolo --version --full`: the one-line version
// first, then the commit and a 64-hex sha256 of the running binary. Before this, --full was
// ignored and printed the same single line as plain --version.
func TestVersionFullAddsCommitAndBinaryDigest(t *testing.T) {
	repoRes, _ := reporoot.Resolve(os.Getenv)
	firstLine := "yolo-jail " + version.Get(repoRes.Root)

	var rc int
	out := captureStdout(t, func() { rc = Main([]string{"yolo", "--version", "--full"}) })
	if rc != 0 {
		t.Fatalf("yolo --version --full rc = %d, want 0", rc)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("--full printed %d lines, want the version line plus at least three fields:\n%s", len(lines), out)
	}
	if lines[0] != firstLine {
		t.Errorf("first line = %q, want %q", lines[0], firstLine)
	}
	var commitSeen, digestSeen bool
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "commit: ") && len(l) > len("commit: ") {
			commitSeen = true
		}
		if sha, ok := strings.CutPrefix(l, "binary sha256: "); ok {
			digestSeen = regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sha)
		}
	}
	if !commitSeen {
		t.Errorf("--full has no commit: line:\n%s", out)
	}
	if !digestSeen {
		t.Errorf("--full has no 64-hex binary sha256: line:\n%s", out)
	}
}

// TestPlainVersionIsOneLineAndUnchanged pins that plain --version still prints exactly the
// one line other tools parse.
func TestPlainVersionIsOneLineAndUnchanged(t *testing.T) {
	repoRes, _ := reporoot.Resolve(os.Getenv)
	want := "yolo-jail " + version.Get(repoRes.Root) + "\n"

	var rc int
	out := captureStdout(t, func() { rc = Main([]string{"yolo", "--version"}) })
	if rc != 0 {
		t.Fatalf("yolo --version rc = %d, want 0", rc)
	}
	if out != want {
		t.Fatalf("plain --version = %q, want exactly %q", out, want)
	}
}
