//go:build linux

package entrypoint

import "golang.org/x/sys/unix"

func renameNoReplaceBetweenAt(oldDirFD int, old string, newDirFD int, new string) error {
	return unix.Renameat2(oldDirFD, old, newDirFD, new, unix.RENAME_NOREPLACE)
}
