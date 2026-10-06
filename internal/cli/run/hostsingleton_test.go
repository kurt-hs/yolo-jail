package run

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// hostsingleton_test.go keeps the fixtures of a MACHINE-WIDE daemon, which no launch of this
// build starts any more (HD-R1, docs/design/host-daemon-ownership.md) and an older yolo may
// still have running. The tests that pinned yolo ENSURING one (startHostSingleton) went with it;
// what stays is the fixture the per-jail tests use to show a launch leaves such a daemon alone.

// singletonFixture stands up the state a host-wide daemon an OLDER yolo started leaves on the
// machine: a bound socket at the framework-derived path, and a pid file naming a live process
// (this one). broker.BrokerIsAlive reads exactly those four facts.
//
// The loophole NAME is per-test and unique, because these paths are fixed by derivation and a
// test must not collide with a real /tmp/yolo-<name>.* of the machine it runs on.
func singletonFixture(t *testing.T, name string) net.Listener {
	t.Helper()
	sock := paths.HostSingletonSocket(name)
	assertSockPathFits(t, sock)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("binding the fixture singleton socket %s: %v", sock, err)
	}
	t.Cleanup(func() { _ = ln.Close(); _ = os.Remove(sock) })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				// THE PREAMBLE FIRST, exactly as a real fronted daemon does.
				var hdr [4]byte
				if _, err := io.ReadFull(br, hdr[:]); err != nil {
					return
				}
				body := make([]byte, binary.BigEndian.Uint32(hdr[:]))
				if _, err := io.ReadFull(br, body); err != nil {
					return
				}
				if !strings.Contains(string(body), `"v":1`) {
					return
				}
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(line) == "ping" {
					_, _ = c.Write([]byte("pong\n"))
				}
			}(conn)
		}
	}()
	pidFile := paths.HostSingletonPIDFile(name)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(pidFile)
		_ = os.Remove(paths.HostSingletonLock(name))
	})
	return ln
}

// dialFrontLine sends one newline-terminated request through a published endpoint
// and returns the response line. The endpoint is the jail-facing half: pinned
// certificate, bearer token, then bytes.
func dialFrontLine(t *testing.T, endpointPath, req string) string {
	t.Helper()
	conn, err := svcendpoint.DialLocal(endpointPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", endpointPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		t.Fatalf("writing to %s: %v", endpointPath, err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("reading from %s: %v", endpointPath, err)
	}
	return strings.TrimSpace(line)
}
