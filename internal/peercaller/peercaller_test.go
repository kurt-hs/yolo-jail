package peercaller

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// fakeProc is a /proc tree a test writes: one TCP table and a set of processes.
type fakeProc struct {
	t    *testing.T
	root string
	tcp  []string
}

type fakeProcess struct {
	pid, ppid int
	comm      string
	argv      []string
	sockets   []string // inodes this process holds
}

func newFakeProc(t *testing.T) *fakeProc {
	t.Helper()
	return &fakeProc{t: t, root: t.TempDir()}
}

func (f *fakeProc) conn(local, remote netip.AddrPort, inode string) {
	f.tcp = append(f.tcp, fmt.Sprintf("   %d: %s %s 01 00000000:00000000 00:00000000 00000000  1000        0 %s 1 0000000000000000 20 4 30 10 -1",
		len(f.tcp), FormatProcAddr(local), FormatProcAddr(remote), inode))
}

func (f *fakeProc) process(p fakeProcess) {
	f.t.Helper()
	dir := filepath.Join(f.root, strconv.Itoa(p.pid))
	must(f.t, os.MkdirAll(filepath.Join(dir, "fd"), 0o755))
	must(f.t, os.WriteFile(filepath.Join(dir, "comm"), []byte(p.comm+"\n"), 0o644))
	must(f.t, os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(p.argv, "\x00")+"\x00"), 0o644))
	must(f.t, os.WriteFile(filepath.Join(dir, "status"), []byte(fmt.Sprintf("Name:\t%s\nPid:\t%d\nPPid:\t%d\n", p.comm, p.pid, p.ppid)), 0o644))
	must(f.t, os.Symlink("/dev/null", filepath.Join(dir, "fd", "0")))
	for i, inode := range p.sockets {
		must(f.t, os.Symlink("socket:["+inode+"]", filepath.Join(dir, "fd", strconv.Itoa(i+3))))
	}
}

func (f *fakeProc) write() {
	f.t.Helper()
	must(f.t, os.MkdirAll(filepath.Join(f.root, "net"), 0o755))
	header := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	must(f.t, os.WriteFile(filepath.Join(f.root, "net", "tcp"), []byte(header+strings.Join(f.tcp, "\n")+"\n"), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// homeWithAgents is a jail home with a launcher for each of launchers and an env file for each
// of envFiles.
func homeWithAgents(t *testing.T, launchers, envFiles []string) string {
	t.Helper()
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, LaunchDirRel), 0o755))
	must(t, os.MkdirAll(filepath.Join(home, AgentEnvDirRel), 0o755))
	for _, n := range launchers {
		must(t, os.WriteFile(filepath.Join(home, LaunchDirRel, n), []byte("#!/bin/bash\n"), 0o755))
	}
	for _, n := range envFiles {
		must(t, os.WriteFile(filepath.Join(home, AgentEnvDirRel, n+".sh"), nil, 0o644))
	}
	return home
}

var (
	server = netip.MustParseAddrPort("127.0.0.1:1461")
	client = netip.MustParseAddrPort("127.0.0.1:40122")
)

func resolve(t *testing.T, r Resolver) Caller {
	t.Helper()
	r.GOOS = "linux"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := r.Resolve(ctx, server.String(), client.String())
	t.Logf("%+v -> %v", c, c.Fields())
	return c
}

// THE INCIDENT'S SHAPE: an AWS CLI a Claude Code tool ran asks with no token. The line names
// claude, and the process that asked.
func TestAProcessAnAgentRanIsAttributedToTheAgent(t *testing.T) {
	p := newFakeProc(t)
	p.conn(server, client, "5001") // the adapter's end
	p.conn(client, server, "5002") // the caller's end
	p.write()
	p.process(fakeProcess{pid: 64, ppid: 54, comm: "yolo-jaild", argv: []string{"yolo-jaild", "aws-credential-adapter"}, sockets: []string{"5001"}})
	p.process(fakeProcess{pid: 136, ppid: 0, comm: "claude", argv: []string{"/home/agent/.local/bin/claude", "--dangerously-skip-permissions"}})
	p.process(fakeProcess{pid: 900, ppid: 136, comm: "bash", argv: []string{"/bin/bash", "-c", "aws sts get-caller-identity"}})
	p.process(fakeProcess{pid: 901, ppid: 900, comm: "aws", argv: []string{"/usr/bin/python3", "/usr/bin/aws", "sts", "SECRET-ARG"}, sockets: []string{"5002"}})
	home := homeWithAgents(t, []string{"claude", "pi"}, nil)

	c := resolve(t, Resolver{ProcRoot: p.root, Home: home})
	want := Caller{Agent: "claude", AgentPID: 136, Process: "aws", ProcessPID: 901}
	if c != want {
		t.Fatalf("caller = %+v, want %+v", c, want)
	}
	got := fmt.Sprint(c.Fields())
	if got != "[[caller claude] [caller_pid 136] [caller_process aws] [caller_process_pid 901]]" {
		t.Errorf("fields = %s", got)
	}
	if strings.Contains(got, "SECRET-ARG") || strings.Contains(got, "sts") {
		t.Errorf("an argv word reached the fields: %s", got)
	}
}

