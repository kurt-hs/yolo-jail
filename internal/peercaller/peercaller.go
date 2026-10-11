// Package peercaller names the process on the other end of a loopback TCP connection, and the
// yolo agent it belongs to, for the request logs of the in-jail credential adapters
// (aws-credential-adapter, openai-auth-adapter; docs/reference/agent-credentials.md, "The
// adapters' logs"). Those protocols carry no caller name: an AWS SDK's container-credentials
// fetch is a bare GET with a token, and the token is one per launch, shared by every agent whose
// profile selects Bedrock, so neither the request nor its token can say which agent sent it, and
// a request with NO token (the 2026-10-11 incident: Claude Code with CLAUDE_CODE_USE_BEDROCK set
// while yolo had selected its subscription) carries nothing at all.
//
// What the kernel knows is enough. The peer's address names one socket in this network
// namespace's /proc/net/tcp (or tcp6), whose inode one process's /proc/<pid>/fd links to. From
// that process the walk goes up PPid until a process is one of this jail's agents: a name in
// the agent vocabulary, which is every agent yolo generated a launcher or an env file for
// (~/.yolo/bin/launch/<name>, ~/.config/yolo-agent-env/<name>.sh). A process matches when its
// comm, its argv[0]'s base name or its argv[1]'s (an interpreter running the agent's script,
// `node …/bin/pi`) is such a name. So the AWS CLI a Claude Code Bash tool ran is attributed to
// claude, and named as the process that asked.
//
// # What it never does
//
//   - It logs NO argv and no environment: only the agent's vocabulary name, the asking
//     process's comm (the kernel's 15-byte name), and pids. An argv can hold a secret; a comm
//     is what `ps` shows anyone.
//   - It never blocks a request. Resolve honors its context, which the caller bounds by
//     Budget, and every failure is a Caller whose Agent is "" and whose Why says what failed.
//
// # Where it cannot answer
//
// Linux only: on darwin there is no /proc, and the one darwin caller is the macos-user doorway,
// where the adapter runs host-side, outside the sandbox. A peer outside this jail's pid
// namespace (a host process or a sibling jail on a loopback shared by `network.mode: host`)
// holds a socket no process here links to, and Why says exactly that.
package peercaller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// Budget bounds one lookup, measured from the request's start. The lookup runs beside the
// handler and the response waits for it at most this long (internal/reqlog's ResolveCallers),
// so a slow /proc costs the log a name and the client no more than this.
const Budget = 50 * time.Millisecond

// Unknown is the caller value for a lookup that named no agent.
const Unknown = "unknown"

// The agent vocabulary's two directories, home-relative. The launch directory is
// entrypoint.Env.LaunchDir and the env-file directory is entrypoint.AgentEnvDirRel; a test pins
// both spellings to those authorities, so this leaf stays free of the entrypoint package.
const (
	LaunchDirRel   = ".yolo/bin/launch"
	AgentEnvDirRel = ".config/yolo-agent-env"
)

// maxDepth bounds the walk up the process tree; a real chain is a handful deep.
const maxDepth = 64

// Caller is what a lookup found.
type Caller struct {
	// Agent is the yolo agent the asking process belongs to, "" when the lookup named none.
	Agent string
	// AgentPID is the agent process's pid, 0 when Agent is "".
	AgentPID int
	// Process is the comm of the process holding the peer socket, "" when none was found.
	Process string
	// ProcessPID is that process's pid, 0 when none was found.
	ProcessPID int
	// Why says why Agent is "", and is "" when it is not.
	Why string
}

// Fields renders the caller as the request line's key/value pairs, in order:
//
//	caller=claude caller_pid=136                                    the agent asked itself
//	caller=claude caller_pid=136 caller_process=aws caller_process_pid=9123   a process it ran asked
//	caller=unknown caller_process=curl caller_process_pid=9200 caller_why="…"  no agent above it
//	caller=unknown caller_why="…"                                   no process found
func (c Caller) Fields() [][2]string {
	var out [][2]string
	if c.Agent != "" {
		out = append(out, [2]string{"caller", c.Agent}, [2]string{"caller_pid", strconv.Itoa(c.AgentPID)})
	} else {
		out = append(out, [2]string{"caller", Unknown})
	}
	if c.ProcessPID != 0 && c.ProcessPID != c.AgentPID {
		out = append(out, [2]string{"caller_process", c.Process},
			[2]string{"caller_process_pid", strconv.Itoa(c.ProcessPID)})
	}
	if c.Agent == "" && c.Why != "" {
		out = append(out, [2]string{"caller_why", c.Why})
	}
	return out
}

