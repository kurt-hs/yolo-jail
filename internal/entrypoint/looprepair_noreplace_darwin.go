//go:build darwin

package entrypoint

import "golang.org/x/sys/unix"

func renameNoReplaceBetweenAt(oldDirFD int, old string, newDirFD int, new string) error {
	return unix.RenameatxNp(oldDirFD, old, newDirFD, new, unix.RENAME_EXCL)
}
