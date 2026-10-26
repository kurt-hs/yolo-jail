//go:build linux

package execmonitor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	infoTimeout     = 2 * time.Second
	inspectTimeout  = 2 * time.Second
	procScanTimeout = time.Second
	maxProcEntries  = 16384
	maxInfoBytes    = 1 << 20
	maxInspectBytes = 2 << 20
)

// Runner runs one selected Podman command, returning its stdout, whether it completed, and its
// status. It receives bounded command deadlines and must preserve the user's Podman selection.
type Runner func(argv []string, timeout time.Duration) (string, bool, int)

// Ops are the process seams used by the caller and fake-proc tests.
type Ops struct {
	ProcRoot   string
	UID        int
	SelfPID    int
	UIDSet     bool
	PidfdOpen  func(pid int) (int, error)
	SendSignal func(fd int, sig syscall.Signal) error
	ClosePidfd func(fd int) error
}

func (o Ops) filled() Ops {
	if o.ProcRoot == "" {
		o.ProcRoot = "/proc"
	}
	if o.UID < 0 {
		o.UID = os.Geteuid()
	}
	if !o.UIDSet {
		o.UID = os.Geteuid()
	}
	if o.SelfPID == 0 {
		o.SelfPID = os.Getpid()
	}
	if o.PidfdOpen == nil {
		o.PidfdOpen = func(pid int) (int, error) { return unix.PidfdOpen(pid, 0) }
	}
	if o.SendSignal == nil {
		o.SendSignal = func(fd int, sig syscall.Signal) error {
			return unix.PidfdSendSignal(fd, unix.Signal(sig), nil, 0)
		}
	}
	if o.ClosePidfd == nil {
		o.ClosePidfd = unix.Close
	}
	return o
}

// Recover tries the last-resort predicate for an unresolved explicit force-stop/removal. A false
// result means every predicate declined; callers still must re-probe container existence.
func Recover(name string, run Runner, ops Ops) (bool, error) {
	if name == "" || run == nil {
		return false, errors.New("missing selected container name or Podman runner")
	}
	o := ops.filled()
	infoText, ran, rc := run([]string{"podman", "info", "--format", "json"}, infoTimeout)
	if !ran || rc != 0 || len(infoText) > maxInfoBytes {
		return false, errors.New("bounded Podman info did not answer")
	}
	info, err := ParseRuntimeInfo([]byte(infoText))
	if err != nil {
		return false, err
	}
	resolvedConmonPath, err := filepath.EvalSymlinks(info.ConmonPath)
	if err != nil {
		return false, errors.New("podman's Conmon.Path cannot be resolved")
	}
	conmon, err := os.Stat(resolvedConmonPath)
	if err != nil || !conmon.Mode().IsRegular() || conmon.Mode().Perm()&0111 == 0 {
		return false, errors.New("podman's Conmon.Path is not an executable regular file")
	}
	initial, err := inspect(name, run)
	if err != nil || initial.State != "stopping" || !hasExec(initial) {
		return false, errors.New("typed container snapshot does not authorize recovery")
	}
	candidate, children, err := findCandidate(o, resolvedConmonPath, conmon, initial)
	if err != nil {
		return false, err
	}
	fd, err := o.PidfdOpen(candidate.PID)
	if err != nil {
		return false, fmt.Errorf("pidfd is unavailable: %w", err)
	}
	defer o.ClosePidfd(fd)

	// The pidfd pins one process; all numeric-proc reads below must still match the pre-pin identity.
	pinned, err := readCandidate(o, candidate.PID, resolvedConmonPath, conmon, initial)
	if err != nil || !sameProcess(candidate, pinned) {
		return false, errors.New("monitor identity changed after pidfd pin")
	}
	pinnedChildren, err := directZombieChild(o, candidate.PID)
	if err != nil || !sameChild(children, pinnedChildren) {
		return false, errors.New("monitor child identity changed after pidfd pin")
	}
	latest, err := inspect(initial.ID, run)
	if err != nil || latest.ID != initial.ID || latest.State != "stopping" || !sameExecs(initial.ExecIDs, latest.ExecIDs) || !hasExec(latest) {
		return false, errors.New("typed container/session state changed before signal")
	}
	// Re-read both identities after the runtime metadata query, immediately before the pidfd signal.
	pinned, err = readCandidate(o, candidate.PID, resolvedConmonPath, conmon, initial)
	if err != nil || !sameProcess(candidate, pinned) {
		return false, errors.New("monitor identity changed before signal")
	}
	pinnedChildren, err = directZombieChild(o, candidate.PID)
	if err != nil || !sameChild(children, pinnedChildren) {
		return false, errors.New("monitor child identity changed before signal")
	}
	if err := o.SendSignal(fd, syscall.SIGKILL); err != nil {
		return false, fmt.Errorf("pidfd signal failed: %w", err)
	}
	return true, nil
}