// Resolver looks callers up in one /proc and one home. The zero value reads /proc and $HOME.
type Resolver struct {
	// ProcRoot is the /proc to read; "" is /proc.
	ProcRoot string
	// Home is the jail home whose agent vocabulary is read; "" is $HOME at lookup time.
	Home string
	// GOOS is the platform to answer for; "" is the running one. A test reaches the darwin
	// answer through it.
	GOOS string
}

// Resolve names the caller whose connection runs from peer to local, both "host:port". It
// returns when ctx is done at the latest, with Why saying the lookup ran out of time.
func (r Resolver) Resolve(ctx context.Context, local, peer string) Caller {
	if goos := r.goos(); goos != "linux" {
		return Caller{Why: "naming the calling process needs Linux's /proc, and this adapter runs on " +
			goos + " (on macos-user it runs host-side, outside the sandbox); the line keeps peer only"}
	}
	peerAP, err := netip.ParseAddrPort(peer)
	if err != nil {
		return Caller{Why: "the peer is not a TCP address"}
	}
	localAP, err := netip.ParseAddrPort(local)
	if err != nil {
		return Caller{Why: "the request names no local TCP address"}
	}
	inode, err := r.socketInode(peerAP, localAP)
	if err != nil {
		return Caller{Why: err.Error()}
	}
	pid, err := r.socketOwner(ctx, inode)
	if err != nil {
		return Caller{Why: err.Error()}
	}
	c := Caller{Process: r.comm(pid), ProcessPID: pid}
	agents := r.vocabulary()
	if len(agents) == 0 {
		c.Why = "this home has no agent launchers or agent env files to name an agent by"
		return c
	}
	for p, depth := pid, 0; p > 0 && depth < maxDepth; depth++ {
		if ctx.Err() != nil {
			c.Why = "the lookup ran past its " + Budget.String() + " budget"
			return c
		}
		if name := r.agentName(p, agents); name != "" {
			c.Agent, c.AgentPID = name, p
			return c
		}
		next, ok := r.parent(p)
		if !ok || next == p {
			break
		}
		p = next
	}
	c.Why = "no yolo agent among the asking process's ancestors"
	return c
}

func (r Resolver) goos() string {
	if r.GOOS != "" {
		return r.GOOS
	}
	return goruntime.GOOS
}

func (r Resolver) proc(parts ...string) string {
	root := r.ProcRoot
	if root == "" {
		root = "/proc"
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// socketInode finds the CLIENT end of the connection: the socket whose local address is the
// peer's and whose remote address is ours. Both ends are listed in this namespace's table; the
// server end, the other way round, is the adapter's own.
func (r Resolver) socketInode(peer, local netip.AddrPort) (string, error) {
	peer = netip.AddrPortFrom(peer.Addr().Unmap(), peer.Port())
	local = netip.AddrPortFrom(local.Addr().Unmap(), local.Port())
	read := 0
	for _, table := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile(r.proc("net", table))
		if err != nil {
			continue
		}
		read++
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Scan() // the header
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 10 {
				continue
			}
			l, errL := parseProcAddr(f[1])
			rem, errR := parseProcAddr(f[2])
			if errL != nil || errR != nil || l != peer || rem != local {
				continue
			}
			if f[9] == "0" {
				return "", errors.New("the peer's socket has no inode (already closing)")
			}
			return f[9], nil
		}
	}
	if read == 0 {
		return "", errors.New("cannot read " + r.proc("net", "tcp"))
	}
	return "", errors.New("no socket in this network namespace has the peer's address (it closed, " +
		"or it is outside the jail on a loopback shared by network.mode host)")
}

