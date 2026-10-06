package check

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// brokerLoopholeName is the Claude broker's loophole name. Where its copies run is DERIVED
// (broker.RunningInstances, over paths.FrontSocketFile and an older yolo's
// paths.HostSingleton*), never spelled here, so this section and the daemons that bind them
// cannot drift apart.
const brokerLoopholeName = "claude-oauth-broker"

// hostServiceDefaultJailEndpoint returns the in-jail path of a host service's
// published endpoint file — a REGULAR FILE, which is why the in-jail probe tests
// it with `test -f` and not `test -S`.
func hostServiceDefaultJailEndpoint(name string) string {
	return paths.JailHostServicesDir + "/" + name + paths.ServiceEndpointExt
}

// hostServiceSocketsDir returns the per-jail host-side endpoint-file dir. It
// delegates to paths: this was a hand-copied third implementation of the same
// hash-and-join, in the one package whose whole job is telling the user whether
// the other two agree.
func hostServiceSocketsDir(cname string, isMacOS bool) string {
	return paths.HostServicesDir(cname, isMacOS)
}

// brokerPingConn sends the length-prefixed {"action":"ping"} request over a
// connection the caller already opened and expects a data frame (stream 0) whose
// JSON has pong:true, before the exit frame (stream 2). Any error → false.
//
// The caller opens the connection because reaching the broker means reading a
// jail's endpoint file, pinning its certificate and presenting its token before a
// single protocol byte is written — and because the front, not this function, is
// what puts the connection preamble ahead of these bytes.
func brokerPingConn(conn net.Conn, timeout time.Duration) bool {
	_ = conn.SetDeadline(time.Now().Add(timeout))

	body := []byte(`{"action":"ping"}`)
	var lenPrefix [4]byte
	binary.BigEndian.PutUint32(lenPrefix[:], uint32(len(body)))
	if _, err := conn.Write(append(lenPrefix[:], body...)); err != nil {
		return false
	}

	for {
		hdr := make([]byte, 5)
		if _, err := readFull(conn, hdr); err != nil {
			return false
		}
		sid := hdr[0]
		ln := binary.BigEndian.Uint32(hdr[1:])
		payload := make([]byte, ln)
		if ln > 0 {
			if _, err := readFull(conn, payload); err != nil {
				return false
			}
		}
		switch sid {
		case 0: // STREAM_STDOUT
			decoded, err := jsonx.Decode(payload)
			if err != nil {
				return false
			}
			obj, ok := decoded.(*jsonx.OrderedMap)
			if !ok {
				return false
			}
			pong, _ := obj.Get("pong")
			b, _ := pong.(bool)
			return b
		case 2: // STREAM_EXIT without a pong first → not alive
			return false
		}
	}
}

// readFull reads len(buf) bytes or returns an error (io.ReadFull semantics,
// honoring the connection deadline).
func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// brokerEndpointVisibleInJail reports whether the RUNNING
// container sees the broker's endpoint file. Returns tri-state: visible=true,
// absent=false, unknown=nil (exec unavailable / exec-level failure). Represented
// as (*bool).
//
// `test -f`, not `test -S`: what crosses into the jail is a regular file naming
// the front's loopback listener, not a socket. The old -S probe would report every
// healthy jail as broken.
func (o *Options) brokerEndpointVisibleInJail(rt, cname string) *bool {
	if rt == "" || cname == "" {
		return nil
	}
	jailEndpoint := hostServiceDefaultJailEndpoint(brokerLoopholeName)
	// With the detach sequence off, as on every exec yolo issues into a jail
	// (runtime.DetachKeysArgs, JL-D27). This one reads no stdin, so nothing can type it; the
	// rule is every exec, so a new one copied from this line starts with it.
	argv := append([]string{rt, "exec"}, runtime.DetachKeysArgs(rt)...)
	argv = append(argv, cname, "sh", "-c", "test -f "+jailEndpoint)
	res := o.Exec(argv, "", nil, 10*time.Second)
	if !res.Ran || res.Timeout {
		return nil
	}
	switch res.RC {
	case 0:
		t := true
		return &t
	case 1:
		f := false
		return &f
	default:
		// 125/126/127…: exec-level failure, not a probe answer.
		return nil
	}
}
