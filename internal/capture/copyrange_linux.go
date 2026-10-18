//go:build linux

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// copyrange_linux.go is the chain's KERNEL COPY arm: copy_file_range(2) over the whole file,
// tried once FICLONE has been retired, and the FIEMAP read that says what it did.
//
// # Why a second try after FICLONE refused
//
// The two calls have different predicates. FICLONE refused a pair of files on two vfsmounts with
// EXDEV, even within one superblock, until Linux 5.18 ("fs: allow cross-vfsmount
// reflink/dedupe"), and a jail's store and home are always two mounts. copy_file_range has
// allowed two mounts since 5.3, and within one superblock the VFS hands it to the filesystem's
// remap op first: on btrfs and XFS that IS a reflink. Across two superblocks, or on a filesystem
// with no remap op (ext4), it copies the bytes in the kernel instead.
//
// # Why the result is read back
//
// copy_file_range does not say which of the two it did. So the arm never claims a reflink on the
// call's word: it reads the destination's extent map (FS_IOC_FIEMAP) and counts the file as cloned
// only when its data extents are flagged SHARED. The read is unsynced, because a reflink is in the
// extent map when the call returns and a kernel copy's delayed allocation shows as an unshared
// extent; only a non-empty file that maps no extent at all is read again with FIEMAP_FLAG_SYNC,
// so that a copy still in the page cache is never mistaken for a file with no data. Anything else is reported as "cloned or copied by the kernel",
// which is the whole of what is known.

// copyRangeAvailable: Linux has the arm.
const copyRangeAvailable = true

// copyRangeFile is the kernel copy arm on Linux (copyRangeOne): dst, which must not exist, is
// filled by copy_file_range(2) from src, given perm, and judged by its extent map.
//
// A "this call cannot work here" errno on the FIRST call, before any byte moved, is
// errCopyRangeUnsupported and leaves no file behind, so the chain can retire the arm and fall to
// the arms below it. A failure after bytes moved is a fact about this file and fails it.
func copyRangeFile(src, dst string, perm fs.FileMode) (copyRangeVerdict, error) {
	sf, err := os.Open(src)
	if err != nil {
		return rangeUnknown, err
	}
	defer sf.Close()
	fi, err := sf.Stat()
	if err != nil {
		return rangeUnknown, err
	}
	df, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return rangeUnknown, err
	}
	if cerr := copyRange(df, sf, fi.Size()); cerr != nil {
		df.Close()
		_ = os.Remove(dst)
		return rangeUnknown, cerr
	}
	verdict, mapped := extentVerdict(df, 0)
	if !mapped && fi.Size() > 0 {
		verdict, mapped = extentVerdict(df, fiemapFlagSync)
		if !mapped && verdict == rangeNoData {
			verdict = rangeUnknown
		}
	}
	if cerr := df.Close(); cerr != nil {
		_ = os.Remove(dst)
		return rangeUnknown, cerr
	}
	return verdict, os.Chmod(dst, perm)
}

// copyRange moves size bytes from src to dst with copy_file_range, from offset zero.
//
// The "not here" errnos are the ones Go's own os.File.ReadFrom treats as "fall back": ENOSYS for
// a kernel without the call, EXDEV for a pair it will not cross, EOPNOTSUPP and EINVAL for a
// filesystem or file that cannot take it, EPERM for a seccomp filter, EIO for CIFS. They count
// only before the first byte; after it the copy is half-made and the error is the file's.
// A zero-byte answer at offset zero for a non-empty source is the pre-5.19 kernels' silent
// refusal, and is "not here" too.
func copyRange(dst, src *os.File, size int64) error {
	var done int64
	for done < size {
		chunk := size - done
		if chunk > 1<<30 {
			chunk = 1 << 30
		}
		n, err := unix.CopyFileRange(int(src.Fd()), nil, int(dst.Fd()), nil, int(chunk), 0)
		if err != nil {
			if done == 0 && copyRangeNotHere(err) {
				return fmt.Errorf("%w: %v", errCopyRangeUnsupported, err)
			}
			return fmt.Errorf("copy_file_range: %w", err)
		}
		if n == 0 {
			if done == 0 {
				return fmt.Errorf("%w: it copied nothing", errCopyRangeUnsupported)
			}
			return fmt.Errorf("copy_file_range: the source ended at %d of %d bytes", done, size)
		}
		done += int64(n)
	}
	return nil
}

func copyRangeNotHere(err error) bool {
	switch {
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EXDEV), errors.Is(err, unix.EOPNOTSUPP),
		errors.Is(err, unix.EINVAL), errors.Is(err, unix.EPERM), errors.Is(err, unix.EIO):
		return true
	}
	return false
}

// The FIEMAP ABI (linux/fiemap.h). golang.org/x/sys/unix does not carry it. The request number
// is _IOWR('f', 11, struct fiemap), whose 32-byte header gives one value on every architecture
// Linux has, the ones with a different _IOC layout included.
const (
	fsIocFiemap        = 0xC020660B
	fiemapFlagSync     = 0x1
	fiemapExtentLast   = 0x1
	fiemapExtentInline = 0x200
	fiemapExtentShared = 0x2000
	fiemapBatch        = 64
)

type fiemapExtent struct {
	Logical, Physical, Length uint64
	_                         [2]uint64
	Flags                     uint32
	_                         [3]uint32
}

type fiemapRequest struct {
	Start, Length        uint64
	Flags, Mapped, Count uint32
	_                    uint32
	Extents              [fiemapBatch]fiemapExtent
}

// extentVerdict reads f's extent map, a batch at a time, stopping at the first unshared extent.
// mapped says whether any extent was mapped at all, inline ones included.
func extentVerdict(f *os.File, flags uint32) (verdict copyRangeVerdict, mapped bool) {
	sawShared := false
	var start uint64
	for {
		req := fiemapRequest{Start: start, Length: ^uint64(0) - start, Flags: flags, Count: fiemapBatch}
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), fsIocFiemap, uintptr(unsafe.Pointer(&req))); errno != 0 {
			return rangeUnknown, mapped
		}
		if req.Mapped == 0 {
			break
		}
		mapped = true
		last := false
		for _, e := range req.Extents[:req.Mapped] {
			switch {
			case e.Flags&fiemapExtentInline != 0:
			case e.Flags&fiemapExtentShared != 0:
				sawShared = true
			default:
				return rangeUnshared, true
			}
			if e.Flags&fiemapExtentLast != 0 {
				last = true
			}
			start = e.Logical + e.Length
		}
		if last || req.Mapped < fiemapBatch {
			break
		}
	}
	if sawShared {
		return rangeShared, true
	}
	return rangeNoData, mapped
}
