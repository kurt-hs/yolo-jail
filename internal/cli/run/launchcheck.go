package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mschulkind-oss/yolo-jail/internal/broker"
	"github.com/mschulkind-oss/yolo-jail/internal/frameproto"
	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// launchcheck.go is the LAUNCH half of the launch check: internal/hostservice/launchcheck.go
// coins the term and states the protocol, and a loophole opts in with
// `host_daemon.launch_check` (internal/loopholedecl). After the host services start, the launch
// asks each daemon that declares it what this launch should warn about, and prints the answer.
//
// KEYED ON THE DECLARATION, never on a loophole's name, as the un-narrowed disclosure is
// (loopholesettings.go, docs/design/sso-backed-bedrock.md OQ-SSO10): the launch path renders
// every loophole with no switch on a tool name (AGENTS.md). Its first declarer is aws-auth,
// whose daemon answers with the mint failure that would fail the agent's first Bedrock request
// (design §8, "the launch warns with the `aws sso login` command and proceeds"; SSO-D1).
//
// # Who is asked
//
// A daemon whose record declares the check, that this launch started or ensured (for an attach:
// whose front the running jail's launch published, runAttachLaunchChecks), and whose jail
// daemon this launch SERVES (loopholes.ServedJailDaemons: the container runs it, or on
// macos-user it runs in the guest or opens as a doorway outside it), or that declares no jail
// daemon at all. aws-auth's jail daemon is served only when some agent's provider is on
// Bedrock (providers.md OQ-CN7 (b)), so a launch with no such agent asks
// nothing: a warning about a service none of its agents reaches has nobody to warn.
//
// # What a daemon answers never refuses, and it is never silent about what it could not do
//
// Every warning prints and the launch proceeds (§8: a jail that will not start is worse than a
// first request that fails clearly). A daemon that could not be asked, or did not answer within
// the budget, gets one dim line saying so: the check is a disclosure, so its absence must not
// read as a clean bill. No flag hides either (docs/reference/report-tiers.md, OQ-RO3).
//
// # An older yolo's machine-wide daemon, still running, REFUSES a fresh launch
//
// Since HD-R1 (docs/design/host-daemon-ownership.md) every launch starts its own copy of each host
// daemon, so the daemon a fresh launch asks is always this yolo's build: one that answers
// `unknown action: launch-check` has a manifest declaring a check its program lacks, and gets the
// line saying so (unknownLaunchCheckLine), naming no restart. OQ-HD11's refusal (HD-D5), which
// named the restart of a host-wide daemon too old to answer, has no such daemon left to meet.
//
// THE HOST-WIDE DAEMON THAT CAN STILL EXIST is the one an older yolo started and left running
// through the upgrade, serving the jails that yolo launched. A launch never fronts it, but it works
// on the same credential and takes the same lock as this launch's copy without the record this
// yolo's copies share (HD-D16: the Claude broker's replaced and refused tokens), so it can fail
// this jail's requests. So a FRESH launch whose loophole still has such a copy alive REFUSES, by
// OQ-HD11's own reason, the maintainer's "don't want to launch without a feature that is
// promised", naming broker.StopHostWideCommand and what that stop costs the older yolo's jails
// (HD-D21). It is keyed on the fact, a live copy of this home's at the name-keyed rendezvous
// (broker.LegacyDeps), and never on the loophole's name.
//
//   - AN ATTACH NEVER REFUSES (HD-D5 (1)): it starts nothing, so refusing it would leave the
//     running jail exactly as it is. An attach into a jail an older yolo launched dials that
//     jail's front, which leads to the older copy; when that copy does not answer the check, the
//     yellow line names the same stop, then relaunching the jail with this yolo
//     (olderHostWideAttachLine).
//   - NO LAUNCH STOPS THE OLDER COPY ITSELF: it serves jails an older yolo launched, and only the
//     user knows whether those may lose it now. No hatch reaches the refusal (HD-D5 (4)): the
//     daemon is yolo's own state, and the fix is one command.
//
// # What it costs
//
// One loopback TLS dial through the front the launch just published, and one framed request,
// for each daemon asked, all in parallel. A daemon answering from what it knows (aws-auth with
// a warm cache) replies in milliseconds. One that has to find out is bounded by
// hostservice.LaunchCheckBudget, and the launch stops reading launchCheckMargin after that.

// launchCheckMargin is how long past the budget the launch keeps reading: the dial, the TLS
// handshake and the framing on a loaded host, never the daemon's own work.
const launchCheckMargin = time.Second

