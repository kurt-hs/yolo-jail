package run

// hostdaemonperjail.go is HD-R1's spawn side (docs/design/host-daemon-ownership.md, ruled
// 2026-09-20): a host-side daemon is spawned by the launch that wants it, serves that jail, and
// ends with it. The three loopholes that used to run one daemon per host (claude-oauth-broker,
// openai-auth-broker, aws-auth) take startExternalService like every other per-jail host
// service, so a newer yolo never touches a daemon an older jail is using: each jail's daemon is
// its own launch's build with its own launch's settings.
//
// WHAT STAYS SHARED is the STATE, deliberately, and it is what the sharing was always about
// (§1.1): one credentials file per loophole and the refresh flock beside it, keyed by the home or
// the state file and never by the process, so N daemons in one home take the same kernel lock and
// the second arrival of a concurrent pair reads what the first wrote.
//
// WHAT A JAIL'S END DOES is the manifest's `host_daemon.detach` (§1.3, "detach, do not drain, do
// not reap"): close the jail's front, SIGTERM the daemon alone, and neither wait nor kill. A
// refresh in flight finishes, writes the shared file, and the daemon exits on its own, bounded by
// its own upstream deadline (detachService).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/openaiauth"
	"github.com/mschulkind-oss/yolo-jail/internal/openaiauthdaemon"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// EVERY BACKEND, the same way. PODMAN and APPLE CONTAINER take the container arm's keeper, so a
// jail's own daemons are the keeper's children; what Apple Container starts is unchanged (its
// allow list, loopholeAllow, admits the OpenAI credential service, and the Claude broker when the
// launch delivers the credential view, which that jail's own broker keeps current while the jail
// runs). MACOS-USER and the HOST NOTCH give each SESSION its own, in the session's own
// host-services dir and keyed by it (servicessession.go, HD-D1), and serialize two sessions'
// starts of them with a per-workspace spawn guard (takeHostDaemonSpawnGuard, HD-D4). A launch
// with no jail at all (`yolo host -- pi`, `yolo openai-auth`) spawns the OpenAI daemon it needs
// itself (internal/openaiauthhost's privatedaemon.go).

// openAIHostSocket is the private host-to-host socket of the OpenAI credential daemon this launch
// started, which a host-side consumer dials (a doorway outside the sandbox, a launch-owned
// service's host half): the daemon binds it beside its fronted socket
// (openaiauthdaemon.HostSocketPath). "" when this launch started none, so nothing is pointed at
// a daemon some other launch owns.
func openAIHostSocket(handles []loopholeDaemon) string {
	for _, h := range handles {
		if h.name == openaiauth.LoopholeName && h.upstream != "" {
			return openaiauthdaemon.HostSocketPath(h.upstream)
		}
	}
	return ""
}

// hostDaemonSpawnGuardPath is the per-workspace spawn guard's lock file, beside the workspace's
// launch lock (flock.go) and distinct from it.
func hostDaemonSpawnGuardPath(cname string) string {
	return filepath.Join(paths.GlobalStorage(), "locks", cname+".host-daemons.lock")
}

// takeHostDaemonSpawnGuard is HD-D4's PER-WORKSPACE SPAWN GUARD for macos-user
// (docs/design/host-daemon-ownership.md, OQ-HD10): an exclusive flock a session holds while it
// starts its host services, and releases once they are up. On that backend two launches of one
// workspace are one identity (the container name is a hash of the workspace path) and run at
// once, since nothing attaches; the measured Mac runs credited one broker per pair to the
// machine-wide spawn flock, so per-session daemons owe that backend a guard of their own. A
// `yolo host` launch that starts host services for its doorways is a session of the same
// shape, and takes the same guard (HostDoorways.Start).
//
// WHAT IT SERIALIZES is the start, not a daemon's life: the settings file the session's daemons
// read once at their startup, which a second session of the workspace writes too
// (writeLoopholeSettings), the OpenAI legacy-state migration, and each spawn and readiness wait.
// Sockets need nothing from it: each session's are keyed by its own dir (HD-D1). The refresh
// flock and the CA's cert.lock guard the shared state as they always did.
//
// A REAL GUARD, not the courtesy launch lock (acquireWorkspaceLock), which warns and goes on when
// it cannot be taken: a lock this cannot take is returned, and the arm refuses the launch naming
// the next step. A wait is said, once, so it never reads as a hang.
func (o *Options) takeHostDaemonSpawnGuard(cname string) (*workspaceLock, error) {
	path := hostDaemonSpawnGuardPath(cname)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if err := flockSyscall(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		return &workspaceLock{f: f}, nil
	} else if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		o.pr(o.Stderr).printf("[dim]Waiting for another launch of this workspace to start its "+
			"host services (lock %s)...[/dim]", path)
	}
	if err := flockSyscall(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &workspaceLock{f: f}, nil
}

