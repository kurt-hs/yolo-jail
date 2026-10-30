//go:build darwin

package macosuser

import "golang.org/x/sys/unix"

// renameNoReplace renames old to new, failing when new exists, even as an empty folder.
func renameNoReplace(old, new string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, old, unix.AT_FDCWD, new, unix.RENAME_EXCL)
}
