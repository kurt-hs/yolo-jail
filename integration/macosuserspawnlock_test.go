package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// OQ-HD10'S MEASUREMENT, NOW HD-R1'S CHECK ON A MAC: TWO macos-user LAUNCHES OF ONE WORKSPACE,
// AT ONCE (docs/design/host-daemon-ownership.md#OQ-HD10, HD-D4, HD-R1).
//
// THE QUESTION IT ANSWERED. Retiring `host_daemon.scope: "host"` (HD-R1) removes the spawn
// flock, paths.HostSingletonLock, along with the singleton it guarded. On macos-user there is
// no container, and per-jail identity is per-WORKSPACE (the cname hashes the resolved workspace
// path), so two simultaneous launches of one workspace are one identity. Three scheduled runs
// (2026-09-28 to 2026-09-30) saw ONE BROKER per pair, credited to that flock, so HD-D4 gave
// per-session daemons a per-workspace spawn guard of their own. HD-R1 is built on macos-user
// now, and what this records is what the build promises there.
//
// WHAT IT RECORDS, one `HD10 …` line each, so one grep of the job log recovers the record:
//
//   - SPAWN: how many claude-oauth-broker processes of the PAIR existed while both launches
//     ran — sampled from the host by pgrep every 100 ms, counting only a SESSION'S broker (its
//     argv names a /tmp/yolo-front-… socket: hd10SessionBrokerPIDs) that was not alive before
//     the pair started. ASSERTED: two at once, one per session. A machine-wide broker an older
//     yolo left, or a developer's real one, is neither counted nor stopped: no session of this
//     build adopts it or touches it.
//   - SPAWN-GUARD: whether either launch printed the guard's waiting notice (HD-D4,
//     takeHostDaemonSpawnGuard): a launch that did waited while the other's host services came
//     up. Either answer passes; it depends on how close together the two starts landed.
//   - WORKSPACE-LOCK: whether either launch printed the per-workspace courtesy lock's waiting
//     notice. That lock is taken AFTER the host-daemon start, before content staging
//     (refreshJailBriefings), so a wait there is a wait on the other's staging and bootstrap.
//   - ENDPOINT DURING: what each session's claude-oauth-broker endpoint variable named, and
//     whether the file was readable and its host:port dialable while both sessions were up.
//   - ENDPOINT AFTER: the same probe in the LONGER session after the shorter one's `yolo`
//     process had exited. ASSERTED. The second run (scheduled macos-user run 36319436117,
//     f937d0fd) measured it GONE: both sessions published into ONE per-workspace host-services
//     dir, and the shorter session's teardown removed it under the survivor. Each session
//     publishes into a dir of its own and tears down only that one (servicessession.go), and
//     since HD-R1 its own broker behind its own front, so the survivor's endpoint must answer.
//   - STRAGGLERS: whether the pair's brokers had exited once both launches returned. ASSERTED,
//     within hd10StragglerBound: a jail's end SIGTERMs its broker and does not wait (the
//     manifest's `detach`, §1.3), and a broker with no refresh in flight exits at once; one in
//     flight is bounded by its thirty-second upstream deadline.
//
// # What is red
//
// Neither launch running its probe, or the two sessions never being up at the same time (then
// nothing about concurrency was observed — the single-launch tests are the control). And the
// three claims HD-R1 makes here: a broker per session, the survivor's endpoint still answering,
// and no broker of the pair alive past the bound.
//
// ONE LAUNCH PAIR, and the pair's sessions coordinate through marker files in the workspace
// (which both the sandbox account and this process can write), so the overlap is arranged
// rather than hoped for: each session waits for the other's `up` marker, the shorter one waits
// until the longer has probed, and the longer waits for this process to say the shorter's
// `yolo` has exited before probing again.
func TestMacosUserTwoConcurrentLaunchesOfOneWorkspace(t *testing.T) {
	requireMacosUser(t)
	// The claude pack is the subject: its claude-oauth-broker is the daemon that used to be
	// one per host, which each session now starts for itself.
	packHome(t, `{"packs": ["claude"]}`)
	ws := macosUserWorkspace(t, `{}`)
	syncDir := filepath.Join(ws, "hd10-sync")
	if err := os.Mkdir(syncDir, 0o777); err != nil {
		t.Fatal(err)
	}
	// Mkdir's mode is filtered by the umask; both sessions run as the sandbox account and
	// write here, so the mode is set outright.
	if err := os.Chmod(syncDir, 0o777); err != nil {
		t.Fatal(err)
	}

	// Session brokers already alive (another workspace's session, on a shared runner) are not
	// the pair's: only one that appears during the pair is counted.
	pre := hd10SessionBrokerPIDs()

	envVar := macosUserServiceEnvVar(hd10Broker)
	scriptA := hd10Script(syncDir, envVar, "A", "B", true)
	scriptB := hd10Script(syncDir, envVar, "B", "A", false)

	// The sampler runs from before the first launch until both have returned.
	samples := &hd10Sampler{seen: map[int]bool{}}
	stopSampling := make(chan struct{})
	var samplerDone sync.WaitGroup
	samplerDone.Add(1)
	go func() {
		defer samplerDone.Done()
		for {
			samples.observe(hd10NewPIDs(hd10SessionBrokerPIDs(), pre))
			select {
			case <-stopSampling:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()

	env := hd10LaunchEnv()
	t0 := time.Now()
	a := hd10Launch(t, ws, scriptA, env)
	b := hd10Launch(t, ws, scriptB, env)
	// Each `<X>-exited` marker is written by THIS process once that launch's whole `yolo` —
	// its deferred teardown included — has returned, never by a session. The longer session
	// probes again on B-exited, and every wait in either script also stops on the OTHER
	// session's marker, so a launch that dies early costs its partner nothing but a record.
	var rA, rB hd10Result
	var bExited time.Duration
	for a != nil || b != nil {
		select {
		case rA = <-a:
			a = nil
			_ = os.WriteFile(filepath.Join(syncDir, "A-exited"), []byte("exited\n"), 0o644)
		case rB = <-b:
			b = nil
			bExited = time.Since(t0)
			_ = os.WriteFile(filepath.Join(syncDir, "B-exited"), []byte("exited\n"), 0o644)
		}
	}
	close(stopSampling)
	samplerDone.Wait()
	// THE STRAGGLER BOUND: every broker the pair ran was SIGTERMed by its session's end, and is
	// left to finish whatever refresh it had in flight (§1.3). Polled, never killed here.
	var left []int
	for deadline := time.Now().Add(hd10StragglerBound); ; time.Sleep(200 * time.Millisecond) {
		left = hd10StillAlive(samples.distinct(), hd10SessionBrokerPIDs())
		if len(left) == 0 || time.Now().After(deadline) {
			break
		}
	}

	ranA := strings.Contains(rA.stdout, "=== END ===")
	ranB := strings.Contains(rB.stdout, "=== END ===")
	if !ranA && !ranB {
		t.Fatalf("HD10: NEITHER launch ran its probe (A rc=%d, B rc=%d), so nothing about "+
			"concurrent launches was observed — this is not an answer to OQ-HD10. The "+
			"single-launch macos-user tests are the control: if they are red too, the backend "+
			"is broken, not concurrent.\nA stdout:\n%s\nA stderr:\n%s\nB stdout:\n%s\nB stderr:\n%s",
			rA.rc, rB.rc, lastLines(rA.stdout, 40), lastLines(rA.stderr, 40),
			lastLines(rB.stdout, 40), lastLines(rB.stderr, 40))
	}
	fa := hd10Fields(section(rA.stdout, "=== A ===", "=== END ==="))
	fb := hd10Fields(section(rB.stdout, "=== B ===", "=== END ==="))
	if fa["A_OVERLAP"] != "yes" && fb["B_OVERLAP"] != "yes" {
		t.Fatalf("HD10: the two sessions were never up at the same time (A saw B: %q, B saw A: %q), "+
			"so there was no concurrency to observe. A launch that refused or failed before its "+
			"session is recorded below; read it — a refusal of the second launch IS an answer, "+
			"and this test should then be taught to record it rather than fail.\n"+
			"A rc=%d:\n%s\nB rc=%d:\n%s", fa["A_OVERLAP"], fb["B_OVERLAP"],
			rA.rc, lastLines(rA.combined(), 40), rB.rc, lastLines(rB.combined(), 40))
	}

	waitedOn := func(r hd10Result, notice string) bool { return strings.Contains(r.combined(), notice) }
	const guardNotice = "Waiting for another launch of this workspace to start its host services"
	const lockNotice = "Waiting for concurrent jail launch in workspace"
	spawn := hd10SpawnVerdict(samples.distinct(), samples.max)

	t.Logf("HD10 LAUNCHES: A rc=%d ran=%v, B rc=%d ran=%v (B's yolo exited %s after the pair started)",
		rA.rc, ranA, rB.rc, ranB, bExited.Round(time.Second))
	t.Logf("HD10 SPAWN: %s. Session brokers alive before the pair (not counted): %v; the pair's "+
		"seen: %v (at most %d at once); machine-wide rendezvous %s present: %v",
		spawn, pre, samples.distinct(), samples.max, paths.HostSingletonSocket(hd10Broker),
		hd10Exists(paths.HostSingletonSocket(hd10Broker)))
	t.Logf("HD10 SPAWN-GUARD: A printed the guard's waiting notice: %v; B printed it: %v "+
		"(a wait means one launch's host services started after the other's were up)",
		waitedOn(rA, guardNotice), waitedOn(rB, guardNotice))
	t.Logf("HD10 WORKSPACE-LOCK: A printed the waiting notice: %v; B printed it: %v "+
		"(taken after the host-daemon start, before content staging: a wait means one launch "+
		"waited for the other's staging and bootstrap, not for its spawn)",
		waitedOn(rA, lockNotice), waitedOn(rB, lockNotice))
	t.Logf("HD10 ENDPOINT DURING: A %s | B %s | one file for both: %v", hd10Probe(fa, "A_DURING"),
		hd10Probe(fb, "B_DURING"), fa["A_DURING_VAR"] != "" && fa["A_DURING_VAR"] == fb["B_DURING_VAR"])
	t.Logf("HD10 ENDPOINT AFTER B EXITED: A saw the exit marker: %s; A %s",
		fa["A_SAW_B_EXIT"], hd10Probe(fa, "A_AFTER"))
	t.Logf("HD10 STRAGGLERS: %s", hd10StragglerVerdict(left))
	t.Logf("HD10 WARNINGS: A:\n%s\nB:\n%s", hd10Warnings(rA.combined()), hd10Warnings(rB.combined()))
	t.Logf("HD10 VERDICT: %s; the survivor's endpoint after the other session ended: %s; %s. "+
		"Record them in docs/design/host-daemon-ownership.md#OQ-HD10.",
		spawn, hd10AfterVerdict(fa), hd10StragglerVerdict(left))
	if failure := hd10SpawnFailure(spawn); failure != "" {
		t.Errorf("%s\nA rc=%d:\n%s\nB rc=%d:\n%s", failure,
			rA.rc, lastLines(rA.combined(), 40), rB.rc, lastLines(rB.combined(), 40))
	}
	if failure := hd10SurvivorFailure(fa); failure != "" {
		t.Errorf("%s\nA rc=%d:\n%s\nB rc=%d:\n%s", failure,
			rA.rc, lastLines(rA.combined(), 40), rB.rc, lastLines(rB.combined(), 40))
	}
	if len(left) > 0 {
		t.Errorf("HD10: %s", hd10StragglerVerdict(left))
	}
}

// hd10StragglerBound is how long the pair's brokers may run past both launches' return: the
// upstream request deadline a refresh in flight is bounded by (oauthbroker's httpClient, thirty
// seconds), with margin. A broker with nothing in flight exits within milliseconds of its SIGTERM.
const hd10StragglerBound = 45 * time.Second

// hd10SpawnFailure is "" when the pair ran a broker per session, two at once, and otherwise why
// that is a failure. PURE, and pinned with the verdicts.
func hd10SpawnFailure(verdict string) string {
	if strings.HasPrefix(verdict, "ONE PER SESSION") {
		return ""
	}
	return "HD10: the pair's brokers were " + verdict + ". Under HD-R1 each macos-user session " +
		"starts its own " + hd10Broker + " (internal/cli/run/hostdaemonperjail.go), so two " +
		"overlapping sessions run two at once"
}

// hd10StragglerVerdict names whether the pair's brokers were gone by the bound.
func hd10StragglerVerdict(left []int) string {
	if len(left) == 0 {
		return "NONE LEFT — every broker of the pair exited once its session ended"
	}
	return fmt.Sprintf("STILL RUNNING after %s — the pair's brokers %v outlived their sessions "+
		"past the upstream deadline a refresh in flight is bounded by; a jail's end SIGTERMs its "+
		"broker (the manifest's `detach`), and one that does not exit is the stuck lock holder "+
		"docs/design/host-daemon-ownership.md §1.3 names", hd10StragglerBound, left)
}

// hd10SurvivorFailure is the experiment's one assertion about its answer: "" when the longer
// session's endpoint still answered after the shorter session's `yolo` had exited, teardown and
// all, and otherwise why that is a failure. PURE, and pinned with the verdicts.
//
// NOT OBSERVED fails too. It means the longer session never probed after the other's exit, so a
// run that ends there has not shown the survivor keeps a working endpoint, which is the claim.
func hd10SurvivorFailure(fa map[string]string) string {
	verdict := hd10AfterVerdict(fa)
	if strings.HasPrefix(verdict, "STILL WORKS") {
		return ""
	}
	return "HD10: after the shorter session exited, the longer session's " + hd10Broker +
		" endpoint was " + verdict + ". One macos-user session's exit must never remove or " +
		"invalidate an endpoint another live session of the same workspace uses: each session " +
		"publishes into a host-services dir of its own and removes only that one " +
		"(internal/cli/run/servicessession.go; docs/design/host-daemon-ownership.md#OQ-HD10)"
}

// hd10Broker is the host-scoped loophole whose spawn this measures.
const hd10Broker = "claude-oauth-broker"

// hd10WaitSeconds bounds each wait in a session's script. The per-launch deadline
// (macosUserTimeout) is the outer bound; this one only has to outlast the partner's
// provisioning, which on a cold runner includes whatever of the floor the first launch left.
const hd10WaitSeconds = 900

// hd10Script is one session's probe. self and other name the two sessions; the longer
// session (A) probes a second time after the shorter one has exited.
//
// Only the endpoint's host:port field is printed — the file's last field is a bearer token.
func hd10Script(dir, envVar, self, other string, longer bool) string {
	// Up to hd10WaitSeconds, and never past the other launch's exit: the other session's
	// provisioning may queue behind this one's on the workspace lock, so the bound is
	// generous, and the exit marker is what keeps a dead partner from costing all of it.
	wait := func(marker string) string {
		return fmt.Sprintf(`w=0; while [ ! -e %[1]s/%[2]s ] && [ ! -e %[1]s/%[3]s-exited ] && `+
			`[ $w -lt %[4]d ]; do sleep 1; w=$((w+1)); done`, dir, marker, other, hd10WaitSeconds)
	}
	lines := []string{
		`probe() {`,
		`  ep="${` + envVar + `-}"`,
		`  echo "$1_VAR=${ep:-UNSET}"`,
		`  if [ -z "$ep" ]; then return; fi`,
		`  if [ ! -e "$ep" ]; then echo "$1_FILE=ABSENT"; return; fi`,
		`  if ! head -c 1 "$ep" >/dev/null 2>&1; then echo "$1_FILE=UNREADABLE"; return; fi`,
		`  echo "$1_FILE=READABLE"`,
		`  hp="$(cut -d' ' -f1 "$ep")"; echo "$1_HOSTPORT=$hp"`,
		`  if [ -n "$hp" ] && (exec 3<>"/dev/tcp/${hp%:*}/${hp##*:}") 2>/dev/null; then echo "$1_DIAL=OK"; else echo "$1_DIAL=FAILED"; fi`,
		`}`,
		fmt.Sprintf(`touch %s/%s-up`, dir, self),
		wait(other + "-up"),
		`echo "=== ` + self + ` ==="`,
		fmt.Sprintf(`[ -e %s/%s-up ] && echo "%s_OVERLAP=yes" || echo "%s_OVERLAP=no"`, dir, other, self, self),
		`probe ` + self + `_DURING`,
		fmt.Sprintf(`touch %s/%s-probed`, dir, self),
	}
	if longer {
		lines = append(lines,
			wait(other+"-exited"),
			fmt.Sprintf(`[ -e %s/%s-exited ] && echo "%s_SAW_B_EXIT=yes" || echo "%s_SAW_B_EXIT=no"`, dir, other, self, self),
			`probe `+self+`_AFTER`,
		)
	} else {
		// The shorter session outlives the longer one's first probe, so both "during" probes
		// really are taken while the other session is up.
		lines = append(lines, wait(other+"-probed"))
	}
	return strings.Join(append(lines, `echo "=== END ==="`), "\n")
}

// hd10Result is one launch's outcome. runCommand cannot be used from a second goroutine (it
// calls t.Fatalf), so the pair is started here with the same environment it builds.
type hd10Result struct {
	rc             int
	stdout, stderr string
	startErr       error
}

func (r hd10Result) combined() string { return r.stdout + r.stderr }

// hd10LaunchEnv is runCommand's launcher environment plus macosUserRunEnv's runtime
// selection, computed once on the test goroutine.
func hd10LaunchEnv() []string {
	env := append(os.Environ(), "TERM=dumb")
	env = append(env, childRepoRootEnv()...)
	env = append(env, autoCaptureEnvForSuite()...)
	return append(env, "YOLO_RUNTIME=macos-user")
}

// hd10Launch starts one macos-user launch of script in dir and delivers its result. A launch
// that overruns macosUserTimeout is killed and reported as rc -1: a wedged pair is a finding,
// and it must not leave the other launch unobserved.
//
// The detached-writer cleanup is registered HERE, on the test goroutine, and runs when the
// test ends — after BOTH launches of the pair, which share one HOME: stopping that HOME's
// broker when the first launch returned would kill it under the second
// (detachedwriters_test.go).
func hd10Launch(t *testing.T, dir, script string, env []string) <-chan hd10Result {
	t.Helper()
	ch := make(chan hd10Result, 1)
	args := append(jailRunArgs(), "--", "bash", "-lc", script)
	awaitDetachedWriters(t, dir, launchHome(env))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), macosUserTimeout())
		defer cancel()
		cmd := exec.CommandContext(ctx, yoloBin, args...)
		cmd.Dir = dir
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		r := hd10Result{stdout: stdout.String(), stderr: stderr.String()}
		var ee *exec.ExitError
		switch {
		case err == nil:
		case ctx.Err() != nil:
			r.rc = -1
			r.stderr += fmt.Sprintf("\n[HD10: killed after %s]", macosUserTimeout())
		case errors.As(err, &ee):
			r.rc = ee.ExitCode()
		default:
			r.rc, r.startErr = -1, err
			r.stderr += "\n[HD10: did not start: " + err.Error() + "]"
		}
		ch <- r
	}()
	return ch
}