func inspect(target string, run Runner) (Container, error) {
	out, ran, rc := run([]string{"podman", "inspect", "--type", "container", "--format", "{{json .}}", target}, inspectTimeout)
	if !ran || rc != 0 || len(out) > maxInspectBytes {
		return Container{}, errors.New("typed Podman inspect did not answer within its bound")
	}
	return ParseContainer([]byte(out))
}

func hasExec(c Container) bool {
	if c.ID == "" || c.State != "stopping" {
		return false
	}
	for _, id := range c.ExecIDs {
		if id != "" && id != c.ID {
			return true
		}
	}
	return false
}

func sameExecs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, id := range a {
		set[id]++
	}
	for _, id := range b {
		if set[id] == 0 {
			return false
		}
		set[id]--
	}
	return true
}

type process struct {
	PID       int
	PPID      int
	UID       int
	State     byte
	StartTime uint64
	Dev       uint64
	Ino       uint64
	ExecID    string
}

type conmonArgs struct {
	CID       string
	ExecID    string
	Exec      bool
	Malformed bool
}

func findCandidate(o Ops, conmonPath string, conmon os.FileInfo, snapshot Container) (process, process, error) {
	root, err := os.Open(o.ProcRoot)
	if err != nil {
		return process{}, process{}, err
	}
	defer root.Close()
	deadline := time.Now().Add(procScanTimeout)
	var candidate *process
	read := 0
	for {
		entries, err := root.ReadDir(256)
		for _, entry := range entries {
			pid, parseErr := strconv.Atoi(entry.Name())
			if parseErr != nil || pid <= 1 || pid == o.SelfPID || !entry.IsDir() {
				continue
			}
			read++
			if read > maxProcEntries || time.Now().After(deadline) {
				return process{}, process{}, errors.New("bounded proc scan exhausted")
			}
			owner, statErr := os.Stat(filepath.Join(o.ProcRoot, entry.Name()))
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				return process{}, process{}, errors.New("proc owner could not be read")
			}
			uid, uidOK := fileUID(owner)
			if !uidOK || uid != o.UID {
				continue
			}
			exePath := filepath.Join(o.ProcRoot, entry.Name(), "exe")
			resolvedExePath, resolveErr := filepath.EvalSymlinks(exePath)
			if resolveErr != nil || resolvedExePath != conmonPath {
				continue
			}
			exeInfo, exeErr := os.Stat(exePath)
			if exeErr != nil || !sameFile(conmon, exeInfo) {
				return process{}, process{}, errors.New("matching conmon executable identity is unreadable")
			}
			args, readErr := os.ReadFile(filepath.Join(o.ProcRoot, entry.Name(), "cmdline"))
			if readErr != nil {
				return process{}, process{}, errors.New("same-UID conmon arguments are unreadable")
			}
			parsed := parseConmonArgs(args)
			if parsed.Malformed {
				return process{}, process{}, errors.New("conmon arguments are malformed or ambiguous")
			}
			if parsed.CID != snapshot.ID || parsed.ExecID == "" || parsed.ExecID == snapshot.ID || !parsed.Exec || !contains(snapshot.ExecIDs, parsed.ExecID) {
				continue
			}
			p, err := readCandidate(o, pid, conmonPath, conmon, snapshot)
			if err != nil || p.State == 'Z' || p.State == 'X' || p.UID != o.UID {
				return process{}, process{}, errors.New("candidate conmon identity is unavailable or not live")
			}
			if candidate != nil {
				return process{}, process{}, errors.New("multiple exec-monitor candidates are ambiguous")
			}
			candidate = &p
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return process{}, process{}, err
		}
	}
	if candidate == nil {
		return process{}, process{}, errors.New("no matching exec monitor")
	}
	child, err := directZombieChild(o, candidate.PID)
	if err != nil {
		return process{}, process{}, err
	}
	return *candidate, child, nil
}

