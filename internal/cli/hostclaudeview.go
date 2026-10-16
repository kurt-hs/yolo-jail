package cli

// hostclaudeview.go is the Claude credential view at the host notch
// (docs/design/claude-login-without-interception.md CL-D27): `yolo host -- claude`, with the jails'
// own switch YOLO_CLAUDE_CREDENTIAL_VIEW=1 in the invoking shell, starts Claude on the machine's
// shared login instead of the user's own. The host broker registers a view for it in a directory
// yolo manages (claudeview.HostLocation: <state dir>/host-agents/<pack>), writes the current access
// token there with no refresh token, and keeps it current; Claude is pointed at that directory by
// CLAUDE_SECURESTORAGE_CONFIG_DIR, the variable CL-D22's bridge already sets in a jail.
//
// OFF BY DEFAULT, and that is OQ-NC7's ruling kept rather than reopened: without the switch, host
// Claude keeps its own login in ~/.claude exactly as before, and with it ~/.claude is still never
// written — the managed store is a second directory, which is OQ-OA3's precedent (`yolo host --
// codex` shares the OpenAI login through a managed home and leaves ~/.codex alone). A view holds
// no refresh token, so the host Claude is "a second refresher of nothing" (§8): it races nothing.
// claudeview.DefaultOn("host") stays false until the measures pass, like every backend's.
//
// NEVER FATAL, like the jail's registration: a launch whose view could not be set up starts with
// nothing changed, Claude on the user's own login, and says why and what to look at.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/execx"
	"github.com/mschulkind-oss/yolo-jail/internal/logcap"
	"github.com/mschulkind-oss/yolo-jail/internal/oauthbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// hostClaudeBrokerEnsure starts THIS LAUNCH'S OWN claude-oauth-broker, which keeps the view
// current for as long as the launch's process lives, with its lines on errw (a host agent's stdout
// is its own). A variable so a test can stand in for the daemon; the registration after it is
// real.
//
// A DAEMON OF ITS OWN, AS EVERY JAIL-LESS ACTOR'S IS (HD-R1, HD-D11 and HD-D23 in
// docs/design/host-daemon-ownership.md): it used to ensure the one machine-wide broker, which
// HD-R1 retired, and to refuse one too old to keep host views (CL-D28's mark). The daemon spawned
// here is this build's, so it keeps host views by construction. It is handed this process's pid
// (`--end-with-pid`), which the exec into Claude keeps, so it ends with the agent; it shares the
// credential, the refresh lock and the replaced-token record with every jail's broker (HD-D16).
var hostClaudeBrokerEnsure = func(errw io.Writer) error {
	_, err := startHostViewBroker(errw)
	return err
}

// hostViewBrokerStartTimeout bounds the wait for the spawned broker's socket to accept.
const hostViewBrokerStartTimeout = 5 * time.Second

// hostViewBrokerArgv is the private broker's argv: yolo's own daemon at socket, ending with pid.
func hostViewBrokerArgv(socket string, pid int) []string {
	return execx.SelfExecArgv([]string{"yolo", "internal", "daemon", broker.BrokerLoopholeName,
		"--socket", socket, "--end-with-pid", strconv.Itoa(pid)})
}

// startHostViewBroker spawns the private broker and returns its socket once it accepts. Its log is
// the one every copy of the broker appends to (broker.BrokerLogPath).
func startHostViewBroker(errw io.Writer) (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("name the broker's socket: %w", err)
	}
	socket := filepath.Join(paths.HostSingletonDir,
		fmt.Sprintf("yolo-claude-host-%d-%s.sock", os.Getpid(), hex.EncodeToString(b)))
	argv := hostViewBrokerArgv(socket, os.Getpid())
	logPath := broker.BrokerLogPath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	cmd := exec.Command(argv[0], argv[1:]...)
	if lf, lerr := logcap.Open(logPath, 0o644); lerr == nil {
		cmd.Stdout, cmd.Stderr = lf, lf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start the %s for this launch: %w", broker.BrokerLoopholeName, err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	for deadline := time.Now().Add(hostViewBrokerStartTimeout); ; time.Sleep(25 * time.Millisecond) {
		if broker.SingletonReachable(socket, time.Second) {
			return socket, nil
		}
		select {
		case <-exited:
			return "", fmt.Errorf("the %s this launch started exited at startup; its log: %s",
				broker.BrokerLoopholeName, logPath)
		default:
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
			return "", fmt.Errorf("the %s this launch started did not answer within %s; its log: %s",
				broker.BrokerLoopholeName, hostViewBrokerStartTimeout, logPath)
		}
	}
}

