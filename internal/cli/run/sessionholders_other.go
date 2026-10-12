//go:build !linux

package run

// sessionholders_other.go: a host without /proc/locks (macOS) cannot name a lock's holders. The
// holder is real but unnamed, and a caller must never read that as none (JL-P3).

import "os"

// procLockHolders names no holder off Linux.
func procLockHolders(*os.File) []lockHolder { return nil }
