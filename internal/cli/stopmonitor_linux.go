//go:build linux

package cli

import (
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/execmonitor"
)

// stopMonitorProcessOps are the fake-proc and fake-pidfd seams used by caller tests. The zero value
// selects the real Linux process identity operations only at runtime.
var stopMonitorProcessOps execmonitor.Ops

func stopExecMonitorRecovery(rt, name string, run stopCommandRunner) bool {
	if rt != "podman" {
		return false
	}
	probe := func(argv []string, timeout time.Duration) (string, bool, int) {
		if len(argv) == 0 || argv[0] != rt {
			return "", false, 1
		}
		return run(argv, timeout)
	}
	recovered, _ := execmonitor.Recover(name, probe, stopMonitorProcessOps)
	return recovered
}