// hd10SessionBrokerPattern is the pgrep pattern of a SESSION'S claude-oauth-broker: its argv
// names the upstream socket its launch keyed by the session (internal/cli/run's frontSocketFile,
// /tmp/yolo-front-<key>-<name>.sock). A machine-wide broker an older yolo left answers on
// /tmp/yolo-<name>.sock and never matches, so a developer's real one is neither counted nor
// touched.
const hd10SessionBrokerPattern = "internal daemon " + hd10Broker + " --socket /tmp/yolo-front-"

// hd10SessionBrokerPIDs lists the live session brokers on this host. pgrep exits 1 on no
// match, which is the empty answer rather than a fault.
func hd10SessionBrokerPIDs() []int {
	out, _ := exec.Command("pgrep", "-f", hd10SessionBrokerPattern).Output()
	return hd10ParsePIDs(string(out))
}

// hd10NewPIDs is pids less the ones in pre, which were alive before the pair started. PURE.
func hd10NewPIDs(pids, pre []int) []int {
	old := map[int]bool{}
	for _, p := range pre {
		old[p] = true
	}
	var out []int
	for _, p := range pids {
		if !old[p] {
			out = append(out, p)
		}
	}
	return out
}

// hd10Exists reports whether anything is at path, for the record line.
func hd10Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// hd10StillAlive is the pair's brokers (seen) that are among the live ones (live). PURE.
func hd10StillAlive(seen, live []int) []int {
	alive := map[int]bool{}
	for _, p := range live {
		alive[p] = true
	}
	var out []int
	for _, p := range seen {
		if alive[p] {
			out = append(out, p)
		}
	}
	return out
}