// hostDaemonSpawnGuardProblem says why a launch could not take the guard, and what to do.
func hostDaemonSpawnGuardProblem(cname string, err error) string {
	path := hostDaemonSpawnGuardPath(cname)
	return fmt.Sprintf("could not take this workspace's host-service spawn lock %s (%v), which "+
		"keeps two launches of one workspace from starting their host services at the same "+
		"moment. Make %s a directory you can write, with no directory at the lock's own path, "+
		"then launch again", path, err, filepath.Dir(path))
}

// hostDaemonSpawnGuardRefusal is the macos-user launch's refusal when the guard cannot be taken.
func hostDaemonSpawnGuardRefusal(cname string, err error) string {
	return "[bold red]Refusing the macos-user launch: " + hostDaemonSpawnGuardProblem(cname, err) +
		".[/bold red]"
}

// endpointPromisedAtAssembly reports whether the jail-facing variable of lp's host service is the
// ASSEMBLER's to emit (hostServicesMountArgs, optimistically, before any front exists), so the
// handle a spawn returns for it must carry none: insertHostServiceEnv would otherwise put the
// same `-e` in the argv twice. One predicate for both readers, the assembler's set
// (promisedEndpoints) and the spawn's blanking, so they cannot drift.
//
// THE MEMBERSHIP IS UNCHANGED BY HD-R1, AND RE-KEYED: it was every record declaring
// `host_daemon.scope: "host"`, which HD-R1 retires, and those three records are exactly the ones
// that declare `detach` — the credential daemons whose refresh must outlive the jail. Keying it on
// `detach` keeps each of them promised as before, so a credential daemon that fails to start still
// leaves the jail a variable the reachability witness refuses the launch over, rather than a jail
// that runs without it. Whether the promise should cover more daemons, or be its own manifest
// fact, is a ruling about argv assembly the design leaves open
// (docs/design/host-daemon-ownership.md §9, and HD-D8 there).
//
// AND ONLY ON LOOPBACK-TLS. `detach` is a key any manifest may declare, and it says how a jail's
// end stops the daemon, not how the jail reaches it. The promised variable names the endpoint file
// at hostServiceEndpointPath, which only a loopback-tls service gets (startExternalService); a
// detaching daemon on `transport: "none"` binds a plain socket and keeps its own `_SOCKET`
// variable, so promising it an endpoint would hand its jail a file that never appears, which the
// reachability witness refuses the launch over, and blank the variable it really needs.
func endpointPromisedAtAssembly(lp *loopholes.Loophole) bool {
	return lp != nil && lp.HostDaemon != nil && lp.HostDaemon.Detach &&
		lp.Transport == loopholes.TransportLoopbackTLS
}

// detachService is a jail's end for a daemon whose manifest declares `detach`: SIGTERM to the
// daemon ALONE, not its group, and no wait. The daemon closes its listeners, lets the work in
// flight finish (a refresh that already redeemed a single-use token upstream writes its
// replacement; aws-auth's `aws` child, which this does not signal, completes its mint) and
// exits. Nothing here waits for it, hunts it or reaps it: its upstream deadline is its bound
// (docs/design/host-daemon-ownership.md §1.3, mode 6). The child's own reaper is the Wait
// goroutine its spawn started, while this process lives; after that, init.
//
// Discarded error: ESRCH means it is already gone, which is the outcome asked for.
func detachService(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
}

