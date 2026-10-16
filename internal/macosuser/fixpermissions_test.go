package macosuser

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// Only a file's owner (or root) may change its access list on macOS, and the walk runs as the
// invoking user, so every find that runs `chmod -h +a` must leave out what the sandbox account
// owns: those files were created under the inheriting entries and already carry the grant.
func TestFixPermissionsScriptSkipsWhatTheSandboxAccountOwns(t *testing.T) {
	script := FixPermissionsScript("/Users/Shared/yolo", "")
	if !strings.Contains(script, "/usr/bin/id -u "+SandboxUser) {
		t.Errorf("the walk does not ask whether %s exists:\n%s", SandboxUser, script)
	}
	if !strings.Contains(script, "skip=('!' -user "+shQuote(SandboxUser)+")") {
		t.Errorf("the walk builds no owner exclusion:\n%s", script)
	}
	walks := 0
	for _, line := range strings.Split(script, "\n") {
		if !strings.Contains(line, "-exec chmod -h +a") {
			continue
		}
		walks++
		if !strings.Contains(line, `${skip[@]+"${skip[@]}"}`) {
			t.Errorf("a walk applies the entry to files %s owns:\n%s", SandboxUser, line)
		}
	}
	if walks != 3 {
		t.Errorf("walks = %d, want 3:\n%s", walks, script)
	}
}

// The script is bash that parses, so the exclusion's spelling cannot break the whole walk.
func TestFixPermissionsScriptParses(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	cmd := exec.Command(bash, "-n", "-c", FixPermissionsScript("/Users/Shared/yolo", ""))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
}

// When some entries still cannot be applied, setup names how to see and fix the cause, not a
// re-run of the same walk that failed.
func TestSetupNamesTheCauseWhenTheACLWalkFails(t *testing.T) {
	var rec []string
	var buf bytes.Buffer
	d := mockDeps(&rec)
	d.Out = &buf
	d.RunBash = func(string) int { return 1 }
	MacosSetup(d)
	out := buf.String()
	if strings.Contains(out, "to see which") {
		t.Errorf("still sends the user to re-run the walk to see which:\n%s", out)
	}
	for _, want := range []string{"chmod named each", "ls -leO", "yolo macos-fix-permissions"} {
		if !strings.Contains(out, want) {
			t.Errorf("ACL warning lacks %q:\n%s", want, out)
		}
	}
}
