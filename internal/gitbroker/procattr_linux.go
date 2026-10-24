package gitbroker

import "syscall"

// childProcAttr puts the host ssh in a process group of its own, so ending a call kills
// everything it started (startUpstream's Cancel), and has the kernel SIGKILL it if the
// broker dies first (SIGKILL, OOM), so no upstream connection outlives the broker relaying
// it. The launcher's stop signal to the broker's group does not reach it; the daemon's
// cancelled context does.
func childProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
