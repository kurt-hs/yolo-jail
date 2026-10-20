//go:build darwin

package nativehome

import (
	"golang.org/x/sys/unix"
	"os"
)

// AccountIdle reads only this account's process identities, not host environments or
// another user's processes. A reparented/detached child retains this uid.
func AccountIdle() (bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Geteuid())
	if err != nil {
		return false, err
	}
	for _, p := range procs {
		if int(p.Proc.P_pid) != os.Getpid() {
			return false, nil
		}
	}
	return true, nil
}
