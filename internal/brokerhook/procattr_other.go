//go:build !linux

package brokerhook

import "syscall"

// hookProcAttr puts each hook in a process group of its own, so a stop ends whatever it
// started too. There is no parent-death signal here: a hook whose broker was killed
// outright runs until it finishes, and its answer then reaches no one: only the broker
// reads it.
func hookProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
