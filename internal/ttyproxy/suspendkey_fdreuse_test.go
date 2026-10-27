//go:build linux

package ttyproxy

import (
	"os"
	"os/exec"
	"testing"
)

func TestAssertProxySuspendsClosesOwningFile(t *testing.T) {
	if os.Getenv("YOLO_TTYPROXY_SUSPEND_OWNER") == "1" {
		var ownerFD uintptr
		assertProxySuspends(t, "\x1b[122;5u", true, func(fd uintptr) {
			ownerFD = fd
		})
		if ownerFD != ^uintptr(0) {
			t.Fatalf("suspend fixture's owning file still reports fd %d after parent closed its descriptor; want os.File closed sentinel %d", ownerFD, ^uintptr(0))
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAssertProxySuspendsClosesOwningFile$")
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_SUSPEND_OWNER=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("suspend fixture owning-file retirement regression: %v: %s", err, output)
	}
}
