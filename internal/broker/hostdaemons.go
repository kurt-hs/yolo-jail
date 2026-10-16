// WHAT IS RUNNING: the set the `yolo host-daemon` verb acts over.
//
// THE SET IS WHAT THE JAILS ON THIS MACHINE ARE RUNNING (docs/design/host-daemon-ownership.md
// §5.2), derived from disk and never listed. Since HD-R1 every host daemon is spawned by the
// launch that wanted it, binds a socket keyed by that launch's jail
// (paths.FrontSocketFile: <dir>/yolo-front-<key>-<name>.sock), and ends with it. So a socket
// there is a jail's running copy, and its key names the jail.
//
// THE SECOND SOURCE IS AN OLDER YOLO'S SINGLETON: a machine-wide daemon it started at the
// name-keyed paths (paths.HostSingleton*), which may still be serving the jails that yolo
// launched. Found by its lock and its PID file, as the verb found singletons before; reported,
// and stoppable, never started.
//
// Nothing here may grow a list of names: the census is the disk.
package broker

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Instance is one running copy of a host daemon on this machine.
type Instance struct {
	// Name is the loophole the daemon belongs to, e.g. "claude-oauth-broker".
	Name string `json:"loophole"`
	// Jail is the key the copy's socket is keyed by — the container name's 8-hex hash, or a
	// hash of a macos-user or `yolo host` session's own dir — and "" for a host-wide copy.
	Jail string `json:"jail,omitempty"`
	// HostWide marks the machine-wide copy an OLDER yolo started (HD-R1 retired them), which
	// serves the jails that yolo launched and is no jail's own.
	HostWide bool `json:"host_wide"`
	// Socket is the AF_UNIX socket the copy answers on, behind each jail's front.
	Socket string `json:"socket"`
	// PIDs are the processes serving it, found by their `--socket` argument or a host-wide
	// copy's PID file; empty when none could be found (no pgrep on this host).
	PIDs []int `json:"pids"`
	// Accepting is the socket's accept answer (SingletonReachable).
	Accepting bool `json:"socket_accepting"`
}

// Healthy reports a copy whose socket accepts.
func (i Instance) Healthy() bool { return i.Accepting }

// frontSocketName matches a jail's fronted host-daemon socket and captures its key and name.
var frontSocketName = regexp.MustCompile(`^yolo-front-([0-9a-f]{8})-(.+)\.sock$`)

// RunningInstances lists every running copy of the host daemon called name, or of every host
// daemon when name is "": each jail's own first, sorted by name and then jail, then any
// host-wide copy an older yolo left.
//
// SILENT ON EVERY FAILURE, empty on any doubt: a verb that invented a copy from an unreadable
// directory would offer to stop a daemon that does not exist.
func RunningInstances(name string) []Instance {
	var out []Instance
	matches, _ := filepath.Glob(paths.FrontSocketGlob())
	for _, m := range matches {
		parts := frontSocketName.FindStringSubmatch(filepath.Base(m))
		if parts == nil || (name != "" && parts[2] != name) {
			continue
		}
		out = append(out, Instance{Name: parts[2], Jail: parts[1], Socket: m,
			PIDs: RealPgrepStrays(m), Accepting: SingletonReachable(m, time.Second)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Jail < out[j].Jail
	})
	for _, n := range RendezvousSingletonNames() {
		if name != "" && n != name {
			continue
		}
		deps := LegacyDeps(n)
		st := BrokerStatus(deps)
		if !st.PIDLive && !st.SocketExists {
			continue // a lock an older yolo left, and nothing running behind it
		}
		inst := Instance{Name: n, HostWide: true, Socket: st.Socket, Accepting: st.Reachable}
		if st.PIDLive {
			inst.PIDs = []int{st.PID}
		}
		out = append(out, inst)
	}
	return out
}

// jailStragglers returns the PIDs of every process serving a jail's copy of name, found by its
// `--socket` argument whether or not its socket is still there: a copy whose jail ended and
// that is finishing a request in flight (HD-R1's detached straggler) has unlinked nothing but is
// no longer listed by its socket. Used by stop alone.
func jailStragglers(name string) []int {
	return pgrepArgv("(^| )--socket " + regexp.QuoteMeta(paths.FrontSocketDir+"/yolo-front-") +
		"[0-9a-f]{8}-" + regexp.QuoteMeta(name) + `\.sock( |$)`)
}

// DeclaredHostDaemons is every loophole this machine declares a host daemon for, sorted: the
// names a launch would start a copy of. For a refusal that lists what the verb knows.
func DeclaredHostDaemons() []string {
	var out []string
	for _, lp := range loopholes.NewHostSet(nil).All() {
		if lp.HostDaemon != nil {
			out = append(out, lp.Name)
		}
	}
	sort.Strings(out)
	return out
}

// KnownHostDaemon reports whether name is a host daemon this machine declares, one that is
// running, or the Claude broker, which the `yolo broker` alias always names.
func KnownHostDaemon(name string) bool {
	if name == BrokerLoopholeName {
		return true
	}
	for _, n := range DeclaredHostDaemons() {
		if n == name {
			return true
		}
	}
	return len(RunningInstances(name)) > 0
}

// rendezvousGlob matches every machine-wide daemon's LOCK (paths.HostSingletonGlob).
//
// THE LOCK, NOT THE PID FILE: the `/tmp/yolo-*.pid` namespace has OTHER OWNERS (the in-jail
// supervisor's `/tmp/yolo-jaild.pid`), and a `.pid` scan once produced a member called `jaild`
// whose stop would have killed the jail's supervisor. The lock had exactly one writer, an older
// yolo's ensure, and since 2026-10 one more: the OpenAI legacy-state migration's lock
// (internal/cli/run's migrateLegacyOpenAIAuthState), which RunningInstances skips because nothing
// runs behind it.
func rendezvousGlob() string { return paths.HostSingletonGlob() }

// RendezvousSingletonNames is the loophole names an older yolo ensured a machine-wide daemon
// for on this host. Silent on every failure, empty on any doubt.
func RendezvousSingletonNames() []string {
	matches, err := filepath.Glob(rendezvousGlob())
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range matches {
		base := filepath.Base(m)
		name := strings.TrimSuffix(strings.TrimPrefix(base, "yolo-"), ".lock")
		if name == "" {
			continue
		}
		// The path must round-trip: a file that merely LOOKS like the pattern is not a
		// rendezvous.
		if paths.HostSingletonLock(name) != m {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