func readCandidate(o Ops, pid int, conmonPath string, conmon os.FileInfo, snapshot Container) (process, error) {
	p, err := readProcess(o, pid)
	if err != nil {
		return process{}, err
	}
	if !supportedLiveMonitorState(p.State) {
		return process{}, errors.New("candidate monitor state is unsupported or not live")
	}
	exePath := filepath.Join(o.ProcRoot, strconv.Itoa(pid), "exe")
	resolvedExePath, err := filepath.EvalSymlinks(exePath)
	if err != nil || resolvedExePath != conmonPath {
		return process{}, errors.New("candidate executable path no longer matches Conmon.Path")
	}
	exeInfo, err := os.Stat(exePath)
	if err != nil || !sameFile(conmon, exeInfo) {
		return process{}, errors.New("candidate executable no longer matches Conmon.Path")
	}
	args, err := os.ReadFile(filepath.Join(o.ProcRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return process{}, errors.New("candidate arguments are unreadable")
	}
	parsed := parseConmonArgs(args)
	if parsed.Malformed || parsed.CID != snapshot.ID || !parsed.Exec || parsed.ExecID == "" || parsed.ExecID == snapshot.ID || !contains(snapshot.ExecIDs, parsed.ExecID) {
		return process{}, errors.New("candidate arguments no longer identify the stored exec")
	}
	p.Dev, p.Ino = statFile(exeInfo)
	p.ExecID = parsed.ExecID
	return p, nil
}

func parseConmonArgs(raw []byte) conmonArgs {
	parts := strings.Split(string(raw), "\x00")
	if len(parts) < 2 || filepath.Base(parts[0]) != "conmon" {
		return conmonArgs{Malformed: true}
	}
	var out conmonArgs
	seenCID, seenExecID, seenExec := false, false, false
	for i := 1; i < len(parts); i++ {
		arg := parts[i]
		if arg == "" {
			continue
		}
		switch {
		case arg == "-c" || arg == "--cid":
			if seenCID || i+1 >= len(parts) || parts[i+1] == "" {
				return conmonArgs{Malformed: true}
			}
			seenCID, out.CID = true, parts[i+1]
			i++
		case strings.HasPrefix(arg, "--cid="):
			if seenCID || strings.TrimPrefix(arg, "--cid=") == "" {
				return conmonArgs{Malformed: true}
			}
			seenCID, out.CID = true, strings.TrimPrefix(arg, "--cid=")
		case arg == "-u" || arg == "--cuuid":
			if seenExecID || i+1 >= len(parts) || parts[i+1] == "" {
				return conmonArgs{Malformed: true}
			}
			seenExecID, out.ExecID = true, parts[i+1]
			i++
		case strings.HasPrefix(arg, "--cuuid="):
			if seenExecID || strings.TrimPrefix(arg, "--cuuid=") == "" {
				return conmonArgs{Malformed: true}
			}
			seenExecID, out.ExecID = true, strings.TrimPrefix(arg, "--cuuid=")
		case arg == "-e" || arg == "--exec":
			if seenExec {
				return conmonArgs{Malformed: true}
			}
			seenExec, out.Exec = true, true
		}
	}
	if !seenCID || !seenExecID {
		return conmonArgs{Malformed: true}
	}
	return out
}

func directZombieChild(o Ops, monitor int) (process, error) {
	tasks, err := os.ReadDir(filepath.Join(o.ProcRoot, strconv.Itoa(monitor), "task"))
	if err != nil || len(tasks) == 0 {
		return process{}, errors.New("monitor task enumeration is incomplete")
	}
	children := map[int]bool{}
	for _, task := range tasks {
		tid, err := strconv.Atoi(task.Name())
		if err != nil {
			return process{}, errors.New("monitor task list is malformed")
		}
		data, err := os.ReadFile(filepath.Join(o.ProcRoot, strconv.Itoa(monitor), "task", task.Name(), "children"))
		if err != nil {
			return process{}, errors.New("monitor direct-child enumeration is incomplete")
		}
		for _, token := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(token)
			if err != nil || pid <= 1 {
				return process{}, errors.New("monitor child list is malformed")
			}
			children[pid] = true
		}
		_ = tid
	}
	if len(children) != 1 {
		return process{}, errors.New("monitor has zero or multiple direct children")
	}
	var childPID int
	for pid := range children {
		childPID = pid
	}
	child, err := readProcess(o, childPID)
	if err != nil || child.PPID != monitor || child.UID != o.UID || child.State != 'Z' || child.StartTime == 0 {
		return process{}, errors.New("monitor child is not an owned direct zombie")
	}
	return child, nil
}

