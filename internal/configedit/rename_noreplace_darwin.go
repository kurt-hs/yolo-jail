//go:build darwin

package configedit

import "golang.org/x/sys/unix"

func renameNoReplaceAt(dirfd int, old, new string) error {
	return unix.RenameatxNp(dirfd, old, dirfd, new, unix.RENAME_EXCL)
}
