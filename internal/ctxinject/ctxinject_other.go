//go:build !linux

package ctxinject

import (
	"errors"
	"path/filepath"
)

// errNotLinux: mount injection is Linux's alone. A macOS Podman Machine's injector would run the
// Linux build inside the VM (design §5.3, M3), which is not built.
var errNotLinux = errors.New("live context mounts are injected on Linux only")

// ReadProcessIdentity is not available off Linux.
func ReadProcessIdentity(int) (ProcessIdentity, error) {
	return ProcessIdentity{}, errNotLinux
}

// Attach is not built off Linux.
func Attach(pid int, expected ProcessIdentity, source, dest string, rw bool) error {
	if _, err := checkArgs(pid, dest); err != nil {
		return err
	}
	if !expected.Valid() {
		return errors.New("the target process identity is missing")
	}
	if !filepath.IsAbs(source) {
		return errors.New("source " + quote(source) + " must be an absolute path")
	}
	return errNotLinux
}

// Detach is not built off Linux.
func Detach(pid int, expected ProcessIdentity, dest string) error {
	if _, err := checkArgs(pid, dest); err != nil {
		return err
	}
	if !expected.Valid() {
		return errors.New("the target process identity is missing")
	}
	return errNotLinux
}

// DieWithParent is a no-op off Linux, where nothing is injected.
func DieWithParent() error { return nil }
