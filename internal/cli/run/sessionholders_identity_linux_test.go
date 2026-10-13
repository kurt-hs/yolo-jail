//go:build linux

package run

import (
	"os"
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
