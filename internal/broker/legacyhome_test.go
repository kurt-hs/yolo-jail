package broker

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// legacyhome_test.go pins LegacyCopyServesHome (legacyhome.go): a machine-wide copy is this
// home's only while its process runs under this home, and a doubt reads as this home's.

// homeProcess starts a process that sleeps under HOME=home and writes deps' PID file naming it.
func homeProcess(t *testing.T, deps Deps, home string) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := os.WriteFile(deps.PIDFilePath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestALegacyCopyServesTheHomeItRunsUnder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads a process's HOME through /proc")
	}
	dir := t.TempDir()
	deps := Deps{PIDFilePath: filepath.Join(dir, "d.pid")}
	mine, theirs := t.TempDir(), t.TempDir()
	homeProcess(t, deps, mine)
	if !LegacyCopyServesHome(deps, mine) {
		t.Error("a copy running under this home does not read as this home's")
	}
	if LegacyCopyServesHome(deps, theirs) {
		t.Error("a copy running under ANOTHER home reads as this home's, so a launch here would " +
			"refuse over a copy that shares none of its credentials")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(mine, link); err != nil {
		t.Fatal(err)
	}
	if !LegacyCopyServesHome(deps, link) {
		t.Error("this home reached through a symlink does not read as itself")
	}
}

// TestALegacyCopyWhoseHomeCannotBeReadIsTakenAsThisHomes: no PID file, or a PID whose environment
// cannot be read, is the doubt, and the doubt refuses.
func TestALegacyCopyWhoseHomeCannotBeReadIsTakenAsThisHomes(t *testing.T) {
	dir := t.TempDir()
	deps := Deps{PIDFilePath: filepath.Join(dir, "d.pid")}
	if !LegacyCopyServesHome(deps, t.TempDir()) {
		t.Error("a copy with no PID file does not read as this home's")
	}
	if err := os.WriteFile(deps.PIDFilePath, []byte("999999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !LegacyCopyServesHome(deps, t.TempDir()) {
		t.Error("a copy whose environment cannot be read does not read as this home's")
	}
}
