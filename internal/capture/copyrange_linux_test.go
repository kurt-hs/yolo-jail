//go:build linux

package capture

// copyrange_linux_test.go pins the copy_file_range arm (copyrange_linux.go): reached when FICLONE
// answers EXDEV, which a kernel before 5.18 does for a store and a home on two mounts of one
// filesystem, and counted as a reflink only when the extent map says so.
//
// FICLONE is answered through its seam (ficlone) so the refusal goes through reflinkFile's own
// call and the real chain; copy_file_range and FIEMAP are the kernel's, on t.TempDir's
// filesystem. Where that is btrfs the arm must clone, and the btrfs halves fail if it does not;
// elsewhere they skip, naming the filesystem.

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// bigBody is large enough that btrfs stores it in data extents rather than inline in its
// metadata, so the extent map has something to say.
var bigBody = strings.Repeat("0123456789abcdef", 1<<16) // 1 MiB

// bigEntry is an admitted entry holding one 1 MiB file and one small one.
func bigEntry(t *testing.T) *Entry {
	t.Helper()
	return craftedEntry(t, map[string]string{
		".local/share/big/blob":  bigBody,
		".local/share/big/small": "small\n",
	}, nil, []ManifestEntry{
		dirEntry(".local"), dirEntry(".local/share"), dirEntry(".local/share/big"),
		{Path: ".local/share/big/blob", Kind: KindFile, Mode: "0755", Size: int64(len(bigBody))},
		{Path: ".local/share/big/small", Kind: KindFile, Mode: "0644", Size: 6},
	})
}

// refuseFICLONE makes the FICLONE ioctl answer EXDEV, and hardlink EXDEV too, as two mounts do.
// It counts the copy_file_range arm's calls, which still reach the kernel.
func refuseFICLONE(t *testing.T) *int {
	t.Helper()
	oldF, oldH, oldC := ficlone, hardlinkOne, copyRangeOne
	ficlone = func(int, int) error { return unix.EXDEV }
	hardlinkOne = func(src, dst string) error {
		return &os.LinkError{Op: "link", Old: src, New: dst, Err: syscall.EXDEV}
	}
	calls := 0
	copyRangeOne = func(src, dst string, perm fs.FileMode) (copyRangeVerdict, error) {
		calls++
		return copyRangeFile(src, dst, perm)
	}
	t.Cleanup(func() { ficlone, hardlinkOne, copyRangeOne = oldF, oldH, oldC })
	return &calls
}

// When FICLONE answers EXDEV, copy_file_range places every file, the bytes arrive, and the
// report claims a reflink only as far as the extent map shows one.
func TestMaterializeTriesCopyFileRangeWhenFICLONEAnswersEXDEV(t *testing.T) {
	entry := bigEntry(t)
	home := filepath.Join(resolvedTempDir(t), "home")
	calls := refuseFICLONE(t)

	var errw bytes.Buffer
	res, err := Materialize(MaterializeOptions{Entry: entry, Home: home, Stderr: &errw})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if !strings.Contains(res.ReflinkRetired, "cross-device") {
		t.Errorf("reflink was not retired by FICLONE's EXDEV: %q", res.ReflinkRetired)
	}
	if res.RangeRetired != "" && strings.Contains(res.RangeRetired, "not supported") {
		t.Skipf("copy_file_range is not available on %s here: %s", orUnknown(fsName(home)), res.RangeRetired)
	}
	if *calls != 2 {
		t.Errorf("copy_file_range was attempted %d times, want once per file (2)", *calls)
	}
	if got := res.RangeCloned + res.RangeCopied + res.RangeUnverified; got != 2 {
		t.Fatalf("copy_file_range placed %d files, want 2 (%+v)", got, res)
	}
	for rel, want := range map[string]string{"blob": bigBody, "small": "small\n"} {
		got, err := os.ReadFile(filepath.Join(home, ".local", "share", "big", rel))
		if err != nil || string(got) != want {
			t.Errorf("%s does not hold the store's bytes (%d bytes, %v)", rel, len(got), err)
		}
	}
	if fi, err := os.Stat(filepath.Join(home, ".local", "share", "big", "blob")); err != nil ||
		fi.Mode().Perm() != 0o755 {
		t.Errorf("blob's mode is not the manifest's 0755 (%v)", err)
	}

	report := errw.String()
	if res.Copied == 0 {
		if report != "" {
			t.Errorf("nothing was copied, so nothing should be reported: %s", report)
		}
	} else if !strings.Contains(report, "copy_file_range") {
		t.Errorf("the copy report does not say copy_file_range placed the files:\n%s", report)
	}

	if kind := fsName(home); kind != "btrfs" {
		t.Logf("the temp dir is on %s, where copy_file_range need not clone: %+v", orUnknown(kind), res)
		return
	}
	// On btrfs, within one filesystem, copy_file_range reflinks, and the extent map says so.
	if res.RangeCloned != 2 || res.Reflinked != 2 || res.Copied != 0 || res.Mechanism() != "reflink" {
		t.Fatalf("on btrfs copy_file_range must clone both files: %+v", res)
	}
}

