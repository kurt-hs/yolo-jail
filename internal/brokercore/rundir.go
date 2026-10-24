package brokercore

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// rundir.go is where a broker, or `yolo check`'s self-check, lays out the private directories
// the host programs it runs work under: broker/<source>/run/<pid>-<random>, 0700. One may hold
// a copy of a host credential (the github source copies the host's hosts.yml), so a run dir
// is collected by LIVENESS, like a scope file (BB-D44), never by age: its owner removes it
// when it stops, and one a killed owner left (SIGKILL, OOM) is removed by the next broker or
// self-check of that source to start, once kill(pid, 0) answers ESRCH for the pid in its
// name. Any other answer, and any name that does not start with a pid, keeps it.

// RunRoot is the directory every run dir of source sits in.
func RunRoot(source string) string { return filepath.Join(paths.BrokerSourceDir(source), "run") }

// NewRunDir names this process's run dir for source. The caller creates it.
func NewRunDir(source string) string {
	id, err := brokerscope.NewLaunchID()
	if err != nil {
		id = strconv.FormatInt(int64(os.Getpid()), 36)
	}
	return filepath.Join(RunRoot(source), strconv.Itoa(os.Getpid())+"-"+id)
}

// SweepRunDirs removes source's run dirs whose owning process is known gone, and returns them.
func SweepRunDirs(source string) (removed []string) {
	root := RunRoot(source)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		pidText, _, ok := strings.Cut(e.Name(), "-")
		pid, err := strconv.Atoi(pidText)
		if !ok || err != nil || pid <= 0 {
			continue
		}
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			p := filepath.Join(root, e.Name())
			if os.RemoveAll(p) == nil {
				removed = append(removed, p)
			}
		}
	}
	return removed
}
