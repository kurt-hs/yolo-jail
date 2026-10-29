package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/gitbroker"
)

// Every `yolo …` command a git push refusal names as its next step is one this CLI runs: its
// subcommand is registered, and `yolo approve` takes the arguments the step gives it, reaching
// the store (an unknown request is exit 4, never a usage error).
func TestGitRefusalNextStepsNameRealCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_USER_LAYER", "")
	cmd := regexp.MustCompile("`(yolo [^`]*)`")
	seen := map[string]bool{}
	for _, step := range gitbroker.RefusalNextSteps() {
		for _, m := range cmd.FindAllStringSubmatch(step, -1) {
			seen[m[1]] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("no next step names a yolo command")
	}
	for line := range seen {
		args := strings.Fields(strings.ReplaceAll(line, "<id>", "r-ffffffffffffffffffffffffffffffff"))
		run, ok := registry[args[1]]
		if !ok {
			t.Errorf("%q names a subcommand this CLI does not have", line)
			continue
		}
		if args[1] != "approve" {
			continue
		}
		want := 0
		if len(args) > 2 {
			want = approveExitUnknownID
		}
		rc, out, errs := captureStd(t, func() int { return run(args[1:]) })
		if rc != want {
			t.Errorf("%q exited %d, want %d:\n%s%s", line, rc, want, out, errs)
		}
	}
}