// An agent run by an interpreter (`node …/bin/pi`) is named by argv[1], and an agent known only
// by its env file counts.
func TestAnInterpretedAgentIsNamedByItsScript(t *testing.T) {
	p := newFakeProc(t)
	p.conn(client, server, "7")
	p.write()
	p.process(fakeProcess{pid: 50, ppid: 1, comm: "node", argv: []string{"node", "/home/agent/.npm-global/bin/pi"}, sockets: []string{"7"}})
	c := resolve(t, Resolver{ProcRoot: p.root, Home: homeWithAgents(t, nil, []string{"pi"})})
	if c.Agent != "pi" || c.AgentPID != 50 || fmt.Sprint(c.Fields()) != "[[caller pi] [caller_pid 50]]" {
		t.Errorf("caller = %+v, fields %v", c, c.Fields())
	}
}

// Every failure is caller=unknown with the reason, and names the process when one was found.
func TestAFailedLookupSaysWhy(t *testing.T) {
	p := newFakeProc(t)
	p.conn(client, server, "7")
	p.write()
	p.process(fakeProcess{pid: 50, ppid: 0, comm: "curl", argv: []string{"curl", "http://127.0.0.1:1461/credentials"}, sockets: []string{"7"}})
	home := homeWithAgents(t, []string{"claude"}, nil)

	c := resolve(t, Resolver{ProcRoot: p.root, Home: home})
	if c.Agent != "" || c.Process != "curl" || c.ProcessPID != 50 || !strings.Contains(c.Why, "no yolo agent among") {
		t.Errorf("no agent above curl: %+v", c)
	}
	if f := fmt.Sprint(c.Fields()); !strings.HasPrefix(f, "[[caller unknown] [caller_process curl] [caller_process_pid 50] [caller_why ") {
		t.Errorf("fields = %s", f)
	}

	// A peer no process here holds: a host process on a shared loopback.
	q := newFakeProc(t)
	q.conn(client, server, "7")
	q.write()
	if c := resolve(t, Resolver{ProcRoot: q.root, Home: home}); c.Agent != "" || !strings.Contains(c.Why, "no process in this jail holds") {
		t.Errorf("unheld socket: %+v", c)
	}
	// A peer with no socket in this namespace at all.
	r := newFakeProc(t)
	r.write()
	if c := resolve(t, Resolver{ProcRoot: r.root, Home: home}); !strings.Contains(c.Why, "no socket in this network namespace") {
		t.Errorf("no socket: %+v", c)
	}
	// A context already done stops the scan.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c := (Resolver{ProcRoot: p.root, Home: home, GOOS: "linux"}).Resolve(ctx, server.String(), client.String()); !strings.Contains(c.Why, "budget") {
		t.Errorf("a done context: %+v", c)
	}
	// darwin has no /proc: the macos-user doorway says so.
	if c := (Resolver{GOOS: "darwin"}).Resolve(context.Background(), server.String(), client.String()); !strings.Contains(c.Why, "needs Linux's /proc") {
		t.Errorf("darwin: %+v", c)
	}
	// An httptest request's peer is not a connection at all.
	if c := (Resolver{GOOS: "linux"}).Resolve(context.Background(), "", "192.0.2.1:1234"); c.Agent != "" || c.Why == "" {
		t.Errorf("no local address: %+v", c)
	}
}

// The kernel's address spelling: 127.0.0.1:8080 is 0100007F:1F90 on a little-endian machine,
// and every address round-trips.
func TestProcAddressesParse(t *testing.T) {
	if binary.NativeEndian.Uint16([]byte{1, 0}) == 1 {
		got, err := parseProcAddr("0100007F:1F90")
		if err != nil || got != netip.MustParseAddrPort("127.0.0.1:8080") {
			t.Errorf("parseProcAddr = %v, %v", got, err)
		}
	}
	for _, s := range []string{"127.0.0.1:1461", "[::1]:443", "[::ffff:127.0.0.1]:9"} {
		ap := netip.MustParseAddrPort(s)
		got, err := parseProcAddr(FormatProcAddr(ap))
		want := netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
		if err != nil || got != want {
			t.Errorf("%s: round trip = %v, %v", s, got, err)
		}
	}
}

// The vocabulary's directories are the launcher's and the credential gate's.
func TestTheVocabularyDirectoriesAreTheLaunchersAndTheEnvFiles(t *testing.T) {
	if AgentEnvDirRel != entrypoint.AgentEnvDirRel {
		t.Errorf("AgentEnvDirRel = %q, entrypoint's is %q", AgentEnvDirRel, entrypoint.AgentEnvDirRel)
	}
	env := &entrypoint.Env{Home: "/h"}
	if got := filepath.Join("/h", LaunchDirRel); got != env.LaunchDir() {
		t.Errorf("launch dir = %q, entrypoint's is %q", got, env.LaunchDir())
	}
}

// AGAINST THE REAL /proc: this test's own connection is found, and attributed to the agent its
// binary is named as.
func TestTheRealProcNamesThisProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc is Linux's")
	}
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("no /proc/net/tcp here")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			defer c.Close()
			time.Sleep(2 * time.Second)
		}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	must(t, err)
	defer conn.Close()
	self := filepath.Base(os.Args[0])
	home := homeWithAgents(t, []string{self}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := Resolver{Home: home}.Resolve(ctx, ln.Addr().String(), conn.LocalAddr().String())
	if c.Agent != self || c.AgentPID != os.Getpid() {
		t.Errorf("caller = %+v, want %s pid %d", c, self, os.Getpid())
	}
}
