package run

// macosusercredentialservice_test.go pins the macos-user arm's OpenAI credential service under
// HD-R1 (docs/design/host-daemon-ownership.md, hostdaemonperjail.go): each session starts its
// own daemon, in its own host-services dir and keyed by it, and hands the doorways outside the
// sandbox that daemon's private socket, never the machine-wide one an older yolo's singleton
// answers on. A machine-wide daemon is left alone: it serves the jails an older yolo launched.
//
// What these replaced, and why it is gone rather than moved: three tests of the singleton's
// at-most-twice ensure on this arm (a reused daemon that stopped between the ensure and the
// launch's connect was ensured once more). A session's own daemon is spawned, never reused, so
// no other launch can stop it in that gap, and a spawn that fails is reported once, by the spawn.

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/openaiauthdaemon"
	"github.com/mschulkind-oss/yolo-jail/internal/openauthclient"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// THE SESSION'S OWN DAEMON, AND THE DOORWAY POINTED AT IT. A live machine-wide daemon (an older
// yolo's singleton, here a fixture) is neither fronted nor stopped; the launch spawns the
// session's own, publishes its endpoint in the session's dir, and hands the Codex doorway that
// daemon's private socket, which answers while the command runs and is gone after. Hand the
// doorway openaiauthhost.HostSocketPath() again and its socket is the machine-wide one.
func TestTheMacosUserArmStartsTheSessionsOwnOpenAIService(t *testing.T) {
	o, stderr, seen := codexNativeLaunch(t)
	doors := observeDoorways(t)
	singletonFixture(t, openAIAuthBrokerName)
	var hostSocket string
	var pingErr error
	run := o.MacosUserRun
	o.MacosUserRun = func(cfg *jsonx.OrderedMap, ws string, a, b []string, c, d string, h macosuser.HomeOverlay,
		ctx macosuser.HostContext, dry bool, env *jsonx.OrderedMap, bt []packload.BlockedTool, jd macosuser.JailDaemons) int {
		_, in := doors.only(t, openAIAuthBrokerName)
		hostSocket = in[openauthclient.HostSocketEnv]
		_, pingErr = openauthclient.RequestUnix(hostSocket, map[string]any{"action": "ping"}, &bytes.Buffer{})
		return run(cfg, ws, a, b, c, d, h, ctx, dry, env, bt, jd)
	}
	if rc := Run(*o); rc != 0 || !seen.reached {
		t.Fatalf("Run() = %d (reached the command: %v)\n%s", rc, seen.reached, stderr.String())
	}
	endpoint, _ := seen.env.Get(hostServiceEnvVar(openAIAuthBrokerName))
	sessionDir := filepath.Dir(endpoint.(string))
	if !strings.HasPrefix(filepath.Base(sessionDir), hostServicesDirPrefix) {
		t.Fatalf("the command was handed %v, not an endpoint in a session dir", endpoint)
	}
	want := openaiauthdaemon.HostSocketPath(frontSocketFile(frontShortHash(sessionDir), openAIAuthBrokerName))
	if hostSocket != want {
		t.Errorf("the doorway was handed the OpenAI private socket %q, want the session's own daemon's %q",
			hostSocket, want)
	}
	if pingErr != nil {
		t.Errorf("the session's own OpenAI daemon did not answer on %s while the command ran: %v", hostSocket, pingErr)
	}
	if fileExists(hostSocket) {
		t.Errorf("the session's OpenAI daemon left its private socket %s after the session ended", hostSocket)
	}
	conn, err := net.DialTimeout("unix", paths.HostSingletonSocket(openAIAuthBrokerName), time.Second)
	if err != nil {
		t.Errorf("the launch stopped the machine-wide OpenAI daemon an older yolo's jails would be using: %v", err)
	} else {
		_ = conn.Close()
	}
}

// A SESSION'S OWN DAEMON THAT DOES NOT START REFUSES THE LAUNCH, before the command runs, and the
// refusal names the next step that fits a daemon of the session's own: its log, then a launch
// again, never a command that restarts a shared daemon. The daemon is made to fail by putting a
// file where its state dir goes, so it exits at its first MkdirAll.
func TestTheMacosUserArmRefusesWhenItsOwnOpenAIServiceDoesNotStart(t *testing.T) {
	o, stderr, seen := codexNativeLaunch(t)
	observeDoorways(t)
	stateDir := loopholes.StateDirFor(openAIAuthBrokerName)
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc := Run(*o); rc != 1 || seen.reached {
		t.Fatalf("Run() = %d (reached the command: %v), want the launch refused before the command\n%s",
			rc, seen.reached, stderr.String())
	}
	out := stderr.String()
	for _, want := range []string{"OpenAI credential service did not start; refusing the macos-user launch.",
		broker.SingletonLogPath(openAIAuthBrokerName), "then launch again"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, broker.CycleCommand(openAIAuthBrokerName)) {
		t.Errorf("the refusal names %q, which restarts no daemon of this session's:\n%s",
			broker.CycleCommand(openAIAuthBrokerName), out)
	}
}
