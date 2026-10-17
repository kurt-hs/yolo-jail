package capture

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// InstallerStatusLeaf is the file name a build's driver records its build line's exit status under,
// beside the toolchain record in the build's workspace (Options.StatusFile): one name for the
// container build jail and the macos-user build act, so the host reads both the same way.
const InstallerStatusLeaf = "line-status"

// InstallerExitStatus is the exit status install's error says the installer had: 0 for nil, its
// exit code, 128 plus the signal for one a signal ended, as a shell says it, and 127 for an
// installer that could not be started.
func InstallerExitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		if c := ee.ExitCode(); c >= 0 {
			return c
		}
	}
	return 127
}

// WriteInstallerStatus records status at path, as ReadInstallerStatus reads it.
func WriteInstallerStatus(path string, status int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(status)+"\n"), 0o644)
}

// ReadInstallerStatus is the installer's exit status recorded at path; ok is false when there is no
// record or it does not hold one.
func ReadInstallerStatus(path string) (status int, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