// hd10ParsePIDs reads pgrep's one-pid-per-line output, sorted.
func hd10ParsePIDs(out string) []int {
	var pids []int
	for _, f := range strings.Fields(out) {
		if n, err := strconv.Atoi(f); err == nil {
			pids = append(pids, n)
		}
	}
	sort.Ints(pids)
	return pids
}

// hd10Sampler accumulates what the pgrep samples saw.
type hd10Sampler struct {
	mu   sync.Mutex
	seen map[int]bool
	max  int
}

func (s *hd10Sampler) observe(pids []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range pids {
		s.seen[p] = true
	}
	if len(pids) > s.max {
		s.max = len(pids)
	}
}

func (s *hd10Sampler) distinct() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int
	for p := range s.seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// hd10SpawnVerdict names what the samples say about the pair's brokers. PURE, and pinned under
// -short (TestMacosUserHD10VerdictsNameEachCase). Under HD-R1 the answer to expect is ONE PER
// SESSION: each session starts its own, and they share the credentials file and its refresh
// flock rather than a process (docs/design/host-daemon-ownership.md §1.1).
func hd10SpawnVerdict(distinct []int, maxAtOnce int) string {
	switch {
	case len(distinct) == 0:
		return "NO BROKER — neither session left a claude-oauth-broker of its own running long " +
			"enough to sample, so its spawn failed or was skipped; read the WARNINGS line"
	case len(distinct) == 1:
		return "ONE BROKER — exactly one session broker was ever seen, so one session ran none " +
			"of its own: its spawn failed, or it fronted another's"
	case maxAtOnce >= 2:
		return fmt.Sprintf("ONE PER SESSION — %d session brokers were alive together while both "+
			"sessions were up, each its own session's", maxAtOnce)
	default:
		return fmt.Sprintf("%d BROKERS IN SEQUENCE — never two at once, so the sessions' brokers "+
			"did not overlap although the sessions did: one died and another started", len(distinct))
	}
}

