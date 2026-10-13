// Package brokerlock serializes scope authority and request/grant state per source.
package brokerlock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// WithTransaction holds dir/.lock exclusively. Callbacks must not call another
// lock-taking API for this directory; reads and current-policy gates do not lock.
// All cooperating authority writers must use this same source directory.
func WithTransaction(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("broker directory is not real")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, statErr := f.Stat()
	if statErr != nil || !fi.Mode().IsRegular() {
		return errors.New("store lock is not a regular file")
	}
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