// stopService is the one choice between the two teardowns a spawned host service gets: detach
// for a daemon that declares it, the process-group SIGTERM, grace and SIGKILL for every other.
//
// DETACH SKIPS A PROCESS ALREADY REAPED: it signals the bare pid, and once the child's Wait has
// returned (exited closed: a supervisor's backoff wait, a give-up) that pid may be another
// process's. Until then the pid is still this child's, a zombie at worst. The kill path signals
// regardless: its target is the group, which may still hold a child the leader left (a
// daemonizing wrapper's service, a crashed leader's fork), and a group with no members left
// answers ESRCH.
func (o *Options) stopService(out printer, name string, hd *loopholes.HostDaemon, cmd *exec.Cmd,
	exited <-chan struct{}) {
	if hd != nil && hd.Detach {
		select {
		case <-exited:
		default:
			detachService(cmd)
		}
		return
	}
	killServiceGroup(out, name, o.serviceTermGrace(), cmd, exited)
}

// removeSocketSiblings unlinks the files a fronted daemon bound beside its upstream socket under
// the socket's own name and a suffix: the OpenAI and AWS credential daemons' private host-to-host
// socket, `<socket>.host`. A detached daemon is still exiting when its jail's teardown runs, and
// none of yolo's daemons unlink a socket on close (hostservice's bindUnixSocket says why), so the
// teardown removes them. Errors are discarded for retireFrontSockets' reason: the next daemon to
// bind the path removes a leftover first.
func removeSocketSiblings(socketPath string) {
	matches, _ := filepath.Glob(socketPath + ".*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// migrateLegacyOpenAIAuthState runs the OpenAI legacy-state migration (prepareLegacyOpenAIAuthState)
// before this launch spawns its own OpenAI daemon, and reports whether the spawn may go ahead.
//
// "MIGRATE, THEN SPAWN" (docs/design/host-daemon-ownership.md §5.2): the migration moves STATE,
// which stays shared, so it survives the singleton. It runs under the lock it always ran under,
// the machine-wide spawn lock of the old singleton (paths.HostSingletonLock), so two launches
// never relocate the file at once, and a launch of an older yolo, which takes the same lock
// around its own migration and spawn, is serialized with this one too.
//
// IT STOPS AN OLDER DAEMON ONLY WHEN THAT DAEMON IS THE BROKEN ONE: a host-wide OpenAI daemon an
// older yolo started with the literal `{state}` path, which would go on writing a login to the
// wrong file. A host-wide daemon serving the canonical path is left alone, since it serves the
// jails an older yolo launched and the relocated file is the one it reads.
//
// A migration that cannot complete keeps today's answer: no daemon for this launch, and a line
// naming the file and what to do, rather than a daemon that would serve "not logged in" beside a
// login it cannot see.
func (o *Options) migrateLegacyOpenAIAuthState() bool {
	out := o.pr(o.Stderr)
	legacy := broker.LegacyDeps(openaiauth.LoopholeName)
	lock, err := takeStrictLock(legacy.LockPath)
	if err != nil {
		out.printf("[yellow]Warning: could not take %s to check for a legacy OpenAI credential "+
			"file (%v); this launch starts no OpenAI credential service. Fix the lock file's "+
			"permissions (or remove it, if no older yolo is running), then launch again.[/yellow]",
			legacy.LockPath, err)
		return false
	}
	defer lock.Close()
	afterStop, err := prepareLegacyOpenAIAuthState(o.Workspace, legacy)()
	if err != nil {
		out.printf("[yellow]Warning: could not check for a legacy OpenAI credential file: %v. "+
			"This launch starts no OpenAI credential service; fix the file named above, then "+
			"launch again.[/yellow]", err)
		return false
	}
	if afterStop == nil {
		return true
	}
	if singletonUsesLegacyOpenAIStatePath(legacy) {
		broker.BrokerKill(legacy, syscall.SIGTERM, broker.BrokerKillTimeout)
	}
	if err := afterStop(); err != nil {
		out.printf("[yellow]Warning: could not migrate the legacy OpenAI credential file: %v. "+
			"This launch starts no OpenAI credential service; keep the file you want at %s, "+
			"remove the other, then launch again.[/yellow]", err,
			filepath.Join(loopholes.StateDirFor(openaiauth.LoopholeName), openaiauth.StateFileName))
		return false
	}
	return true
}

// takeStrictLock opens path and takes a BLOCKING exclusive flock on it, and unlike the
// workspace courtesy lock (acquireWorkspaceLock) it returns the failure rather than a guard
// that guards nothing: a caller that needs the lock refuses the step it guards.
func takeStrictLock(path string) (*workspaceLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if err := flockSyscall(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &workspaceLock{f: f}, nil
}