// hd10AfterVerdict names what the longer session saw once the shorter had exited. PURE, and
// pinned with hd10SpawnVerdict.
func hd10AfterVerdict(fa map[string]string) string {
	switch {
	case fa["A_SAW_B_EXIT"] != "yes":
		return "NOT OBSERVED (the longer session never saw the shorter one exit)"
	case fa["A_AFTER_FILE"] == "ABSENT":
		return "GONE — the shorter session's teardown removed the endpoint file the longer " +
			"session is using (what the second run measured, when both sessions shared one " +
			"per-workspace host-services dir)"
	case fa["A_AFTER_DIAL"] == "OK":
		return "STILL WORKS — the file is there and its host:port answers"
	case fa["A_AFTER_FILE"] == "READABLE":
		return "STALE — the file is there and its host:port no longer answers"
	case fa["A_AFTER_VAR"] == "UNSET" || fa["A_AFTER_VAR"] == "":
		return "NO VARIABLE — the longer session was never told the endpoint"
	default:
		return "UNREADABLE — the file is there and the sandbox account cannot read it"
	}
}

// hd10Fields parses the probe's KEY=VALUE lines.
func hd10Fields(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k != "" && !strings.ContainsAny(k, " \t") {
			out[k] = v
		}
	}
	return out
}

