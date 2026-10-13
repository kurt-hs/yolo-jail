//go:build linux

package macosuser

// Linux ptys exercise Script's real terminal gate; they do not prove sudo or
// sandbox-exec works on macOS. No agent or jail is launched here.
import (
	"github.com/mschulkind-oss/yolo-jail/internal/provision"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"syscall"
	"testing"
	"unsafe"
)

func openProvisionPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	var unlock int32
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, m.Fd(), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		m.Close()
		t.Skipf("unlockpt: %v", e)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN)
	if err != nil {
		m.Close()
		t.Skipf("ptsname: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Skipf("open slave: %v", err)
	}
	t.Cleanup(func() { s.Close(); m.Close() })
	return m, s
}

func TestScriptTerminalOutcomesReachRunProvisionStage(t *testing.T) {
	for _, answer := range []string{"n", "y"} {
		t.Run(answer, func(t *testing.T) {
			master, slave := openProvisionPty(t)
			if _, err := master.Write([]byte(answer + "\n")); err != nil {
				t.Fatal(err)
			}
			status := 0
			if answer == "n" {
				status = provision.StageAbortedStatus
			}
			assertScriptOutcome(t, "exit 7", "", slave, answer == "y", status)
		})
	}
}
