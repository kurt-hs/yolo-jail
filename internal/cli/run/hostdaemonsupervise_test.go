package run

// hostdaemonsupervise_test.go pins the host-side supervisor of a jail's host daemons
// (hostdaemonsupervise.go; docs/design/host-daemon-ownership.md OQ-HD5, HD-D25, HD-D3) through its
// one call site, startExternalService: delete `go sup.run()` there and every restart, give-up and
// retired-exit test below fails.

import (
	"bufio"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/supervisor"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// fastHostDaemonRestarts runs HD-D25's policy on a millisecond scale for one test: the same shape
// (a first wait doubling to a ceiling, a bound on restarts in a row), so a crash loop takes
// milliseconds, not the production 31 seconds.
func fastHostDaemonRestarts(t *testing.T, initial, ceiling time.Duration, restarts int) {
	t.Helper()
	prev := hostDaemonRestarts
	hostDaemonRestarts = daemonRestartPolicy{initial: initial, ceiling: ceiling, restarts: restarts}
	t.Cleanup(func() { hostDaemonRestarts = prev })
}

// supervisedFixture is a test's view of one supervised daemon: its Options, the launch's terminal
// and launch.log halves, and the files its spawns record themselves in.
type supervisedFixture struct {
	t        *testing.T
	o        *Options
	terminal *syncBuffer
	launch   *syncBuffer
	pids     string
}

func newSupervisedFixture(t *testing.T) *supervisedFixture {
	t.Helper()
	if goruntime.GOOS != "linux" {
		t.Skip("spawns host processes and reads /proc")
	}
	t.Setenv("HOME", t.TempDir())
	f := &supervisedFixture{t: t, terminal: &syncBuffer{}, launch: &syncBuffer{},
		pids: filepath.Join(t.TempDir(), "pids")}
	f.o = &Options{}
	fillDefaults(f.o)
	// A LAUNCH'S WRITER, as attachLaunchLog installs it: the terminal, teed into launch.log.
	f.o.Stdout = teeLog{w: f.terminal, log: f.launch}
	return f
}

// spawnScript starts a socket-transport service whose argv is sh running script, with $1 the path
// the framework substituted and $PIDS the file each spawn appends its pid to.
func (f *supervisedFixture) spawnScript(name, script string, hd *loopholes.HostDaemon) loopholeDaemon {
	f.t.Helper()
	spec := jsonx.NewOrderedMap()
	spec.Set("command", []any{"sh", "-c", script, name, "{socket}"})
	env := jsonx.NewOrderedMap()
	env.Set("PIDS", f.pids)
	spec.Set("env", env)
	h, ok := f.o.startExternalService(name, spec, f.t.TempDir(), "", "", hd)
	if !ok {
		f.t.Fatalf("the service never came up:\n%s", f.terminal.String())
	}
	f.t.Cleanup(func() {
		h.stop()
		for _, pid := range f.spawned() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return h
}

// spawned is every pid a spawn of the service recorded, in order.
func (f *supervisedFixture) spawned() []int {
	raw, _ := os.ReadFile(f.pids)
	var pids []int
	for _, l := range strings.Fields(string(raw)) {
		if pid, err := strconv.Atoi(l); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// hostLog is the service's host log, logs/host-service-<name>.log.
func hostLog(name string) string {
	raw, _ := os.ReadFile(filepath.Join(paths.GlobalStorage(), "logs", "host-service-"+name+".log"))
	return string(raw)
}

// ended reports whether the handle's end closed within d.
func ended(h loopholeDaemon, d time.Duration) bool {
	select {
	case <-h.end.done:
		return true
	case <-time.After(d):
		return false
	}
}

// THE POLICY IS THE JAIL SIDE'S (HD-D25 (1)): one policy for both supervisors, from the same two
// constants, and the bound is the five restarts that backoff takes before its ceiling.
func TestTheHostDaemonRestartPolicyIsTheJailSidesBackoffBounded(t *testing.T) {
	want := daemonRestartPolicy{initial: time.Second, ceiling: 30 * time.Second, restarts: 5}
	if hostDaemonRestarts != want {
		t.Errorf("the host daemon restart policy is %+v, want %+v", hostDaemonRestarts, want)
	}
	if supervisor.RestartBackoffInitial != 1.0 || supervisor.RestartBackoffMax != 30.0 {
		t.Errorf("the jail side's backoff moved (%v, %v): HD-D25 takes it for both sides",
			supervisor.RestartBackoffInitial, supervisor.RestartBackoffMax)
	}
	var waits []time.Duration
	for d, i := want.initial, 0; i < want.restarts; i++ {
		waits = append(waits, d)
		d = min(d*2, want.ceiling)
	}
	if got := waits[len(waits)-1]; got != 16*time.Second {
		t.Errorf("the fifth restart waits %s, want 16s, the last step before the 30s ceiling", got)
	}
}

// A KILLED DAEMON IS RESTARTED, and the jail's front, published before the death, reaches the new
// one from its next request: the front dials the daemon's socket per connection. The death and the
// restart are lines in the daemon's host log and in launch.log, and not on the terminal.
func TestAKilledHostDaemonIsRestartedAndItsFrontServesAgain(t *testing.T) {
	f := newSupervisedFixture(t)
	fastHostDaemonRestarts(t, 10*time.Millisecond, 10*time.Second, 5)
	socketsDir := t.TempDir()
	if err := os.Chmod(socketsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(t.TempDir(), "release")
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	spec := jsonx.NewOrderedMap()
	spec.Set("command", []any{os.Args[0], "-front-upstream-child", "lingers", "{socket}"})
	env := jsonx.NewOrderedMap()
	env.Set("YJ_FRONT_CHILD_PID", pidFile)
	env.Set("YJ_FRONT_CHILD_RELEASE", release)
	spec.Set("env", env)
	h, ok := f.o.startExternalService("phoenix", spec, socketsDir, loopholes.TransportLoopbackTLS,
		"127.0.0.1", &loopholes.HostDaemon{Publishes: loopholes.PublishesSocket, RequestEnd: loopholes.RequestEndFramed})
	if !ok {
		t.Fatalf("the fronted daemon never came up:\n%s", f.terminal.String())
	}
	defer h.stop()
	readPid := func() int {
		raw, _ := os.ReadFile(pidFile)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
		return pid
	}
	first := readPid()
	if first == 0 {
		t.Fatal("the daemon never recorded its pid")
	}
	ping := func() string {
		conn, err := svcendpoint.DialLocal(h.hostPath, 5*time.Second)
		if err != nil {
			return "dial: " + err.Error()
		}
		defer conn.Close()
		boundConn(t, conn)
		if _, err := conn.Write([]byte("ping\n")); err != nil {
			return "write: " + err.Error()
		}
		reply, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return "read: " + err.Error()
		}
		return strings.TrimSpace(reply)
	}
	if got := ping(); got != "pong" {
		t.Fatalf("before the death the front answered %q, want pong", got)
	}

	if err := syscall.Kill(first, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if !waitFor(10*time.Second, func() bool { return strings.Contains(hostLog("phoenix"), "restarted (pid") }) {
		t.Fatalf("the killed daemon was never restarted; its host log:\n%s", hostLog("phoenix"))
	}
	if second := readPid(); second == first || !processLive(second) {
		t.Fatalf("after the restart the daemon's pid is %d (was %d): no new process serves", second, first)
	}
	if got := ping(); got != "pong" {
		t.Errorf("the front published before the death answered %q after the restart, want pong", got)
	}
	select {
	case <-h.end.done:
		t.Error("the handle's end closed for a death the supervisor restarted: a restarted daemon is not down")
	default:
	}

	for _, want := range []string{"host service 'phoenix' died mid-session (its process ended (signal: killed))",
		"restart 1 of 5", "host service 'phoenix' restarted (pid"} {
		if !strings.Contains(hostLog("phoenix"), want) {
			t.Errorf("the daemon's host log lacks %q:\n%s", want, hostLog("phoenix"))
		}
		if !strings.Contains(f.launch.String(), want) {
			t.Errorf("launch.log lacks %q:\n%s", want, f.launch.String())
		}
	}
	if strings.Contains(f.terminal.String(), "died mid-session") || strings.Contains(f.terminal.String(), "restarted (pid") {
		t.Errorf("a mid-session death reached the terminal, where the agent's interface runs:\n%s", f.terminal.String())
	}
}

// FIVE RESTARTS, THEN A DEATH THAT STANDS (HD-D25 (2), (3)): a daemon that dies straight after
// each start is restarted five times in a row and then given up on, the give-up naming the daemon
// and its log, and the handle's end closes so the keeper records it down.
func TestAHostDaemonThatKeepsDyingIsGivenUpOnAfterFiveRestarts(t *testing.T) {
	f := newSupervisedFixture(t)
	fastHostDaemonRestarts(t, 5*time.Millisecond, 10*time.Second, 5)
	h := f.spawnScript("crashloop", `echo $$ >> "$PIDS"; : > "$1"; sleep 0.05; exit 3`, nil)
	if !ended(h, 20*time.Second) {
		t.Fatalf("supervision never gave up on a daemon that dies at every start; its host log:\n%s", hostLog("crashloop"))
	}
	if n := len(f.spawned()); n != 6 {
		t.Errorf("the daemon was started %d times, want 6: the first spawn and five restarts", n)
	}
	log := hostLog("crashloop")
	if n := strings.Count(log, "died mid-session"); n != 5 {
		t.Errorf("the host log has %d restart lines, want 5:\n%s", n, log)
	}
	giveUp := "host service 'crashloop' died (its process ended (exit status 3)) after 5 restarts in a row; giving up"
	for _, out := range []struct{ what, text string }{{"host log", log}, {"launch.log", f.launch.String()}} {
		if !strings.Contains(out.text, giveUp) || !strings.Contains(out.text, "host-service-crashloop.log") {
			t.Errorf("the %s does not give up naming the daemon and its log:\n%s", out.what, out.text)
		}
	}
	if how := h.end.how(); !strings.Contains(how, "exit status 3") || !strings.Contains(how, "gave up after 5 restarts") {
		t.Errorf("the end's account is %q, want the death and the give-up", how)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(f.spawned()); n != 6 {
		t.Errorf("a daemon given up on was started again (%d spawns)", n)
	}
}

// UP PAST THE CEILING RESETS THE COUNT (HD-D25 (2)): a daemon that dies only after living longer
// than the backoff's ceiling is not a crash loop, and is restarted past the bound. The control is
// the test above: the same death inside the ceiling gives up.
func TestAHostDaemonUpPastTheCeilingResetsTheRestartCount(t *testing.T) {
	f := newSupervisedFixture(t)
	fastHostDaemonRestarts(t, 5*time.Millisecond, 100*time.Millisecond, 2)
	h := f.spawnScript("longlived", `echo $$ >> "$PIDS"; : > "$1"; sleep 0.3; exit 3`, nil)
	if !waitFor(15*time.Second, func() bool { return len(f.spawned()) >= 5 }) {
		t.Fatalf("the daemon was started %d times; its host log:\n%s", len(f.spawned()), hostLog("longlived"))
	}
	select {
	case <-h.end.done:
		t.Fatalf("supervision gave up on a daemon that lived past the ceiling each time:\n%s", hostLog("longlived"))
	default:
	}
	if strings.Contains(hostLog("longlived"), "giving up") {
		t.Errorf("the host log gave up past the bound of 2:\n%s", hostLog("longlived"))
	}
	if strings.Contains(hostLog("longlived"), "restart 2 of 2") {
		t.Errorf("the count did not reset after a life past the ceiling:\n%s", hostLog("longlived"))
	}
}

// A TEARDOWN'S STOP IS NEVER A DEATH (HD-D25 (5)), by both teardowns: the process-group kill, and
// detach, the SIGTERM a credential daemon finishes its work after.
func TestATeardownStopIsNeverRestarted(t *testing.T) {
	for _, tc := range []struct {
		name string
		hd   *loopholes.HostDaemon
	}{
		{"kill", nil},
		{"detach", &loopholes.HostDaemon{Detach: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSupervisedFixture(t)
			fastHostDaemonRestarts(t, 5*time.Millisecond, 10*time.Second, 5)
			f.o.ServiceTermGrace = 2 * time.Second
			name := "torn-" + tc.name
			h := f.spawnScript(name, `echo $$ >> "$PIDS"; : > "$1"; exec sleep 30`, tc.hd)
			pid := f.spawned()[0]
			h.stop()
			if !waitFor(5*time.Second, func() bool { return !processLive(pid) }) {
				t.Fatalf("the daemon (pid %d) outlived its stop", pid)
			}
			if !ended(h, 5*time.Second) {
				t.Error("supervision did not end with the teardown's stop")
			}
			time.Sleep(200 * time.Millisecond)
			if n := len(f.spawned()); n != 1 {
				t.Errorf("a daemon its teardown stopped was started again (%d spawns)", n)
			}
			if log := hostLog(name); strings.Contains(log, "died") || strings.Contains(log, "restart") {
				t.Errorf("the teardown's stop was reported as a death:\n%s", log)
			}
		})
	}
}

// A TEARDOWN DURING A RESTART LEAKS NOTHING: the stop marks the supervisor first, so a restart in
// its readiness wait is the process the teardown stops, and nothing is spawned after it.
func TestATeardownDuringARestartStopsTheRestartedProcess(t *testing.T) {
	f := newSupervisedFixture(t)
	fastHostDaemonRestarts(t, 5*time.Millisecond, 10*time.Second, 5)
	f.o.ServiceTermGrace = time.Second
	// The first spawn serves and dies; every later one never becomes ready, so the restart sits in
	// its readiness wait.
	h := f.spawnScript("midrestart", `echo $$ >> "$PIDS"; if [ "$(wc -l < "$PIDS")" -eq 1 ]; then : > "$1"; sleep 0.1; exit 3; fi; exec sleep 30`, nil)
	if !waitFor(10*time.Second, func() bool { return len(f.spawned()) == 2 }) {
		t.Fatalf("the restart never spawned; host log:\n%s", hostLog("midrestart"))
	}
	restarted := f.spawned()[1]
	h.stop()
	if !waitFor(5*time.Second, func() bool { return !processLive(restarted) }) {
		t.Fatalf("the restart in flight (pid %d) outlived the teardown: it leaked", restarted)
	}
	if !ended(h, 5*time.Second) {
		t.Error("supervision did not end with the teardown's stop")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(f.spawned()); n != 2 {
		t.Errorf("a process was spawned after the teardown (%d spawns)", n)
	}
}

// A RETIRED STATE DIRECTORY IS FINAL (HD-D3): a daemon that exits with
// hostservice.ExitStateDirRetired is reported once and never restarted, since a restart would
// recreate the directory a launch archived; the handle's end closes, so the keeper records it down.
func TestARetiredStateDirExitIsNotRestarted(t *testing.T) {
	f := newSupervisedFixture(t)
	fastHostDaemonRestarts(t, 5*time.Millisecond, 10*time.Second, 5)
	h := f.spawnScript("retiree", `echo $$ >> "$PIDS"; : > "$1"; sleep 0.1; exit `+
		shquote.Quote(strconv.Itoa(hostservice.ExitStateDirRetired)), nil)
	if !ended(h, 10*time.Second) {
		t.Fatalf("supervision did not end for a retired state directory; host log:\n%s", hostLog("retiree"))
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(f.spawned()); n != 1 {
		t.Errorf("a daemon whose state directory was retired was started again (%d spawns)", n)
	}
	want := "host service 'retiree' stopped: its state directory was retired; not restarted"
	for _, out := range []struct{ what, text string }{{"host log", hostLog("retiree")}, {"launch.log", f.launch.String()}} {
		if strings.Count(out.text, want) != 1 {
			t.Errorf("the %s does not say the retired exit once:\n%s", out.what, out.text)
		}
		if strings.Contains(out.text, "died") {
			t.Errorf("the %s reports the deliberate exit as a death:\n%s", out.what, out.text)
		}
	}
	if how := h.end.how(); !strings.Contains(how, "state directory was retired") {
		t.Errorf("the end's account is %q, want the retirement", how)
	}
}

// launchLogOnly is the launch.log half of each writer a launch hands its services, and nothing of
// the terminal's.
func TestLaunchLogOnlyTakesTheLogHalfAlone(t *testing.T) {
	var term, outer, inner syncBuffer
	if w := launchLogOnly(&term); w != nil {
		t.Errorf("a bare terminal has a log half: %T", w)
	}
	w := launchLogOnly(teeLog{w: teeLog{w: &term, log: &inner}, log: &outer})
	if w == nil {
		t.Fatal("a stack of tees has no log half")
	}
	_, _ = w.Write([]byte("line\n"))
	if term.String() != "" || outer.String() != "line\n" || inner.String() != "line\n" {
		t.Errorf("terminal %q, outer log %q, inner log %q", term.String(), outer.String(), inner.String())
	}
	k := keeperStream{&keeperSink{}, frameStdout}
	if got, ok := launchLogOnly(k).(keeperStream); !ok || got != k {
		t.Errorf("a keeper's stream was not kept as its own routing: %T", launchLogOnly(k))
	}
}
