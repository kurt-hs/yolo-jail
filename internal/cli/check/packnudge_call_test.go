package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// TestCheckCallsThePackNudge runs the whole Check over a user config whose [allow.aws-auth] table
// names a pack `packs` does not list, and expects the pack switch's finding and its `yolo pack
// add` next step. The section tests call sectionPackNudge directly; deleting its call in Check
// leaves them green, and fails this one.
func TestCheckCallsThePackNudge(t *testing.T) {
	var out bytes.Buffer
	o := baseOptions(t, &out)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	dir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(`{"allow": {"aws-auth": {"profile": "dev"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o.readFile = os.ReadFile
	o.PathExists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	o.LookPath = func(name string) (string, bool) { return "/usr/bin/" + name, name == "podman" }
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		if strings.Join(argv, " ") == "podman --version" {
			return ExecResult{Ran: true, RC: 0, Stdout: "podman version 6.1.2"}
		}
		return ExecResult{Ran: false}
	}
	scriptedPodman(&o, runtime.Attempt{Exited: true, RC: 0, Stdout: `{"host":{}}`})
	nudge, _, _ := nudgeOptions(nil, "", nil)
	o.ListedPacks, o.AllowFacts = nudge.ListedPacks, nudge.AllowFacts

	Check(o)
	got := stripANSI(out.String())
	for _, want := range []string{"Pack switch", "yolo pack add aws-auth"} {
		if !strings.Contains(got, want) {
			t.Errorf("Check's output lacks %q (is sectionPackNudge still called?):\n%s", want, got)
		}
	}
}