// launchCheckTextMax caps one printed line from a daemon, in runes.
const launchCheckTextMax = 600

// launchCheckEntry is who asks the launch check: a fresh launch, which refuses beside an older
// yolo's machine-wide copy, or an attach, which says so and goes on (HD-D5 (1)).
type launchCheckEntry int

const (
	freshLaunchCheck launchCheckEntry = iota
	attachLaunchCheck
)

// runLaunchChecks asks every started daemon that is due one (see the file comment) and prints
// the answers in the order the daemons started. For a fresh launch it returns the refusal when a
// loophole it started a copy of still has a machine-wide copy an older yolo left running
// (olderHostWideRefusal), and nil otherwise; the caller prints it and stops, since what it must
// unwind (a keeper's services, a macos-user session) is the caller's. A refused daemon is not
// asked: the refusal stands alone. An attach is never refused: it prints the line instead.
func (o *Options) runLaunchChecks(rt string, started []loopholeDaemon, payload []loopholes.JailDaemonSpec,
	entry launchCheckEntry) *olderDaemonRefusal {
	var refused *olderDaemonRefusal
	if entry == freshLaunchCheck {
		refused = olderHostWideRefusal(started)
	}
	served := map[string]bool{}
	for _, s := range loopholes.ServedJailDaemons(rt, payload) {
		served[s.Name] = true
	}
	var due []loopholeDaemon
	for _, h := range started {
		if !h.launchCheck || h.hostPath == "" || refused.names(h.name) {
			continue
		}
		if h.hasJailDaemon && !served[h.name] {
			continue
		}
		due = append(due, h)
	}
	type answer struct {
		report hostservice.LaunchCheckReport
		err    error
	}
	answers := make([]answer, len(due))
	if len(due) > 0 {
		budget := hostservice.LaunchCheckBudget
		o.withStderrProgress("Checking host services", func() bool {
			var wg sync.WaitGroup
			for i, h := range due {
				wg.Add(1)
				go func(i int, h loopholeDaemon) {
					defer wg.Done()
					r, err := askLaunchCheck(h.hostPath, budget)
					answers[i] = answer{r, err}
				}(i, h)
			}
			wg.Wait()
			return true
		})
	}
	out := o.pr(o.Stderr)
	for i, h := range due {
		a := answers[i]
		var unknown *launchCheckUnknownError
		if errors.As(a.err, &unknown) {
			// ONLY AN ATTACH'S FRONT CAN LEAD TO AN OLDER YOLO'S COPY (runAttachLaunchChecks sets
			// hostWide); a fresh launch's daemon is its own build, whose manifest is what is wrong.
			if h.hostWide {
				out.print("[bold yellow]loophole " + h.name + ": " +
					richtext.Escape(launchCheckText(olderHostWideAttachLine(h.name))) + "[/bold yellow]")
				continue
			}
			out.print("[bold yellow]loophole " + h.name + ": " +
				richtext.Escape(launchCheckText(unknownLaunchCheckLine(unknown))) + "[/bold yellow]")
			continue
		}
		if a.err != nil {
			out.print("[dim]loophole " + h.name + ": could not ask the host service what this " +
				"launch should know: " + richtext.Escape(launchCheckText(a.err.Error())) + "[/dim]")
			continue
		}
		for _, w := range a.report.Warnings {
			out.print("[bold yellow]loophole " + h.name + ": " +
				richtext.Escape(launchCheckText(w)) + "[/bold yellow]")
		}
		for _, n := range a.report.Notes {
			out.print("[dim]loophole " + h.name + ": " + richtext.Escape(launchCheckText(n)) + "[/dim]")
		}
	}
	return refused
}

// olderHostWideAlive reports whether the loophole called name still has a machine-wide copy an
// older yolo left running for THIS home: a live PID file and a socket that accepts at the
// name-keyed rendezvous (broker.LegacyDeps), which no yolo since HD-R1 writes, whose process runs
// under this launch's home (broker.LegacyCopyServesHome). A copy under another home shares the
// rendezvous paths, which are keyed by the name in /tmp, but none of this launch's credentials.
func olderHostWideAlive(name string) bool {
	deps := broker.LegacyDeps(name)
	return broker.BrokerIsAlive(deps) && broker.LegacyCopyServesHome(deps, paths.Home())
}

// olderDaemonRefusal is the refusal of a fresh launch beside an older yolo's machine-wide copies
// (see the file comment): each by loophole name, in the order this launch started its own.
type olderDaemonRefusal struct {
	older []string
}