// hd10Probe renders one probe's fields on one line.
func hd10Probe(f map[string]string, prefix string) string {
	return fmt.Sprintf("var=%s file=%s hostport=%s dial=%s",
		orNone(f[prefix+"_VAR"]), orNone(f[prefix+"_FILE"]), orNone(f[prefix+"_HOSTPORT"]), orNone(f[prefix+"_DIAL"]))
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// hd10Warnings keeps the launch lines that could carry a collision: warnings, refusals and
// the lock notices.
func hd10Warnings(out string) string {
	var keep []string
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "warning") || strings.Contains(l, "refus") ||
			strings.Contains(l, "waiting for concurrent") || strings.Contains(l, "waiting for another launch") ||
			strings.Contains(l, "could not") || strings.Contains(l, hd10Broker) {
			keep = append(keep, "  "+strings.TrimSpace(line))
		}
	}
	if len(keep) == 0 {
		return "  (none)"
	}
	return strings.Join(keep, "\n")
}

// TestMacosUserHD10VerdictsNameEachCase pins the experiment's classifiers from Linux, under
// -short. They decide what the recorded line SAYS, so a case that reads as another would put a
// wrong answer into OQ-HD10's evidence a night later. Named TestMacosUser… so the job that runs
// the experiment also checks them; not behind requireMacosUser.
func TestMacosUserHD10VerdictsNameEachCase(t *testing.T) {
	for _, tc := range []struct {
		name     string
		distinct []int
		max      int
		want     string
	}{
		{"nothing ever ran", nil, 0, "NO BROKER"},
		{"one daemon", []int{7}, 1, "ONE BROKER"},
		{"one per session", []int{7, 9}, 2, "ONE PER SESSION"},
		{"a replacement", []int{7, 9}, 1, "2 BROKERS IN SEQUENCE"},
	} {
		got := hd10SpawnVerdict(tc.distinct, tc.max)
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: hd10SpawnVerdict = %q, want it to start %q", tc.name, got, tc.want)
		}
		// The spawn assertion passes on a broker per session alone.
		failure := hd10SpawnFailure(got)
		switch {
		case tc.want == "ONE PER SESSION" && failure != "":
			t.Errorf("%s: hd10SpawnFailure = %q, want \"\" for a broker per session", tc.name, failure)
		case tc.want != "ONE PER SESSION" && !strings.Contains(failure, tc.want):
			t.Errorf("%s: hd10SpawnFailure = %q, want a failure naming %q", tc.name, failure, tc.want)
		}
	}
	if got := hd10StragglerVerdict(nil); !strings.HasPrefix(got, "NONE LEFT") {
		t.Errorf("hd10StragglerVerdict(none) = %q, want NONE LEFT", got)
	}
	if got := hd10StragglerVerdict([]int{7}); !strings.HasPrefix(got, "STILL RUNNING") || !strings.Contains(got, "[7]") {
		t.Errorf("hd10StragglerVerdict([7]) = %q, want STILL RUNNING naming the pid", got)
	}
	if got := hd10NewPIDs([]int{3, 5, 7}, []int{5}); len(got) != 2 || got[0] != 3 || got[1] != 7 {
		t.Errorf("hd10NewPIDs = %v, want [3 7]: a broker alive before the pair is not the pair's", got)
	}
	if got := hd10StillAlive([]int{3, 7}, []int{7, 11}); len(got) != 1 || got[0] != 7 {
		t.Errorf("hd10StillAlive = %v, want [7]", got)
	}
	// A session's broker names its session-keyed socket; an older yolo's machine-wide one does not.
	if !strings.Contains("yolo internal daemon claude-oauth-broker --socket /tmp/yolo-front-0b472111-claude-oauth-broker.sock",
		hd10SessionBrokerPattern) ||
		strings.Contains("yolo internal daemon claude-oauth-broker --socket /tmp/yolo-claude-oauth-broker.sock",
			hd10SessionBrokerPattern) {
		t.Errorf("hd10SessionBrokerPattern %q does not tell a session's broker from a machine-wide one",
			hd10SessionBrokerPattern)
	}

	for _, tc := range []struct {
		name   string
		fields string
		want   string
	}{
		{"never saw the exit", "A_SAW_B_EXIT=no", "NOT OBSERVED"},
		{"the file is gone", "A_SAW_B_EXIT=yes\nA_AFTER_VAR=/tmp/x\nA_AFTER_FILE=ABSENT", "GONE"},
		{"still answers", "A_SAW_B_EXIT=yes\nA_AFTER_FILE=READABLE\nA_AFTER_DIAL=OK", "STILL WORKS"},
		{"stale", "A_SAW_B_EXIT=yes\nA_AFTER_FILE=READABLE\nA_AFTER_DIAL=FAILED", "STALE"},
		{"no variable", "A_SAW_B_EXIT=yes\nA_AFTER_VAR=UNSET", "NO VARIABLE"},
		{"unreadable", "A_SAW_B_EXIT=yes\nA_AFTER_VAR=/tmp/x\nA_AFTER_FILE=UNREADABLE", "UNREADABLE"},
	} {
		if got := hd10AfterVerdict(hd10Fields(tc.fields)); !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: hd10AfterVerdict = %q, want it to start %q", tc.name, got, tc.want)
		}
		// The survivor assertion passes on a working endpoint alone, and every other verdict,
		// NOT OBSERVED included, is a failure that names it.
		failure := hd10SurvivorFailure(hd10Fields(tc.fields))
		switch {
		case tc.want == "STILL WORKS" && failure != "":
			t.Errorf("%s: hd10SurvivorFailure = %q, want \"\" for a survivor whose endpoint answers", tc.name, failure)
		case tc.want != "STILL WORKS" && !strings.Contains(failure, tc.want):
			t.Errorf("%s: hd10SurvivorFailure = %q, want a failure naming %q", tc.name, failure, tc.want)
		}
	}

	if got := hd10ParsePIDs("123\n45\n\n"); len(got) != 2 || got[0] != 45 || got[1] != 123 {
		t.Errorf("hd10ParsePIDs = %v, want [45 123]", got)
	}
	// The sessions' probe lines must be what the parser reads: the script's own markers.
	script := hd10Script("/s", "YOLO_SERVICE_X_ENDPOINT", "A", "B", true)
	for _, want := range []string{"=== A ===", "A_OVERLAP", "probe A_DURING", "B-exited", "probe A_AFTER", "=== END ==="} {
		if !strings.Contains(script, want) {
			t.Errorf("the longer session's script lacks %q:\n%s", want, script)
		}
	}
	if s := hd10Script("/s", "V", "B", "A", false); strings.Contains(s, "_AFTER") || !strings.Contains(s, "A-probed") {
		t.Errorf("the shorter session's script must wait for the other's probe and never probe after:\n%s", s)
	}
	// Every wait stops on the partner's exit marker, or a launch that dies early holds the
	// other for the whole bound.
	for _, s := range []string{script, hd10Script("/s", "V", "B", "A", false)} {
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "w=0;") && !strings.Contains(line, "-exited ]") {
				t.Errorf("a wait does not stop on the partner's exit marker: %s", line)
			}
		}
	}
}

