//go:build darwin

package capture

import "io/fs"

// copyRangeAvailable: darwin has no copy_file_range arm. clonefile(2) is the reflink arm there,
// and a copy is the copy arm's, so a chain starts with this arm retired and its report never
// names a Linux call.
const copyRangeAvailable = false

// copyRangeFile is never reached on darwin (copyRangeAvailable); it answers "not here".
func copyRangeFile(src, dst string, perm fs.FileMode) (copyRangeVerdict, error) {
	return rangeUnknown, errCopyRangeUnsupported
}