func readProcess(o Ops, pid int) (process, error) {
	if pid <= 1 || pid == o.SelfPID {
		return process{}, errors.New("unsafe process ID")
	}
	base := filepath.Join(o.ProcRoot, strconv.Itoa(pid))
	owner, err := os.Stat(base)
	if err != nil {
		return process{}, err
	}
	uid, ok := fileUID(owner)
	if !ok {
		return process{}, errors.New("process owner is unavailable")
	}
	status, err := os.ReadFile(filepath.Join(base, "status"))
	if err != nil {
		return process{}, err
	}
	statusUID, ok := parseEffectiveUID(status)
	if !ok || statusUID != uid {
		return process{}, errors.New("process UID metadata is inconsistent")
	}
	stat, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return process{}, err
	}
	pidFromStat, ppid, state, start, err := parseProcStat(stat)
	if err != nil || pidFromStat != pid || start == 0 {
		return process{}, errors.New("process stat identity is malformed")
	}
	return process{PID: pid, PPID: ppid, UID: uid, State: state, StartTime: start}, nil
}

func supportedLiveMonitorState(state byte) bool {
	switch state {
	case 'R', 'S', 'D', 'T', 't':
		return true
	default:
		return false
	}
}

func parseProcStat(data []byte) (pid, ppid int, state byte, start uint64, err error) {
	s := strings.TrimSpace(string(data))
	open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open <= 0 || close <= open || close+1 >= len(s) {
		return 0, 0, 0, 0, errors.New("malformed proc stat")
	}
	pid, err = strconv.Atoi(strings.TrimSpace(s[:open]))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	fields := strings.Fields(s[close+1:])
	if len(fields) <= 19 || len(fields[0]) != 1 {
		return 0, 0, 0, 0, errors.New("short proc stat")
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	start, err = strconv.ParseUint(fields[19], 10, 64)
	return pid, ppid, fields[0][0], start, err
}

func parseEffectiveUID(data []byte) (int, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
		if len(fields) != 4 {
			return 0, false
		}
		uid, err := strconv.Atoi(fields[1])
		return uid, err == nil && uid >= 0
	}
	return 0, false
}

func fileUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

func sameFile(a, b os.FileInfo) bool {
	adev, aino := statFile(a)
	bdev, bino := statFile(b)
	return adev == bdev && aino == bino
}

func statFile(info os.FileInfo) (uint64, uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(stat.Dev), stat.Ino
}

func sameProcess(a, b process) bool {
	return a.PID == b.PID && a.PPID == b.PPID && a.UID == b.UID && a.State == b.State && a.StartTime == b.StartTime && a.Dev == b.Dev && a.Ino == b.Ino && a.ExecID == b.ExecID
}

func sameChild(a, b process) bool { return sameProcess(a, b) }

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