// TestMacosUserHD10GuardsTheMachineWideBroker is the CALL-SITE half of the experiment's rules,
// read from the source for warmupgate_test.go's reason: the experiment runs only on a macos-user
// host, so no -short test can drive it, and the rules' own cases pass whether or not the
// experiment consults them. It fails if the experiment counts any broker but a session's (a
// developer's machine-wide one would then read as the pair's), if it regains a `host-daemon
// stop` (under HD-R1 the one daemon that verb stops by name is a machine-wide one an older
// yolo's jails are using), or if it stops asserting what HD-R1 promises there.
func TestMacosUserHD10GuardsTheMachineWideBroker(t *testing.T) {
	src, err := os.ReadFile("macosuserspawnlock_test.go")
	if err != nil {
		t.Fatalf("reading macosuserspawnlock_test.go: %v", err)
	}
	body := string(src)
	i := strings.Index(body, "\nfunc TestMacosUserTwoConcurrentLaunchesOfOneWorkspace(")
	if i < 0 {
		t.Fatal("TestMacosUserTwoConcurrentLaunchesOfOneWorkspace is gone; this test has lost its subject")
	}
	j := strings.Index(body[i+1:], "\n}\n")
	if j < 0 {
		t.Fatal("could not find the end of TestMacosUserTwoConcurrentLaunchesOfOneWorkspace")
	}
	fn := body[i : i+1+j]
	if !strings.Contains(fn, "samples.observe(hd10NewPIDs(hd10SessionBrokerPIDs(), pre))") {
		t.Error("the experiment no longer samples only the session brokers that appeared during " +
			"the pair; a machine-wide broker an older yolo left would read as the pair's")
	}
	if strings.Contains(fn, `"host-daemon", "stop"`) || strings.Contains(fn, `"host-daemon", "restart"`) {
		t.Error("the experiment runs `yolo host-daemon` against a daemon by name; under HD-R1 the " +
			"pair's brokers end with their sessions, and the daemon that verb names is a " +
			"machine-wide one an older yolo's jails may be using")
	}
	for _, call := range []string{"if failure := hd10SpawnFailure(spawn); failure != \"\" {\n\t\tt.Errorf(",
		"if len(left) > 0 {\n\t\tt.Errorf("} {
		if !strings.Contains(fn, call) {
			t.Errorf("the experiment no longer asserts %q: HD-R1's claim on a Mac would go back to "+
				"being recorded instead of checked", call)
		}
	}
	// The survivor ASSERTION, not just its verdict line: hd10SurvivorFailure's cases pass whether
	// or not the experiment consults it, and the teardown fix it checks can only be seen on a Mac.
	if !strings.Contains(fn, "if failure := hd10SurvivorFailure(fa); failure != \"\" {\n\t\tt.Errorf(") {
		t.Error("the experiment no longer fails when the surviving session's endpoint stops " +
			"answering after the other session exits (hd10SurvivorFailure); it would go back to " +
			"recording the macos-user teardown defect instead of catching it")
	}
}
