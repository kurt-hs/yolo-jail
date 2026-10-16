package hostservice

// stopmarker.go is how `yolo host-daemon stop` tells a jail's supervisor that the death it is
// about to see was asked for (docs/design/host-daemon-ownership.md HD-D28): the verb is one yolo
// process, the jail's supervisor another, and a SIGTERM alone reads to the supervisor as a crash,
// which it restarts a second later.
//
// THE MARKER is a file beside the copy's upstream socket, <socket>.stopped, written by the verb
// BEFORE it signals and naming, one per line, the pids it is about to signal. The supervisor,
// after a death, takes it (TakeStopMarker) only if it lists the pid that died: then it removes it
// and does not restart. Keyed by the socket because the socket is the one name both processes
// already share for a copy (the verb finds copies by it; the supervisor respawns at it); guarded
// by the pid so a marker that outlived its purpose cannot stand for a later copy at the same
// socket.
//
// ITS LIFETIME: consumed by the supervisor; removed by every spawn at the socket (the first
// spawn's ClearStopMarker, a restart's removal of the socket's siblings), so a marker the verb
// left for a copy no supervisor watched (one whose jail had already ended) lasts only until the
// next launch of that jail.
//
// The socket's directory is /tmp on the host, which other users share: the marker is created
// O_EXCL, so a planted file or link is never written through, and is honored only when it is a
// regular file this user owns.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// StopMarkerPath is the marker beside socket.
func StopMarkerPath(socket string) string { return socket + ".stopped" }

// WriteStopMarker records that the pids serving socket are about to be stopped on purpose. Any
// marker already there is replaced.
func WriteStopMarker(socket string, pids []int) error {
	path := StopMarkerPath(socket)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, p := range pids {
		fmt.Fprintf(&b, "%d\n", p)
	}
	_, werr := f.WriteString(b.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
	}
	return werr
}

// TakeStopMarker reports whether the death of pid, which served socket, was a stop the verb
// asked for, and removes the marker when it was. A marker that does not list pid is left for
// the next spawn at the socket to remove.
func TakeStopMarker(socket string, pid int) bool {
	path := StopMarkerPath(socket)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, l := range strings.Fields(string(raw)) {
		if n, err := strconv.Atoi(l); err == nil && n == pid {
			_ = os.Remove(path)
			return true
		}
	}
	return false
}

// ClearStopMarker removes a marker an earlier copy at socket left.
func ClearStopMarker(socket string) { _ = os.Remove(StopMarkerPath(socket)) }
