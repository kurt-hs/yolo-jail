//go:build !linux

package ctxinject

import (
	"errors"
	"path/filepath"
)

// errNotLinux: mount injection is Linux's alone. A macOS Podman Machine's injector would run the
// Linux build inside the VM (design §5.3, M3), which is not built.
var errNotLinux = errors.New("live context mounts are injected on Linux only")

// Attach is not built off Linux.
func Attach(pid int, source, dest string, rw bool) error {
	if _, err := checkArgs(pid, dest); err != nil {
		return err
	}
	if !filepath.IsAbs(source) {
		return errors.New("source " + quote(source) + " must be an absolute path")
	}
	return errNotLinux
}

// Detach is not built off Linux.
func Detach(pid int, dest string) error {
	if _, err := checkArgs(pid, dest); err != nil {
		return err
	}
	return errNotLinux
}

// DieWithParent is a no-op off Linux, where nothing is injected.
func DieWithParent() error { return nil }
