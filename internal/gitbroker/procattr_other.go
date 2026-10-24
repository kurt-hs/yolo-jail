//go:build !linux

package gitbroker

import "syscall"

// childProcAttr leaves the host ssh in the broker's process group, so the launcher's stop
// signal to that group reaches it. There is no parent-death signal here: an ssh whose broker
// was killed outright runs until its next write fails (the broker's end of its pipes is gone)
// or it finishes.
func childProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