// names reports whether the refusal names the loophole called name. Nil-safe: no refusal names
// nothing.
func (r *olderDaemonRefusal) names(name string) bool {
	return r != nil && slices.Contains(r.older, name)
}

// olderHostWideRefusal is the refusal for the daemons in started whose loophole still has a
// machine-wide copy an older yolo left running, in start order, or nil when there is none. The one
// refusal every fresh launch that starts host services owes: the keeper and the macos-user arm
// through runLaunchChecks, and `yolo host`'s HostDoorways.Start, which asks no launch check.
func olderHostWideRefusal(started []loopholeDaemon) *olderDaemonRefusal {
	var older []string
	for _, h := range started {
		if !slices.Contains(older, h.name) && olderHostWideAlive(h.name) {
			older = append(older, h.name)
		}
	}
	if len(older) == 0 {
		return nil
	}
	return &olderDaemonRefusal{older: older}
}

// text is the refusal after its headline: the copies, why they stop this launch, the one command
// line that stops them alone, and what that costs the jails the older yolo launched.
func (r *olderDaemonRefusal) text() string {
	quoted := make([]string, len(r.older))
	cmds := make([]string, len(r.older))
	for i, name := range r.older {
		quoted[i] = "'" + name + "'"
		cmds[i] = broker.StopHostWideCommand(name)
	}
	what, it := "a machine-wide daemon for "+quoted[0]+" that an older yolo started is still running", "it"
	if len(r.older) > 1 {
		what = "machine-wide daemons for " + strings.Join(quoted[:len(quoted)-1], ", ") + " and " +
			quoted[len(quoted)-1] + " that an older yolo started are still running"
		it = "them"
	}
	return what + ". This yolo gives each jail its own, and an older copy running beside them works " +
		"on the same credential without the record they share, so it can fail this jail's requests." +
		"\n  Stop " + it + " with: " + strings.Join(cmds, " && ") +
		"\n  Jails the older yolo launched lose " + it + " until you launch them again with this yolo; " +
		"every jail's own copy keeps running. Then launch again."
}

// markup is the refusal as the launch stream prints it; headline names who refuses ("Refusing this
// launch", "Refusing the macos-user launch").
func (r *olderDaemonRefusal) markup(headline string) string {
	return "[bold red]" + richtext.Escape(headline+": "+r.text()) + "[/bold red]"
}

// olderHostWideAttachLine is an attach's line when the jail it enters fronts a machine-wide copy an
// older yolo left running and that copy does not answer the launch check (HD-D5 (1), HD-D21): the
// attach goes on, and the line names the stop and the relaunch that end it.
func olderHostWideAttachLine(name string) string {
	return "this jail was launched by an older yolo, whose machine-wide daemon does not answer the " +
		"launch check, so this entry cannot warn about what would fail its agents' requests. Stop " +
		"that daemon with: " + broker.StopHostWideCommand(name) + ", then leave this jail and " +
		"launch it again with this yolo, which gives it its own"
}

// launchCheckUnknownError is a daemon's answer that it does not know the launch-check action
// (hostservice.IsUnknownLaunchCheck).
type launchCheckUnknownError struct {
	rc     int
	detail string
}

func (e *launchCheckUnknownError) Error() string {
	return fmt.Sprintf("it exited %d: %s", e.rc, e.detail)
}

// unknownLaunchCheckLine is the warning for a daemon of this yolo's that does not know the action:
// this launch started it from its manifest, which declares a check its program does not answer,
// and no restart changes the program, so there is no command to name.
func unknownLaunchCheckLine(e *launchCheckUnknownError) string {
	return "its daemon does not answer the launch check its manifest declares (it said: " +
		e.detail + "), so this launch cannot warn about what would fail its agents' requests"
}

