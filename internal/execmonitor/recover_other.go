//go:build !linux

package execmonitor

import (
	"errors"
	"syscall"
	"time"
)

// Runner runs one selected Podman command.
type Runner func(argv []string, timeout time.Duration) (string, bool, int)

// Ops is unsupported outside Linux.
type Ops struct {
	ProcRoot   string
	UID        int
	UIDSet     bool
	SelfPID    int
	PidfdOpen  func(pid int) (int, error)
	SendSignal func(fd int, sig syscall.Signal) error
	ClosePidfd func(fd int) error
}

// Recover always declines outside Linux.
func Recover(string, Runner, Ops) (bool, error) {
	return false, errors.New("exec-monitor recovery is Linux-only")
}
