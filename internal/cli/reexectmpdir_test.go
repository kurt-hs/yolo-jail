package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A child of this test binary runs TestMain again, and TestMain's isolateTheStagedTree makes a
// yolo-cli-staged-tree-* directory it removes only after m.Run returns. A helper child exits,
// execs away or is killed before that, so unless it runs under a TMPDIR the parent test owns,
// each run leaks one directory: 94 had piled up in a developer Mac's TMPDIR by 2026-10-23.
// Every re-exec of os.Args[0] in this package must therefore set TMPDIR within the dozen lines
// after it.
func TestEveryTestBinaryReexecGetsItsOwnTMPDIR(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	reexec := regexp.MustCompile(`exec\.Command(Context)?\([^,]*os\.Args\[0\]`)
	found := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		for i, line := range lines {
			if !reexec.MatchString(line) || strings.HasPrefix(strings.TrimSpace(line), "//") ||
				strings.Contains(line, "reexec.MustCompile") {
				continue
			}
			found++
			end := min(len(lines), i+13)
			if !strings.Contains(strings.Join(lines[i:end], "\n"), `"TMPDIR=`) {
				t.Errorf("%s:%d re-execs the test binary without a TMPDIR of the test's own, so the "+
					"child's staged tree leaks into the machine's TMPDIR:\n\t%s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
	if found == 0 {
		t.Fatal("found no re-exec of os.Args[0]; the pattern no longer matches this package's spelling")
	}
}
