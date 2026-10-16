// Package broker is the `yolo host-daemon {status,stop,restart,logs}` verb and its
// `yolo broker` alias over the Claude OAuth broker, and the lifecycle helpers they act with.
//
// SINCE HD-R1 THERE IS NO SINGLETON TO MANAGE (docs/design/host-daemon-ownership.md, ruled
// 2026-09-20). Every host daemon is spawned by the launch that wants it and ends with that
// launch, so what the verb reports is what the jails on this machine are running
// (hostdaemons.go's RunningInstances): one copy per jail, at its jail-keyed socket. The package
// used to be the engine that ENSURED one daemon per host for every jail (EnsureSingleton, the
// settings record, the capability stamp); all of that is deleted, because nothing in this build
// starts such a daemon.
//
// WHAT IS LEFT OF THE SINGLETON is an older yolo's: a machine-wide daemon it started may still be
// running at the name-keyed /tmp paths (paths.HostSingleton*), serving the jails that yolo
// launched. The verb reports it and can stop it; it never starts one. Deps, BrokerStatus,
// BrokerIsAlive and BrokerKill below are the helpers that read and stop such a daemon by its PID
// file, kept behind an injectable seam so they are testable against a fake socket and pid.
package broker

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/execx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// BrokerSingletonSocket / BrokerSingletonPIDFile / BrokerSingletonLock are the Claude broker's
// machine-wide paths, where an OLDER yolo's broker answers. Derived from paths.HostSingleton*,
// never spelled here; in production they are /tmp/yolo-claude-oauth-broker.{sock,pid,lock}.
func BrokerSingletonSocket() string { return paths.HostSingletonSocket(BrokerLoopholeName) }

// BrokerSingletonPIDFile is the Claude broker's machine-wide PID file (see BrokerSingletonSocket).
func BrokerSingletonPIDFile() string { return paths.HostSingletonPIDFile(BrokerLoopholeName) }

// BrokerSingletonLock is the Claude broker's machine-wide lock (see BrokerSingletonSocket).
func BrokerSingletonLock() string { return paths.HostSingletonLock(BrokerLoopholeName) }

const BrokerLoopholeName = "claude-oauth-broker"

// Timing knobs.
const (
	// SocketPollInterval is the poll interval for PID-exit waits.
	SocketPollInterval = 50 * time.Millisecond
	// BrokerKillTimeout is BrokerKill's SIGTERM grace before SIGKILL.
	BrokerKillTimeout = 3 * time.Second
	// ReachTimeout is the reachability probe's deadline.
	ReachTimeout = 2 * time.Second
)

// Status is the snapshot BrokerStatus returns for a machine-wide daemon: pid (present?), pid
// liveness, socket presence, reachability, and the display paths.
type Status struct {
	PID          int  `json:"pid"`
	PIDPresent   bool `json:"pid_present"`
	PIDLive      bool `json:"pid_live"`
	SocketExists bool `json:"socket_exists"`
	// Reachable is the socket's ACCEPT answer, not a protocol round trip: see
	// SingletonReachable for why the host side cannot speak a fronted daemon's protocol.
	Reachable bool   `json:"socket_accepting"`
	Socket    string `json:"socket"`
	PIDFile   string `json:"pid_file"`
	// Healthy is PIDLive && Reachable.
	Healthy bool `json:"healthy"`
}

// Deps are the injectable seams for reading and stopping a machine-wide daemon an older yolo
// left. LegacyDeps wires them to the real paths and OS effects; tests substitute fakes.
type Deps struct {
	// Name is the LOOPHOLE the daemon belongs to, for the messages.
	Name        string
	SocketPath  string
	PIDFilePath string
	LockPath    string
	LogPath     string
	Now         func() time.Time
	Sleep       func(time.Duration)
	PathExists  func(string) bool

	// Reachable reports whether the socket accepts a connection (see SingletonReachable).
	Reachable func(socketPath string, timeout time.Duration) bool
	// Alive reports process liveness (kill(pid,0); EPERM counts as alive).
	Alive func(pid int) bool
	// Kill sends sig to pid. Errors are swallowed by callers.
	Kill func(pid int, sig syscall.Signal) error
	// Pgrep returns the PIDs of the processes serving the daemon's socket, self excluded
	// (RealPgrepStrays). BrokerKill falls back to it when the PID file is gone.
	Pgrep func() []int
}

// LegacyDeps returns Deps for the machine-wide daemon an older yolo may have left running for the
// loophole called name: every path is derived from the name (paths.HostSingleton*).
func LegacyDeps(name string) Deps {
	sock := paths.HostSingletonSocket(name)
	return Deps{
		Name:        name,
		SocketPath:  sock,
		PIDFilePath: paths.HostSingletonPIDFile(name),
		LockPath:    paths.HostSingletonLock(name),
		LogPath:     HostServiceLogPath(name),
		Now:         time.Now,
		Sleep:       time.Sleep,
		PathExists:  func(p string) bool { _, err := os.Lstat(p); return err == nil },
		Reachable:   SingletonReachable,
		Alive:       execx.IsAlive,
		Kill:        func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
		Pgrep:       func() []int { return RealPgrepStrays(sock) },
	}
}

