package brokerhook

import "syscall"

// hookProcAttr puts each hook in a process group of its own, so a stop ends whatever it
// started too, and has the kernel SIGKILL it if the broker dies first (SIGKILL, OOM), so no
// hook outlives the broker whose request it is answering.
func hookProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
