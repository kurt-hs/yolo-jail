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
	firstLine := version.Line(repoRes.Root)

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
	want := version.Line(repoRes.Root) + "\n"

	var rc int
	out := captureStdout(t, func() { rc = Main([]string{"yolo", "--version"}) })
	if rc != 0 {
		t.Fatalf("yolo --version rc = %d, want 0", rc)
	}
	if out != want {
		t.Fatalf("plain --version = %q, want exactly %q", out, want)
	}
}

// TestPlainVersionNamesTheBinaryBesideYOLOVersion pins the plain line THROUGH THE FRONT DOOR
// when YOLO_VERSION names another version: the override first, then the binary's own version,
// so the line a person quotes names the build that printed it. A jail sets the variable to its
// launcher's version, which prints the plain line when the two builds agree.
func TestPlainVersionNamesTheBinaryBesideYOLOVersion(t *testing.T) {
	repoRes, _ := reporoot.Resolve(os.Getenv)
	t.Setenv("YOLO_VERSION", "")
	binary := version.Binary(repoRes.Root)

	t.Setenv("YOLO_VERSION", "9.9.9")
	var rc int
	out := captureStdout(t, func() { rc = Main([]string{"yolo", "--version"}) })
	if want := "yolo-jail 9.9.9 (YOLO_VERSION override; binary " + binary + ")\n"; rc != 0 || out != want {
		t.Errorf("--version with YOLO_VERSION=9.9.9: rc=%d out=%q, want %q", rc, out, want)
	}

	t.Setenv("YOLO_VERSION", binary)
	out = captureStdout(t, func() { rc = Main([]string{"yolo", "--version"}) })
	if want := "yolo-jail " + binary + "\n"; rc != 0 || out != want {
		t.Errorf("--version with YOLO_VERSION naming the binary's own version: rc=%d out=%q, want %q", rc, out, want)
	}
}