// HostServiceLogPath is the log every daemon of the loophole called name writes,
// GLOBAL_STORAGE/logs/host-service-<name>.log: one file across every jail's copy and an older
// yolo's machine-wide one, so a reader finds them together.
func HostServiceLogPath(loopholeName string) string {
	return filepath.Join(paths.GlobalStorage(), "logs", "host-service-"+loopholeName+".log")
}

// BrokerLogPath is the Claude broker's log (HostServiceLogPath).
func BrokerLogPath() string { return HostServiceLogPath(BrokerLoopholeName) }

// BrokerReadPID is the integer PID from the daemon's PID file, or (0,false) if the file is
// absent, unreadable or malformed.
func BrokerReadPID(deps Deps) (int, bool) {
	raw, err := os.ReadFile(deps.PIDFilePath)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, false
	}
	return n, true
}

// BrokerStatus reports the machine-wide daemon's pid, its liveness, its socket's presence and
// whether that socket accepts.
func BrokerStatus(deps Deps) Status {
	pid, present := BrokerReadPID(deps)
	pidLive := present && deps.Alive(pid)
	sockExists := deps.PathExists(deps.SocketPath)
	reachable := sockExists && deps.Reachable(deps.SocketPath, ReachTimeout)
	return Status{
		PID:          pid,
		PIDPresent:   present,
		PIDLive:      pidLive,
		SocketExists: sockExists,
		Reachable:    reachable,
		Socket:       deps.SocketPath,
		PIDFile:      deps.PIDFilePath,
		Healthy:      pidLive && reachable,
	}
}

// BrokerIsAlive reports PID file present + PID live + socket present + socket accepting. All
// four must hold.
func BrokerIsAlive(deps Deps) bool {
	pid, present := BrokerReadPID(deps)
	if !present || !deps.Alive(pid) {
		return false
	}
	if !deps.PathExists(deps.SocketPath) {
		return false
	}
	return deps.Reachable(deps.SocketPath, ReachTimeout)
}

// BrokerKill sends sig to the machine-wide daemon (PID file first, else the processes serving
// its socket), waits for every signaled PID to exit (escalating to SIGKILL on stragglers), then
// removes the PID file, the socket and the sidecars an older yolo wrote beside them. Returns
// true iff something was running to signal.
func BrokerKill(deps Deps, sig syscall.Signal, timeout time.Duration) bool {
	var pids []int
	if primary, ok := BrokerReadPID(deps); ok {
		pids = append(pids, primary)
	} else {
		pids = append(pids, deps.Pgrep()...)
	}
	if len(pids) == 0 {
		removeIgnoreMissing(deps.SocketPath)
		return false
	}
	for _, pid := range pids {
		_ = deps.Kill(pid, sig)
	}
	deadline := deps.Now().Add(timeout)
	survivors := append([]int(nil), pids...)
	for len(survivors) > 0 && deps.Now().Before(deadline) {
		survivors = liveOnly(deps, survivors)
		if len(survivors) > 0 {
			deps.Sleep(SocketPollInterval)
		}
	}
	for _, pid := range survivors {
		_ = deps.Kill(pid, syscall.SIGKILL)
	}
	removeIgnoreMissing(deps.PIDFilePath)
	removeIgnoreMissing(deps.SocketPath)
	// The sidecars an older yolo wrote beside its PID file: its capability stamp, its launch-check
	// stamp (HD-D20, retired by HD-D21) and its settings record.
	removeIgnoreMissing(deps.PIDFilePath + ".capability")
	removeIgnoreMissing(deps.PIDFilePath + ".launch-check")
	removeIgnoreMissing(deps.PIDFilePath + ".settings")
	return true
}

// liveOnly returns the subset of pids that are still alive.
func liveOnly(deps Deps, pids []int) []int {
	var out []int
	for _, p := range pids {
		if deps.Alive(p) {
			out = append(out, p)
		}
	}
	return out
}

// SingletonReachable reports whether a host daemon's socket ACCEPTS a connection. Connect, then
// close — no bytes are written and none are read.
//
// It cannot be a protocol round trip: the daemon sits behind yolo's front, so the first thing it
// reads on every connection is yolo's CONNECTION PREAMBLE, yolo's assertion of WHICH JAIL is on the
// other end. A host-side probe belongs to no jail, and forging one would put a fabricated jail_id
// in the daemon's audit line. The end-to-end check lives where it can still be spoken: `yolo
// check`'s per-jail probe, which goes through the front.
func SingletonReachable(socketPath string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// RealPgrepStrays returns the PIDs of the running processes serving the daemon at socketPath —
// whatever passes that socket as `--socket` — with this process filtered out. A missing pgrep, a
// timeout or no match yields none.
func RealPgrepStrays(socketPath string) []int {
	return pgrepArgv("(^| )--socket " + regexp.QuoteMeta(socketPath) + "( |$)")
}

// pgrepArgv returns the PIDs whose joined argv matches the extended regex pattern, self excluded.
func pgrepArgv(pattern string) []int {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid == self {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// removeIgnoreMissing unlinks p, ignoring every error: the files are ours, and a leftover is
// cleared by the next stop.
func removeIgnoreMissing(p string) { _ = os.Remove(p) }
