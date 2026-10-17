package cli

// capturerunendgroup_test.go pins `yolo internal capture-run --end-process-group` (pi-extensions.md
// PPX-D49) at the driver's own argv parser: the word the sealed macos-user build act puts on its
// driver argv reaches capture.Options.EndProcessGroup, so a child the build line left running is
// gone when the driver exits. Red with the flag's case or its hand-off deleted.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestCaptureRunEndsTheBuildLinesProcessGroupWhenAsked(t *testing.T) {
	for _, end := range []bool{true, false} {
		t.Run(map[bool]string{true: "--end-process-group", false: "without it"}[end], func(t *testing.T) {
			home := t.TempDir()
			pidFile := filepath.Join(t.TempDir(), "pid")
			script := filepath.Join(t.TempDir(), "build.sh")
			if err := os.WriteFile(script, []byte("(sleep 30) & echo $! > '"+pidFile+"'\nexit 3\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			args := []string{"--out=" + filepath.Join(t.TempDir(), "out"), "--home=" + home}
			if end {
				args = append(args, "--end-process-group")
			}
			if rc := runCaptureRun(append(args, "--", "/bin/sh", script)); rc != 1 {
				t.Errorf("capture-run over a failed build line exited %d, want 1", rc)
			}
			b, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			alive := !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
			if st, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
				if i := strings.LastIndexByte(string(st), ')'); i >= 0 && i+2 < len(st) && st[i+2] == 'Z' {
					alive = false
				}
			}
			if alive == end {
				t.Errorf("the build line's child alive after capture-run = %v, want %v", alive, !end)
			}
		})
	}
}
