//go:build linux

package run

// sessionholders_linux.go names a lock's holders from /proc/locks, the one lock table a jail's host
// runs on. The session lock is held with flock(2), so only FLOCK entries answer.

import (
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// userHZ is the divisor /proc expresses a process's start time in. /proc always uses USER_HZ, which
// is 100 on Linux regardless of the kernel's own HZ (proc(5), field 22 of /proc/<pid>/stat).
const userHZ = 100

// procLockHolders names the processes holding an flock on f's file, read from /proc/locks. Every
// line there is "<id>: <TYPE> <KIND> <ACCESS> <pid> <maj:min:inode> <start> <end>" (proc(5)), the
// device in hex and the inode in decimal, the pid the holder's; the session lock is held with
// flock(2), so only FLOCK lines answer. comm and start time come from the holder's own /proc entry,
// and are left out when that cannot be read. An unreadable /proc yields nothing, which its caller
// reads as an unnamed holder, never as none (JL-P3).
func procLockHolders(f *os.File) []lockHolder {
	want := kernelLockIdentity(f)
	if want == "" {
		return nil
	}
	data, err := os.ReadFile("/proc/locks")
	if err != nil {
		return nil
	}
	boot := bootTime()
	var out []lockHolder
	seen := map[int]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[1] != "FLOCK" || fields[5] != want {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil || pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, holderFromProc(pid, boot))
	}
	return out
}

// kernelLockIdentity reads the identity the kernel's lock table uses. On overlayfs
// st_dev can name the overlay while a flock names its backing inode's device.
// f is the unlocked diagnostic descriptor, not a session's held descriptor. A
// nonblocking shared take lets fdinfo name that exact inode without guessing by
// inode number alone. Release it BEFORE reading /proc/locks so the diagnostic
// cannot name itself as a remaining session or keep the jail alive afterward.
func kernelLockIdentity(f *os.File) string {
	fd := int(f.Fd())
	if err := unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return ""
	}
	data, err := os.ReadFile("/proc/self/fdinfo/" + strconv.Itoa(fd))
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	if err != nil || unlockErr != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 7 && fields[0] == "lock:" && fields[2] == "FLOCK" {
			return fields[6]
		}
	}
	return ""
}

// bootTime reads /proc/stat's btime line, the base a process's starttime is measured from, or the
// zero time when it cannot be read.
func bootTime() time.Time {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(secs, 0)
			}
		}
	}
	return time.Time{}
}

// holderFromProc names pid, reading its comm and start time from its own /proc entry. A process that
// ended before the read leaves whatever could be read: the pid at least, which is the fact the
// caller acts on.
func holderFromProc(pid int, boot time.Time) lockHolder {
	h := lockHolder{PID: pid}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		h.Comm = strings.TrimSpace(string(b))
	}
	if boot.IsZero() {
		return h
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return h
	}
	// comm is parenthesized and may hold spaces and parens, so the numbered fields begin after the
	// last ')': the first field there is number 3, and starttime is number 22.
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return h
	}
	fields := strings.Fields(s[i+1:])
	const starttimeField, firstAfterComm = 22, 3
	if len(fields) <= starttimeField-firstAfterComm {
		return h
	}
	ticks, err := strconv.ParseInt(fields[starttimeField-firstAfterComm], 10, 64)
	if err != nil || ticks < 0 {
		return h
	}
	h.Since = boot.Add(time.Duration(ticks) * time.Second / userHZ)
	return h
}
