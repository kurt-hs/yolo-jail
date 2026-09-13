//go:build !linux

package main

import (
	"fmt"
	"os"
)

// openPty refuses on every platform but linux. yolo-serial only ever RUNS in the jail,
// which is linux; this half exists so the package still compiles when the host build
// targets darwin (`go test -short ./...` on a Mac, and the cross-GOOS lint pass).
//
// THE CALL SITE MUST STILL CHECK THE ERROR: pty_linux.go's openPty can succeed. That
// makes `err != nil` provably true under GOOS=darwin and only there, which is why the
// darwin lint pass in the Justfile's `lint` recipe drops SA4023 — the reasoning is
// written out there. Nothing here needs changing for it, and nothing here may be
// restructured to placate it.
func openPty() (*os.File, string, error) {
	return nil, "", fmt.Errorf("virtual PTY bridge is only supported on linux")
}