// hostClaudeView returns environ with the launched Claude pointed at its view, when this launch
// gives it one, and environ unchanged otherwise. Three things must hold, and the first that does
// not ends it silently: the switch resolves on for the host; the launched program's own pack
// links Claude's credential through a `shared_credentials` hook (the pack is the Claude the view is
// for, run.ClaudeSharedCredentialDir); and the claude-oauth-broker loophole is active with a pack
// that may run host code (run.PromisedEndpoints, the jail launch's own predicate).
func hostClaudeView(launch *hostComposition, environ []string, errw io.Writer) []string {
	if config.InJail() || !claudeview.Selected(claudeview.HostRuntime, os.Getenv) {
		return environ
	}
	pack := hostInstallingPack(launch.packs, launch.agent)
	if pack == nil || run.ClaudeSharedCredentialDir([]*packload.Pack{pack}) == "" {
		return environ
	}
	if !slices.Contains(run.PromisedEndpoints(claudeview.HostRuntime, launch.cfg), broker.BrokerLoopholeName) {
		fmt.Fprintf(errw, "yolo host: %s=1 asks for the machine's shared Claude login, and the "+
			"%s loophole is not active here (disabled in your config, or its pack may not run host "+
			"code), so %s starts on its own login in ~/.claude; `yolo loopholes status` says "+
			"which\n", claudeview.SwitchEnv, broker.BrokerLoopholeName, launch.agent)
		return environ
	}
	loc := claudeview.HostLocation(pack.Name)
	res, err := registerHostClaudeView(loc, errw)
	if err != nil {
		fmt.Fprintf(errw, "yolo host: Warning: could not give %s the machine's shared Claude "+
			"login (%v), so it starts on its own login in ~/.claude. `yolo claude-auth status` "+
			"shows the broker's store; unset %s to stop asking\n", launch.agent, err, claudeview.SwitchEnv)
		return environ
	}
	fmt.Fprintf(errw, "yolo host: %s reads the machine's shared Claude login from a store yolo "+
		"manages, %s, which the host broker keeps current (%s=1); your own ~/.claude is not "+
		"touched\n", launch.agent, homeTilde(loc.Dir), claudeview.SwitchEnv)
	if res.WasLoggedOut {
		fmt.Fprintf(errw, "yolo host: /logout in an earlier `yolo host -- %s` had signed this store "+
			"out of the machine's login; this launch signs it back in. `yolo claude-auth logout` "+
			"signs the whole machine out\n", launch.agent)
	}
	if !res.Wrote {
		fmt.Fprintf(errw, "yolo host: this machine has no shared Claude login yet; /login in this "+
			"session enrolls the machine, for every jail and `yolo host` launch that shares it\n")
	}
	return setEnviron(environ, claudeview.SecureStorageEnv, loc.Dir)
}

// registerHostClaudeView starts this launch's broker and registers loc with it.
func registerHostClaudeView(loc claudeview.Location, errw io.Writer) (oauthbroker.RegisterResult, error) {
	if err := hostClaudeBrokerEnsure(errw); err != nil {
		return oauthbroker.RegisterResult{}, err
	}
	oauthbroker.ConfigureStore()
	return oauthbroker.RegisterView(loc, claudeview.HostRuntime, "")
}

// hostInstallingPack is the selected pack that installs bin, nil when none does.
func hostInstallingPack(packs []*packload.Pack, bin string) *packload.Pack {
	name := installingPack(packs, bin)
	if name == "" {
		return nil
	}
	for _, p := range packs {
		if p != nil && p.Name == name {
			return p
		}
	}
	return nil
}

// setEnviron is environ with key set to value: every existing entry for key removed, so the child
// sees one value whichever entry its libc reads first, and the new one appended.
func setEnviron(environ []string, key, value string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}