// The verdict is the extent map's, measured: shared after copy_file_range on btrfs, unshared after
// a user-space copy, and "no data" for a file btrfs keeps inline.
func TestExtentVerdictOnBtrfs(t *testing.T) {
	dir := btrfsTempDir(t)
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte(bigBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, err := copyRangeFile(src, filepath.Join(dir, "range"), 0o644); err != nil || v != rangeShared {
		t.Errorf("copy_file_range on one btrfs: verdict %d (%v), want rangeShared", v, err)
	}
	if err := copyOne(src, filepath.Join(dir, "copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "copy"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if v, _ := extentVerdict(f, fiemapFlagSync); v != rangeUnshared {
		t.Errorf("a user-space copy's verdict is %d, want rangeUnshared: copyOne must not be "+
			"handing the copy to the kernel", v)
	}
	small := filepath.Join(dir, "small")
	if err := os.WriteFile(small, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, err := copyRangeFile(small, filepath.Join(dir, "small-range"), 0o644); err != nil || v != rangeNoData {
		t.Errorf("a two-byte file: verdict %d (%v), want rangeNoData", v, err)
	}
}

// A file copy_file_range places UNSHARED demotes the arm below hardlink for the rest of the run:
// a hardlink costs no bytes and a kernel copy does. The report names the kernel copy.
func TestCopyFileRangeThatCopiesIsDemotedBelowHardlink(t *testing.T) {
	entry := bigEntry(t)
	home := filepath.Join(resolvedTempDir(t), "home")
	oldR, oldH, oldC := reflinkOne, hardlinkOne, copyRangeOne
	t.Cleanup(func() { reflinkOne, hardlinkOne, copyRangeOne = oldR, oldH, oldC })
	reflinkOne = func(string, string, fs.FileMode) error { return fmt.Errorf("%w: %w", errCloneUnsupported, unix.EXDEV) }
	links, ranges := 0, 0
	hardlinkOne = func(src, dst string) error { links++; return os.Link(src, dst) }
	copyRangeOne = func(src, dst string, perm fs.FileMode) (copyRangeVerdict, error) {
		ranges++
		return rangeUnshared, copyOne(src, dst, perm)
	}

	var errw bytes.Buffer
	res, err := Materialize(MaterializeOptions{Entry: entry, Home: home, Stderr: &errw})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if ranges != 1 || links != 1 || res.RangeCopied != 1 || res.Linked != 1 || res.Copied != 1 {
		t.Fatalf("want one kernel copy then one hardlink: %d range calls, %d link calls, %+v",
			ranges, links, res)
	}
	if !strings.Contains(res.RangeRetired, "without sharing its extents") {
		t.Errorf("the demotion was not recorded: %q", res.RangeRetired)
	}
	report := errw.String()
	if !strings.Contains(report, "1 of them were copied by the kernel") ||
		!strings.Contains(report, "1 of 2 files ("+"1048576 bytes)") {
		t.Errorf("the report does not name the kernel copy and its bytes:\n%s", report)
	}
}

// An arm that answers "not here" is asked once, and the user-space copy places the rest.
func TestCopyFileRangeNotHereIsRetiredForTheRun(t *testing.T) {
	entry := bigEntry(t)
	home := filepath.Join(resolvedTempDir(t), "home")
	oldR, oldH, oldC := reflinkOne, hardlinkOne, copyRangeOne
	t.Cleanup(func() { reflinkOne, hardlinkOne, copyRangeOne = oldR, oldH, oldC })
	reflinkOne = func(string, string, fs.FileMode) error { return fmt.Errorf("%w: %w", errCloneUnsupported, unix.EXDEV) }
	hardlinkOne = func(src, dst string) error {
		return &os.LinkError{Op: "link", Old: src, New: dst, Err: syscall.EXDEV}
	}
	ranges := 0
	copyRangeOne = func(string, string, fs.FileMode) (copyRangeVerdict, error) {
		ranges++
		return rangeUnknown, errCopyRangeUnsupported
	}
	var errw bytes.Buffer
	res, err := Materialize(MaterializeOptions{Entry: entry, Home: home, Stderr: &errw})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if ranges != 1 || res.Copied != 2 || res.RangeCopied+res.RangeUnverified+res.RangeCloned != 0 {
		t.Fatalf("want copy_file_range asked once and 2 user-space copies: %d calls, %+v", ranges, res)
	}
	if !strings.Contains(errw.String(), "copy_file_range is not supported here") {
		t.Errorf("the report does not say why copy_file_range was not used:\n%s", errw.String())
	}
}

// When FICLONE refuses for any reason but EXDEV, the filesystem cannot share extents, so hardlink
// keeps its place ahead of copy_file_range: an ext4 store and home on one mount still get all
// hardlinks and no copy report. The copy_file_range arm here is the real one.
func TestHardlinkStaysAheadOfCopyFileRangeWhenTheFilesystemCannotReflink(t *testing.T) {
	entry := bigEntry(t)
	home := filepath.Join(resolvedTempDir(t), "home")
	oldF, oldC := ficlone, copyRangeOne
	t.Cleanup(func() { ficlone, copyRangeOne = oldF, oldC })
	ficlone = func(int, int) error { return unix.EOPNOTSUPP }
	ranges := 0
	copyRangeOne = func(src, dst string, perm fs.FileMode) (copyRangeVerdict, error) {
		ranges++
		return copyRangeFile(src, dst, perm)
	}
	var errw bytes.Buffer
	res, err := Materialize(MaterializeOptions{Entry: entry, Home: home, Stderr: &errw})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if ranges != 0 || res.Linked != 2 || res.Copied != 0 || errw.Len() != 0 {
		t.Fatalf("want 2 hardlinks, no copy_file_range and no report: %d range calls, %+v\n%s",
			ranges, res, errw.String())
	}
}
