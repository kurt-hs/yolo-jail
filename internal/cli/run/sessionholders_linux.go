//go:build linux

package run

// sessionholders_linux.go names a lock's holders from /proc/locks, the one lock table a jail's host
// runs on. The session lock is held with flock(2), so only FLOCK entries answer.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
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
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	want := lockDevInode(unix.Major(uint64(st.Dev)), unix.Minor(uint64(st.Dev)), st.Ino)
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

// lockDevInode is /proc/locks' "<maj:min:inode>" spelling of a lock's file: the device in hex, the
// inode in decimal, which is what st_dev and st_ino resolve to.
func lockDevInode(major, minor uint32, ino uint64) string {
	return fmt.Sprintf("%02x:%02x:%d", major, minor, ino)
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
