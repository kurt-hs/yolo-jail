//go:build linux

package run

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Exercise the production reader on the actual filesystem: overlayfs can expose
// a different st_dev from the backing inode named by the kernel lock table.
func TestProcLockHoldersUsesTheKernelFileIdentity(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "lock")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	probe, err := os.OpenFile(f.Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	holders := procLockHolders(probe)
	if len(holders) != 1 || holders[0].PID != os.Getpid() {
		t.Fatalf("holders = %v; want only pid %d", holders, os.Getpid())
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if got := procLockHolders(probe); len(got) != 0 {
		t.Fatalf("diagnostic probe fabricated holders: %v", got)
	}
	if got := readSessionLockHolder(probe); got != heldByNobody {
		t.Fatalf("diagnostic probe left a lock behind: %v", got)
	}
}

// TestProcLockHoldersNamesAHolderOneReadOfTheLockTableSkipped: /proc/locks is a seq_file the
// kernel hands out a buffer at a time, resuming each buffer by POSITION in its lock list. A lock
// listed ahead of a still-held one and released between two of one read's buffers moves the held
// one back past the resume point, and that read never shows it; on a busy host, which takes and
// releases flocks all the time, a held session lock went unnamed. The first read here is the real
// table with this process's lines dropped, as that skip leaves it; the holder must still be named
// (JL-P3).
func TestProcLockHoldersNamesAHolderOneReadOfTheLockTableSkipped(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "lock")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	probe, err := os.OpenFile(f.Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	real := readProcLocks
	t.Cleanup(func() { readProcLocks = real })
	reads := 0
	readProcLocks = func() ([]byte, error) {
		reads++
		data, err := real()
		if reads > 1 || err != nil {
			return data, err
		}
		var kept []string
		for _, line := range strings.Split(string(data), "\n") {
			if fields := strings.Fields(line); len(fields) > 4 && fields[4] == strconv.Itoa(os.Getpid()) {
				continue
			}
			kept = append(kept, line)
		}
		return []byte(strings.Join(kept, "\n")), nil
	}
	holders := procLockHolders(probe)
	if len(holders) != 1 || holders[0].PID != os.Getpid() {
		t.Fatalf("holders = %v after one read of /proc/locks skipped the holder; want only pid %d", holders, os.Getpid())
	}
}
