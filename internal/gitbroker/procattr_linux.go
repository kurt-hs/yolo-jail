package gitbroker

import "syscall"

// childProcAttr has the kernel SIGKILL the host ssh if the broker dies first (SIGKILL, OOM),
// so no upstream connection outlives the broker relaying it. The ssh stays in the broker's
// process group, so the launcher's stop signal to that group reaches it too.
func childProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
