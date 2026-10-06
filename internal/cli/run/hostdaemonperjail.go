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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/openaiauth"
)

// hostWideDaemonsStillSharedOn reports the backends HD-R1 is NOT built on yet, where a record
// declaring `host_daemon.scope: "host"` still gets the one machine-wide daemon (startHostSingleton).
// The ruling is built one backend at a time, podman first, and this predicate is the seam between
// the two halves until the last backend moves and it is deleted with the key.
func hostWideDaemonsStillSharedOn(rt string) bool {
	return rt != "podman" // parity: HonoredBy — every other backend still reaches the shared credential state through the machine-wide daemon (startHostSingleton) until HD-R1 is built there
}

// endpointPromisedAtAssembly reports whether the jail-facing variable of lp's host service is the
// ASSEMBLER's to emit (hostServicesMountArgs, optimistically, before any front exists), so the
// handle a spawn returns for it must carry none: insertHostServiceEnv would otherwise put the
// same `-e` in the argv twice. One predicate for both readers, the assembler's set and the
// spawn's blanking, so they cannot drift.
func endpointPromisedAtAssembly(lp *loopholes.Loophole) bool {
	return lp != nil && lp.HostDaemon != nil && lp.HostDaemon.Scope == loopholes.ScopeHost
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
func (o *Options) stopService(out printer, name string, hd *loopholes.HostDaemon, cmd *exec.Cmd,
	exited <-chan struct{}) {
	if hd != nil && hd.Detach {
		detachService(cmd)
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
	out := o.pr(o.Stdout)
	legacy := broker.SingletonDeps(openaiauth.LoopholeName, nil)
	legacy.Out = o.Stdout
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
