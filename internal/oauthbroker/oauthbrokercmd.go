package oauthbroker

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/version"
)

func defaultCredsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	return filepath.Join(home, ".local/share/yolo-jail/home/.claude-shared-credentials/.credentials.json")
}

// Main is the per-host Claude OAuth refresh daemon entry point.
//
// CLI contract: --socket, --creds-file, --init-ca, --force-init-ca,
// --self-check, --no-background-refresh, -v/--verbose.
func Main(argv []string) int {
	fs := flag.NewFlagSet("yolo-claude-oauth-broker-host", flag.ExitOnError)
	socket := fs.String("socket", "", "Unix socket to bind (set by `yolo run`)")
	credsFile := fs.String("creds-file", defaultCredsPath(), "Shared credentials file")
	initCA := fs.Bool("init-ca", false, "Generate CA + leaf cert and exit (idempotent)")
	forceInitCA := fs.Bool("force-init-ca", false, "Regenerate CA + leaf even if they exist")
	selfCheck := fs.Bool("self-check", false, "Emit status and exit")
	noBgRefresh := fs.Bool("no-background-refresh", false, "Disable the proactive refresh loop")
	logFile := fs.String("log-file", "", "append the operational log here (default: stderr)")
	verbose := fs.Bool("verbose", false, "Verbose logging")
	verboseShort := fs.Bool("v", false, "Verbose logging")
	endWithPID := fs.Int("end-with-pid", 0,
		"Exit once this process is gone, and remove the socket: a jail-less launch's own broker")
	_ = fs.Parse(argv)

	// Configure the operational log. Matches the cgd/journald --log-file
	// convention (commit ec888c6): empty path -> stderr (captured by the
	// loophole supervisor); -v -> DEBUG. Set up before any broker call so the
	// incident-forensics lines (refresh attempts, token fingerprints, upstream
	// status codes) are captured for an unattended soak.
	SetupLog(*logFile, *verbose || *verboseShort)

	// Stamp the versioned User-Agent (a generic client UA triggers Cloudflare
	// 1010 on platform.claude.com).
	if v := version.Get(""); v != "" && v != "unknown" {
		SetUserAgent("yolo-jail-oauth-broker/" + v)
	}

	// The store before anything reads it: the canonical login, the view registrations and
	// the relay source all live under BrokerDir (store.go). The self-check grades the
	// canonical once one exists.
	ConfigureStore()
	if *selfCheck {
		return SelfCheck(*credsFile)
	}
	if *initCA || *forceInitCA {
		if err := EnsureCAAndLeaf(*forceInitCA); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		dir := BrokerDir()
		fmt.Printf("CA: %s\nleaf: %s\n", filepath.Join(dir, "ca.crt"), filepath.Join(dir, "server.crt"))
		return 0
	}

	if *socket == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --socket is required when running as a daemon.")
		fmt.Fprintln(os.Stderr, "       Use --init-ca for first-time setup.")
		return 2
	}

	// Ensure CA + leaf exist (jails need the CA at boot).
	if err := EnsureCAAndLeaf(false); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// The refresh flock lives in the broker state dir — the fixed rendezvous
	// path every broker instance agrees on for kernel-flock serialization.
	RefreshLockPath = filepath.Join(BrokerDir(), "refresh.lock")

	// Startup snapshot of the shared creds file — lets tomorrow's debugger see
	// the starting state and cross-reference it with do_refresh's drift lines.
	LogStartup(*credsFile)
	logInfo("startup: canonical=%s", describeCreds(CanonicalPath))

	stop := make(chan struct{})
	var stopOnce sync.Once
	shutdown := func() { stopOnce.Do(func() { close(stop) }) }
	// THE EXIT WAITS FOR A REFRESH IN FLIGHT, the background one included. A jail's end
	// SIGTERMs this daemon and does not wait for it (the manifest's `detach`,
	// docs/design/host-daemon-ownership.md §1.3), so the daemon itself is what lets a refresh
	// that has already redeemed the single-use refresh token upstream write its replacement
	// before the process goes. Its bound is the upstream request's own deadline (httpClient).
	// Serving stops first, and ServeFrontedUnix already waits for every handler it accepted.
	background := make(chan struct{})
	if *noBgRefresh {
		close(background)
	} else {
		go func() {
			defer close(background)
			RunBackgroundRefresher(*credsFile, stop,
				BackgroundRefreshTickSeconds, BackgroundRefreshLeadSeconds)
		}()
	}
	defer func() { shutdown(); <-background }()

	// hostservice.ServeUnix installs its own SIGTERM/SIGINT handler; wire a stop
	// channel too so the background refresher goroutine exits with the process.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigCh; shutdown() }()

	// The state dir holds what this process minted at startup (EnsureCAAndLeaf above)
	// and the refresh lock. When it goes — a launch retiring the loophole, a deleted HOME —
	// the daemon EXITS, so the next launch that selects claude spawns one that mints again,
	// instead of adopting this one and never getting a CA (hostservice.WatchStateDir).
	// A RETIRED STATE DIRECTORY IS A DELIBERATE EXIT, said by its own status
	// (hostservice.ExitStateDirRetired), so the supervisor that spawned this daemon reports it and
	// does not restart it: a restart would recreate the directory a launch just archived
	// (docs/design/host-daemon-ownership.md HD-D3, HD-D25).
	var retired atomic.Bool
	if err := hostservice.WatchStateDir(BrokerDir(), stop, func(reason string) {
		logInfo("%s", hostservice.StateDirGoneExitLine("yolo-claude-oauth-broker-host", reason))
		retired.Store(true)
		shutdown()
	}); err != nil {
		fmt.Fprintln(os.Stderr, "yolo-claude-oauth-broker-host: watch the state directory:", err)
		return 1
	}
	// A JAIL-LESS LAUNCH'S OWN BROKER (HD-R1, HD-D23; internal/cli's startHostViewBroker, which
	// keeps a `yolo host -- claude` credential view current): it ends with the launch's process,
	// which may have exec'd Claude in its place, and its socket is that launch's alone, so it
	// removes it as it goes. The exit still waits for a refresh in flight (the deferred wait above).
	if *endWithPID > 0 {
		defer func() { _ = os.Remove(*socket) }()
		hostservice.WatchProcess(*endWithPID, stop, func(reason string) {
			logInfo("%s", hostservice.ProcessGoneExitLine("yolo-claude-oauth-broker-host", reason))
			shutdown()
		})
	}

	// ServeFrontedUnix, and the third name is the whole point — see the no-`Serve`
	// note in internal/hostservice for the outage a signature-preserving change to
	// a shared helper caused here once.
	//
	// This used to be ServeUnix, on the true premise that the daemon was
	// host-to-host: the per-jail relay dialed it and stamped a jail_id into the
	// request. The relay is GONE (docs/design/broker-as-a-pack.md §7). The manifest
	// declares `publishes: "socket"`, so yolo runs the jail's svcendpoint front over
	// this socket (one daemon per jail since HD-R1), and every connection that
	// arrives here has crossed a jail boundary and carries yolo's CONNECTION
	// PREAMBLE. Reading it is what replaces the relay's stamp: the jail_id on this
	// daemon's audit line stays host-asserted (invariant I1), and the ~100 lines
	// that re-serialized someone else's JSON to keep it that way are deleted rather
	// than moved.
	//
	// Leaving ServeUnix here would be the silent-CORRUPTION direction: the preamble
	// would be consumed AS the client's request, and every Claude OAuth refresh in
	// every jail would fail on a connection both ends believe is healthy.
	if err := hostservice.ServeFrontedUnix(BuildHandler(*credsFile), *socket, stop); err != nil {
		fmt.Fprintln(os.Stderr, "yolo-claude-oauth-broker-host:", err)
		return 1
	}
	if retired.Load() {
		return hostservice.ExitStateDirRetired
	}
	return 0
}