// launchCheckText makes a daemon's string safe to print as one terminal line: control
// characters become spaces, and it is capped at launchCheckTextMax runes. A daemon is host code
// the launch already runs, but its words still must not move the cursor or forge a second line.
func launchCheckText(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if n == launchCheckTextMax {
			b.WriteString("…")
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// askLaunchCheck dials the endpoint file as a host-side client (svcendpoint.DialLocal, the
// probe shape that authenticates with the 0600 file this process published) and sends one
// launch-check request. The whole exchange is bounded by budget plus launchCheckMargin.
func askLaunchCheck(endpointPath string, budget time.Duration) (hostservice.LaunchCheckReport, error) {
	deadline := time.Now().Add(budget + launchCheckMargin)
	conn, err := svcendpoint.DialLocal(endpointPath, launchCheckMargin)
	if err != nil {
		return hostservice.LaunchCheckReport{}, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	return exchangeLaunchCheck(conn, budget)
}

// exchangeLaunchCheck is the framed conversation on an open connection.
func exchangeLaunchCheck(conn net.Conn, budget time.Duration) (hostservice.LaunchCheckReport, error) {
	body, err := json.Marshal(map[string]any{
		"action":                         hostservice.LaunchCheckAction,
		hostservice.LaunchCheckBudgetKey: budget.Milliseconds(),
	})
	if err != nil {
		return hostservice.LaunchCheckReport{}, err
	}
	if err := frameproto.WriteRequest(conn, body); err != nil {
		return hostservice.LaunchCheckReport{}, fmt.Errorf("send: %w", err)
	}
	var stdout, stderr bytes.Buffer
	for {
		frame, err := frameproto.ReadFrame(conn)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return hostservice.LaunchCheckReport{}, fmt.Errorf("no answer within %s", budget+launchCheckMargin)
			}
			return hostservice.LaunchCheckReport{}, fmt.Errorf("read: %w", err)
		}
		switch frame.StreamID {
		case frameproto.StreamStdout:
			stdout.Write(frame.Payload)
		case frameproto.StreamStderr:
			stderr.Write(frame.Payload)
		case frameproto.StreamExit:
			rc, err := frameproto.ExitCode(frame.Payload)
			if err != nil {
				return hostservice.LaunchCheckReport{}, err
			}
			if rc != 0 {
				detail := firstNonEmptyLine(stderr.String(), "no diagnostic")
				// A daemon started by a yolo that predates the launch check answers this way.
				if hostservice.IsUnknownLaunchCheck(detail) {
					return hostservice.LaunchCheckReport{}, &launchCheckUnknownError{rc, detail}
				}
				return hostservice.LaunchCheckReport{}, fmt.Errorf("it exited %d: %s", rc, detail)
			}
			var report hostservice.LaunchCheckReport
			if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
				return hostservice.LaunchCheckReport{}, fmt.Errorf("malformed answer: %w", err)
			}
			return report, nil
		}
	}
}

// firstNonEmptyLine is s's first non-blank line, or fallback.
func firstNonEmptyLine(s, fallback string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return fallback
}

// runAttachLaunchChecks is the launch check for an ATTACH: a new entry into a jail that is
// already running, whose host services its own launch started and still fronts. Attaching is
// how a user re-enters a long-lived jail (`yolo -p bedrock -- claude` in a second terminal), and
// an SSO session that was live at the launch may have lapsed since, so the entry is asked about
// the same way a launch is. The daemons asked are the enabled loopholes declaring the check
// whose front the running jail's launch published in its services dir, and the served test is
// this entry's payload (what its selection runs), exactly as runLaunchChecks applies it.
//
// IT ASKS AND NEVER STARTS. An attach runs above the config-change approval gate and never
// starts, ensures or restarts a service (noteSingletonSettingsDrift's rule), and a front belongs
// to the jail's keeper. So a loophole with no published endpoint is not asked: its launch did not
// start it, or the backend does not run it (Apple Container starts no aws-auth service). An
// endpoint file left by a keeper that has since died is dialled and fails, which prints the dim
// "could not ask" line, and that is true: the jail cannot reach the service either.
//
// IT NEVER REFUSES (HD-D5 (1)). A jail an older yolo launched fronts that yolo's machine-wide copy,
// and while such a copy is alive the entry's handle says so (hostWide), so a copy that does not
// answer the check gets the line naming its stop and the relaunch (olderHostWideAttachLine) where a
// fresh launch refuses. Refusing the entry would leave the running jail exactly as it is.
func (o *Options) runAttachLaunchChecks(cname, rt string, cfg *jsonx.OrderedMap, payload []loopholes.JailDaemonSpec) {
	socketsDir := hostServiceSocketsDir(cname, o.IsMacOS)
	var running []loopholeDaemon
	for _, lp := range loopholes.NewHostSet(cfgMap(cfg, "loopholes")).Enabled() {
		if lp.HostDaemon == nil || !lp.HostDaemon.LaunchCheck ||
			lp.Transport != loopholes.TransportLoopbackTLS {
			continue
		}
		hostPath := filepath.Join(socketsDir, lp.Name+paths.ServiceEndpointExt)
		if !fileExists(hostPath) {
			continue
		}
		running = append(running, markLaunchCheck(loopholeDaemon{name: lp.Name, hostPath: hostPath,
			hostWide: olderHostWideAlive(lp.Name)}, lp))
	}
	o.runLaunchChecks(rt, running, payload, attachLaunchCheck)
}
