//go:build !linux

package gitbroker

import "syscall"

// childProcAttr puts the host ssh in a process group of its own, so ending a call kills
// everything it started (startUpstream's Cancel). There is no parent-death signal here: an
// ssh whose broker was killed outright runs until its next write fails (the broker's end of
// its pipes is gone) or it finishes.
func childProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
