package broker

import (
	"net"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// frontSocketsHere points paths.FrontSocketDir at a short private dir for one test, where the
// jails' sockets this test stands up live.
func frontSocketsHere(t *testing.T) {
	t.Helper()
	saved := paths.FrontSocketDir
	paths.FrontSocketDir = shortSocketDir(t)
	t.Cleanup(func() { paths.FrontSocketDir = saved })
}

// listenAt binds a unix socket at path for the test's duration.
func listenAt(t *testing.T, path string) {
	t.Helper()
	assertSockPathFits(t, path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
}

// WHAT IS RUNNING IS WHAT THE JAILS RUN (HD-R1, §5.2): a jail's copy is found by the socket its
// launch keyed by its jail, named by that socket, and judged by whether it accepts. A socket
// left behind by a copy that died does not accept; a file that only looks like a socket name
// (a daemon's private `.host` sibling, a key that is not a jail's) is not a copy at all.
func TestRunningInstancesFindsEachJailsCopyByItsSocket(t *testing.T) {
	frontSocketsHere(t)
	listenAt(t, paths.FrontSocketFile("aaaaaaaa", fixtureDaemon))
	listenAt(t, paths.FrontSocketFile("bbbbbbbb", "another-daemon"))
	stale := paths.FrontSocketFile("cccccccc", fixtureDaemon)
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, decoy := range []string{paths.FrontSocketFile("aaaaaaaa", fixtureDaemon) + ".host",
		paths.FrontSocketDir + "/yolo-front-NOTAKEY-" + fixtureDaemon + ".sock"} {
		if err := os.WriteFile(decoy, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := RunningInstances(fixtureDaemon)
	if len(got) != 2 {
		t.Fatalf("RunningInstances(%s) = %+v, want the two copies keyed aaaaaaaa and cccccccc", fixtureDaemon, got)
	}
	if got[0].Jail != "aaaaaaaa" || !got[0].Accepting || got[0].HostWide {
		t.Errorf("the live copy reads as %+v, want jail aaaaaaaa, accepting", got[0])
	}
	if got[1].Jail != "cccccccc" || got[1].Accepting {
		t.Errorf("the dead copy's socket reads as %+v, want jail cccccccc, not accepting", got[1])
	}
	if all := RunningInstances(""); len(all) != 3 {
		t.Errorf("RunningInstances(\"\") = %+v, want every name's copies", all)
	}
}

// AN OLDER YOLO'S MACHINE-WIDE COPY is found by its lock and its PID file, and reported as host-wide;
// a lock with nothing running behind it (an older yolo's leftover, or the OpenAI migration's lock)
// is not a copy.
func TestRunningInstancesReportsAnOlderYolosHostWideCopy(t *testing.T) {
	frontSocketsHere(t)
	const name = "yjtest-legacy"
	for _, p := range []string{paths.HostSingletonLock(name), paths.HostSingletonLock("yjtest-lock-only")} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(p) })
	}
	pidFile := paths.HostSingletonPIDFile(name)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(pidFile) })
	listenAt(t, paths.HostSingletonSocket(name))

	got := RunningInstances(name)
	if len(got) != 1 || !got[0].HostWide || !got[0].Accepting || !slices.Equal(got[0].PIDs, []int{os.Getpid()}) {
		t.Fatalf("RunningInstances(%s) = %+v, want one host-wide copy, pid %d, accepting", name, got, os.Getpid())
	}
	if lockOnly := RunningInstances("yjtest-lock-only"); len(lockOnly) != 0 {
		t.Errorf("a lock with nothing behind it became a running copy: %+v", lockOnly)
	}
}

// A COPY STILL FINISHING AFTER ITS JAIL ENDED has no socket left, and stop must still find it: by
// the `--socket` argument it was started with, for that name and no other.
func TestStragglersAreFoundByTheirSocketArgument(t *testing.T) {
	requirePgrep(t)
	frontSocketsHere(t)
	mine, _ := startStray(t, "/opt/yolo/bin/yolo", "internal", "daemon", fixtureDaemon,
		"--socket", paths.FrontSocketFile("aaaaaaaa", fixtureDaemon))
	other, _ := startStray(t, "/opt/yolo/bin/yolo", "internal", "daemon", "another-daemon",
		"--socket", paths.FrontSocketFile("aaaaaaaa", "another-daemon"))
	var got []int
	for deadline := 0; deadline < 100 && !slices.Contains(got, mine); deadline++ {
		got = jailStragglers(fixtureDaemon)
	}
	if !slices.Contains(got, mine) {
		t.Errorf("jailStragglers(%s) = %v, want the straggler pid %d", fixtureDaemon, got, mine)
	}
	if slices.Contains(got, other) {
		t.Errorf("jailStragglers(%s) found another daemon's process %d", fixtureDaemon, other)
	}
}

// TestTheRendezvousScanIgnoresForeignPIDFiles is a regression pin with a measured origin: the
// scan globbed `/tmp/yolo-*.pid` and picked up `jaild` — the in-jail SUPERVISOR's pid file, whose
// namespace overlaps the machine-wide rendezvous exactly. `yolo host-daemon stop jaild` would have
// killed the jail's own supervisor. The lock file is what the scan globs instead.
func TestTheRendezvousScanIgnoresForeignPIDFiles(t *testing.T) {
	const foreign = "yjtest-fake-supervisor"
	pidFile := paths.HostSingletonPIDFile(foreign)
	if err := os.WriteFile(pidFile, []byte("1\n"), 0o600); err != nil {
		t.Skipf("cannot write the fixture at %s: %v", pidFile, err)
	}
	t.Cleanup(func() { _ = os.Remove(pidFile) })
	for _, name := range RendezvousSingletonNames() {
		if name == foreign {
			t.Fatalf("a bare PID file at %s became a manageable daemon — `stop` would "+
				"signal whatever process wrote it", pidFile)
		}
	}
}

// THE ALIAS'S NAME IS ALWAYS KNOWN, whatever discovery holds: `yolo broker` names the Claude
// broker in a jail or on a host that selects no claude pack too.
func TestTheBrokerIsAKnownHostDaemonWithoutDiscovery(t *testing.T) {
	frontSocketsHere(t)
	if !KnownHostDaemon(BrokerLoopholeName) {
		t.Error("the Claude broker is not a known host daemon where discovery is empty")
	}
	if KnownHostDaemon("yjtest-nothing-declares-this") {
		t.Error("a name nothing declares or runs reads as known")
	}
}