// socketOwner finds the process with a descriptor on socket:[inode].
func (r Resolver) socketOwner(ctx context.Context, inode string) (int, error) {
	entries, err := os.ReadDir(r.proc())
	if err != nil {
		return 0, fmt.Errorf("cannot list %s", r.proc())
	}
	want := "socket:[" + inode + "]"
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if ctx.Err() != nil {
			return 0, errors.New("the lookup ran past its " + Budget.String() + " budget")
		}
		fdDir := r.proc(e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // another user's process, or one that just exited
		}
		for _, fd := range fds {
			if link, err := os.Readlink(filepath.Join(fdDir, fd.Name())); err == nil && link == want {
				return pid, nil
			}
		}
	}
	return 0, errors.New("no process in this jail holds the peer's socket (it is outside the " +
		"jail's pid namespace, another user's, or already gone)")
}

func (r Resolver) comm(pid int) string {
	b, err := os.ReadFile(r.proc(strconv.Itoa(pid), "comm"))
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
}

// parent is pid's PPid, from /proc/<pid>/status (stat's comm field can hold spaces and parens).
func (r Resolver) parent(pid int) (int, bool) {
	b, err := os.ReadFile(r.proc(strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PPid:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			return n, err == nil
		}
	}
	return 0, false
}

// agentName is the agent pid runs as, or "": its comm, or the base name of argv[0] or argv[1].
// Only the matched vocabulary name leaves this function, never an argv word.
func (r Resolver) agentName(pid int, agents map[string]bool) string {
	names := []string{r.comm(pid)}
	if b, err := os.ReadFile(r.proc(strconv.Itoa(pid), "cmdline")); err == nil {
		argv := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		for i := 0; i < len(argv) && i < 2; i++ {
			if argv[i] != "" && !strings.HasPrefix(argv[i], "-") {
				names = append(names, filepath.Base(argv[i]))
			}
		}
	}
	for _, n := range names {
		if agents[n] {
			return n
		}
	}
	return ""
}

// vocabulary is the set of agent names this home has a launcher or an env file for.
func (r Resolver) vocabulary() map[string]bool {
	home := r.Home
	if home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		return nil
	}
	out := map[string]bool{}
	if es, err := os.ReadDir(filepath.Join(home, LaunchDirRel)); err == nil {
		for _, e := range es {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				out[e.Name()] = true
			}
		}
	}
	if es, err := os.ReadDir(filepath.Join(home, AgentEnvDirRel)); err == nil {
		for _, e := range es {
			if name, ok := strings.CutSuffix(e.Name(), ".sh"); ok && name != "" && !e.IsDir() {
				out[name] = true
			}
		}
	}
	return out
}

// parseProcAddr reads one /proc/net/tcp{,6} address, "0100007F:1F90". The kernel prints each
// 32-bit word of the address as the native-endian integer its bytes make, and the port in host
// order.
func parseProcAddr(s string) (netip.AddrPort, error) {
	addrHex, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return netip.AddrPort{}, errors.New("no port")
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return netip.AddrPort{}, err
	}
	raw, err := hex.DecodeString(addrHex)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.AddrPort{}, errors.New("not an address")
	}
	b := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(b[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	addr, _ := netip.AddrFromSlice(b)
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), nil
}

// FormatProcAddr is parseProcAddr's inverse, for a test that writes a /proc/net/tcp.
func FormatProcAddr(ap netip.AddrPort) string {
	var raw []byte
	if ap.Addr().Is4() {
		a := ap.Addr().As4()
		raw = a[:]
	} else {
		a := ap.Addr().As16()
		raw = a[:]
	}
	var b strings.Builder
	for i := 0; i < len(raw); i += 4 {
		fmt.Fprintf(&b, "%08X", binary.NativeEndian.Uint32(raw[i:]))
	}
	return fmt.Sprintf("%s:%04X", b.String(), ap.Port())
}

// Lookup is Resolve with the zero Resolver, rendered as the request line's fields: the shape
// internal/reqlog's ResolveCallers takes.
func Lookup(ctx context.Context, local, peer string) [][2]string {
	return Resolver{}.Resolve(ctx, local, peer).Fields()
}
