//go:build darwin

package macosuser

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// darwinSZOMB is SZOMB from <sys/proc.h>: a zombie has closed every file it held.
const darwinSZOMB = 5

// guestProcessesReal is Deps.GuestProcesses on macOS: every process on the machine, from the
// kern.proc.all sysctl, which needs no privilege — its pid, parent, start time, and whether its
// real, effective or saved uid is the sandbox account's. Nothing here needs root: the host user
// cannot read another account's arguments or open files, but every process's ids and start time
// are public (ps prints them for any user).
func guestProcessesReal() ([]GuestProc, error) {
	uid, err := sandboxUIDReal()
	if err != nil {
		return nil, err
	}
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("kern.proc.all: %w", err)
	}
	out := make([]GuestProc, 0, len(procs))
	for i := range procs {
		kp := &procs[i]
		pid := int(kp.Proc.P_pid)
		if pid < 0 || kp.Proc.P_stat == darwinSZOMB {
			continue
		}
		sec, nsec := kp.Proc.P_starttime.Unix()
		out = append(out, GuestProc{
			PID:  pid,
			PPID: int(kp.Eproc.Ppid),
			Sandbox: kp.Eproc.Pcred.P_ruid == uid || kp.Eproc.Ucred.Uid == uid ||
				kp.Eproc.Pcred.P_svuid == uid,
			Start: time.Unix(sec, nsec),
		})
	}
	return out, nil
}

// sandboxUID caches the account's uid once read: a finishing launch reads the table every few
// milliseconds while it waits for its guest, and the uid does not change under it.
var sandboxUID struct {
	sync.Mutex
	uid   uint32
	known bool
}

// sandboxUIDReal is the sandbox account's uid through `/usr/bin/id -u`, which asks the directory
// service (os/user's pure-Go path reads /etc/passwd, which a Mac's accounts are not in).
func sandboxUIDReal() (uint32, error) {
	sandboxUID.Lock()
	defer sandboxUID.Unlock()
	if sandboxUID.known {
		return sandboxUID.uid, nil
	}
	out, rc := outputWithTimeout(exec.Command("/usr/bin/id", "-u", "--", SandboxUser), 10*time.Second)
	if rc != 0 {
		return 0, fmt.Errorf("`/usr/bin/id -u %s` exited %d", SandboxUser, rc)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(out), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("`/usr/bin/id -u %s` printed %q", SandboxUser, strings.TrimSpace(out))
	}
	sandboxUID.uid, sandboxUID.known = uint32(n), true
	return sandboxUID.uid, nil
}
