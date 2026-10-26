package hostrun

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/hostpath"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

// ExitRefused is every refusal's exit status.
const ExitRefused = 64

// DefaultSpoolCap is how much of each stream is kept for delivery (HX-D9).
const DefaultSpoolCap = 16 << 20

// AuditService is the audit log's `service` for host-run lines (HX-D45).
const AuditService = "host-run"

var openSub = paths.OpenStateSubdirRoot

// Auditor appends one audit line; *brokeraudit.Log is one.
type Auditor interface{ Append(brokeraudit.Event) }

// HostEnv is everything the host form takes from its process, so a test can supply it.
type HostEnv struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Environ is the child's whole environment, and where PATH and the `env:` line are read.
	Environ   func() []string
	Now       func() time.Time
	Rand      io.Reader
	Audit     Auditor
	Version   string
	SpoolCap  int64
	Heartbeat time.Duration
	// StateDirs are yolo's own state directories, refused as PATH entries; nil is the real ones.
	StateDirs []string
	// ManagedDirs are yolo's host-managed directories inside StateDirs, which no jail mounts:
	// allowed as PATH entries and skipped when the program is resolved (HX-D32, HE-D5); nil is
	// the real ones (hostpath.ManagedDirs).
	ManagedDirs []string
}

func (e HostEnv) withDefaults() HostEnv {
	if e.Stdin == nil {
		e.Stdin = os.Stdin
	}
	if e.Stdout == nil {
		e.Stdout = os.Stdout
	}
	if e.Stderr == nil {
		e.Stderr = os.Stderr
	}
	if e.Environ == nil {
		e.Environ = os.Environ
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.Rand == nil {
		e.Rand = rand.Reader
	}
	if e.Audit == nil {
		e.Audit = brokeraudit.Open(paths.BrokerAuditLog(), func(m string) { fmt.Fprintln(e.Stderr, "yolo host-run: warning:", m) })
	}
	if e.SpoolCap <= 0 {
		e.SpoolCap = DefaultSpoolCap
	}
	if e.Heartbeat <= 0 {
		e.Heartbeat = 10 * time.Second
	}
	if e.StateDirs == nil {
		e.StateDirs = defaultStateDirs()
	}
	if e.ManagedDirs == nil {
		e.ManagedDirs = hostpath.ManagedDirs()
	}
	return e
}

// hostRun is one host-form invocation from step 7 on (design §4.3).
type hostRun struct {
	env       HostEnv
	line      Line
	startDir  string
	workspace string
	environ   []string
	mb        *os.Root
	mbPath    string
}

// RunHost runs the host form from step 7 on, for a line already parsed and rune-checked, a
// resolved start directory and the workspace found from it. It returns the process's exit
// status: the child's, 128 + its signal, or 64 for a refusal.
func RunHost(env HostEnv, l Line, startDir, workspace string) int {
	h := &hostRun{env: env.withDefaults(), line: l, startDir: startDir, workspace: workspace}
	h.environ = h.env.Environ()
	return h.run()
}

func (h *hostRun) say(format string, a ...any) {
	fmt.Fprintf(h.env.Stderr, "yolo host-run: "+format+"\n", a...)
}

// refuse prints a refusal for the human only and returns 64.
func (h *hostRun) refuse(format string, a ...any) int {
	h.say(format, a...)
	return ExitRefused
}

func vis(s string) string { return termsafe.Visible(s) }

func (h *hostRun) jailName() string {
	return paths.HostServicesDirName(paths.JailShortHash(runtime.FromWorkspace(h.workspace)))
}

func (h *hostRun) run() int {
	ws := vis(h.workspace)
	// Step 7: the mailbox.
	yoloDir := paths.WorkspaceStateDir(h.workspace)
	yr, err := paths.OpenStateDirRoot(yoloDir)
	if err != nil {
		var linked *paths.LinkedStateDirError
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return h.refuse("no jail has run in %s, so there is no mailbox to deliver to, and nothing ran: launch `yolo` there first, or run the command without `yolo host-run`.", ws)
		case errors.As(err, &linked):
			return h.refuse("%s is a link; yolo will not write through it, and nothing ran. Remove the link (yolo recreates the directory at the next launch).", vis(linked.Path))
		default:
			return h.refuse("cannot open %s: %v; nothing ran. Run the command without `yolo host-run` and paste the output to the agent.", vis(yoloDir), err)
		}
	}
	defer yr.Close()
	h.mbPath = filepath.Join(yoloDir, MailboxName)
	if err := yr.Mkdir(MailboxName, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return h.refuse("cannot write to %s: %v; nothing ran. Run the command without `yolo host-run` and paste the output to the agent.", vis(h.mbPath), err)
	}
	mb, err := openSub(yr, MailboxName, h.mbPath)
	if err != nil {
		var linked *paths.LinkedStateDirError
		if errors.As(err, &linked) {
			return h.refuse("%s is a link; yolo will not write through it, and nothing ran. Remove the link and run the line again.", vis(linked.Path))
		}
		return h.refuse("cannot write to %s: %v; nothing ran. Run the command without `yolo host-run` and paste the output to the agent.", vis(h.mbPath), err)
	}
	defer mb.Close()
	h.mb = mb

	// Step 8: gh's credential-printing forms.
	if PrintsGHCredential(h.line.Argv) {
		return h.deliverRefusal(8,
			"this prints your GitHub credential; host-run will not deliver it",
			"Do not ask for the GitHub credential: the jail's `gh` runs through the github-broker without it.",
			"Run it without `yolo host-run` if you need it yourself.")
	}

	// Step 9: every PATH entry, then the program.
	pathVal, _ := lookupEnv(h.environ, "PATH")
	entries := filepath.SplitList(pathVal)
	for _, e := range entries {
		if why := pathEntryProblem(e, h.env.StateDirs, h.env.ManagedDirs, h.workspace); why != "" {
			return h.deliverRefusal(9,
				fmt.Sprintf("your PATH holds %s, %s, which a jail can write", shquote.QuoteDisplay(e), why),
				"Ask the human to remove that entry from PATH in the shell they ran the line in (or to open a fresh terminal without it), then to run the same line again.",
				"Remove it from this shell's PATH and run the line again.")
		}
	}
	program, lookErr := lookProgram(h.line.Argv[0], pathVal, h.startDir, h.env.ManagedDirs)

	// Step 10: the run directory.
	started := h.env.Now()
	b, err := newBuildDir(mb, h.mbPath, started, h.env.Rand)
	if err != nil {
		return h.step10(err)
	}
	req := Request{ID: b.id, Label: h.line.Label, Argv: h.line.Argv, Program: program, Cwd: h.startDir,
		Workspace: h.workspace, Started: rfc3339(started)}
	if err := writeExclusive(b.root, RequestFile, marshal(req)); err != nil {
		b.abandon()
		return h.step10(err)
	}
	hb, err := createExclusive(b.root, HeartbeatFile)
	if err != nil {
		b.abandon()
		return h.step10(err)
	}
	beat := func() { _, _ = hb.WriteAt([]byte(rfc3339(h.env.Now())), 0) }
	beat()
	if err := b.publish(); err != nil {
		hb.Close()
		b.abandon()
		return h.step10(err)
	}
	defer b.root.Close()
	h.prune()

	h.disclose(b.id, program, lookErr)
	stopBeat := h.heartbeat(beat)
	out := &spool{term: h.env.Stdout, cap: h.env.SpoolCap}
	errs := &spool{term: h.env.Stderr, cap: h.env.SpoolCap}
	exit, sig, rc := h.execute(program, lookErr, out, errs)
	stopBeat()
	hb.Close()
	ended := h.env.Now()

	res := Result{ID: b.id, Label: h.line.Label, Argv: h.line.Argv, Program: program, Cwd: h.startDir,
		Workspace: h.workspace, Exit: exit, Signal: sig, Started: rfc3339(started), Ended: rfc3339(ended),
		ElapsedMs: ended.Sub(started).Milliseconds(), StdoutBytes: out.total, StderrBytes: errs.total,
		StdoutTruncated: out.truncated(), StderrTruncated: errs.truncated(), YoloVersion: h.env.Version}
	deliverErr := h.deliver(b.root, out, errs, res)

	ev := brokeraudit.Event{Event: "run", Service: AuditService, Jail: h.jailName(), Workspace: h.workspace,
		Argv: h.line.Argv, Set: "human-run", Outcome: "ran", Request: b.id, Exit: exit,
		BytesOut: out.total + errs.total, ElapsedMs: res.ElapsedMs}
	if deliverErr != nil {
		ev.Reason = "delivery failed: " + deliverErr.Error()
	}
	h.env.Audit.Append(ev)

	status := fmt.Sprintf("exit %d", rc)
	if sig != "" {
		status = "killed by signal " + sig + fmt.Sprintf(" (exit %d)", rc)
	} else if exit == nil {
		status = fmt.Sprintf("did not start (exit %d)", rc)
	}
	if deliverErr != nil {
		h.say("ran (%s), but delivery failed: %v; copy the output above to the agent.", status, deliverErr)
		return rc
	}
	h.say("%s after %.1f s. Delivered %s stdout, %s stderr to run %s. Nothing to paste back.",
		status, ended.Sub(started).Seconds(), humanBytes(out.total), humanBytes(errs.total), b.id)
	return rc
}

func (h *hostRun) step10(err error) int {
	return h.refuse("cannot write to %s: %v; nothing ran. Run the command without `yolo host-run` and paste the output to the agent.", vis(h.mbPath), err)
}

func (h *hostRun) prune() {
	if err := prune(h.mb, h.env.Now()); err != nil {
		h.say("warning: could not prune old runs in %s: %v", vis(h.mbPath), err)
	}
}

// deliverRefusal is a refusal from step 8 on: a run directory holding refused.json, one audit
// line, the reason for the human, exit 64 (HX-D39).
func (h *hostRun) deliverRefusal(step int, reason, agentNext, humanNext string) int {
	now := h.env.Now()
	ref := Refusal{Label: h.line.Label, Argv: h.line.Argv, Cwd: h.startDir, Workspace: h.workspace,
		Step: step, Reason: reason, Next: agentNext, Time: rfc3339(now)}
	id, err := deliverRefusal(h.mb, h.mbPath, ref, now, h.env.Rand)
	h.env.Audit.Append(brokeraudit.Event{Event: "refusal", Service: AuditService, Jail: h.jailName(),
		Workspace: h.workspace, Argv: h.line.Argv, Set: "refused", Outcome: "refused", Reason: reason, Request: id})
	h.prune()
	if err != nil {
		h.say("%s; nothing ran. %s (The agent was not told: cannot write to %s: %v.)", reason, humanNext, vis(h.mbPath), err)
		return ExitRefused
	}
	h.say("%s; nothing ran. %s The agent was told, in run %s.", reason, humanNext, id)
	return ExitRefused
}

// disclose prints what will run, where, and where the result goes, with no way to hide it
// (HX-P5).
func (h *hostRun) disclose(id, program string, lookErr error) {
	var b strings.Builder
	fmt.Fprintf(&b, "yolo host-run: run %s", id)
	if h.line.Label != "" {
		fmt.Fprintf(&b, " (label %s)", h.line.Label)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  runs:     %s\n", shquote.JoinDisplay(h.line.Argv))
	if lookErr != nil {
		fmt.Fprintf(&b, "  program:  (not found: %s)\n", vis(lookErr.Error()))
	} else {
		fmt.Fprintf(&b, "  program:  %s\n", vis(program))
	}
	// The start directory is inside the workspace by construction (step 6 walked up to it from
	// here), so this note fires for it on every run; withinAny is what keeps that a fact rather
	// than an assumption when a caller hands RunHost a pair that disagrees.
	in := vis(h.startDir)
	if withinAny(h.startDir, []string{h.workspace}) {
		in += "  (agent-writable: .git/config, .envrc, mise.toml here take effect on your host)"
	}
	fmt.Fprintf(&b, "  in:       %s\n", in)
	if words := agentWritableArgs(h.line.Argv[1:], h.startDir, h.workspace); len(words) > 0 {
		fmt.Fprintf(&b, "  paths:    %s %s agent-writable, and flags such as -C, --git-dir, --work-tree or --prefix make a program act there\n",
			shquote.JoinDisplay(words), map[bool]string{true: "is", false: "are"}[len(words) == 1])
	}
	b.WriteString("  as:       you, with this terminal's environment and logins\n")
	if risky := riskyEnv(h.environ); len(risky) > 0 {
		fmt.Fprintf(&b, "  env:      %s\n", shquote.JoinDisplay(risky))
	}
	fmt.Fprintf(&b, "  result:   stdout, stderr and exit status go to %s/ when it exits, for the agent; the output also shows here\n", vis(h.mbPath))
	io.WriteString(h.env.Stderr, b.String())
}

// heartbeat rewrites the heartbeat on a ticker until the returned stop is called.
func (h *hostRun) heartbeat(beat func()) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(h.env.Heartbeat)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				beat()
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

// execute runs the program with the terminal as stdin and the two streams teed through the
// spools. The child stays in the terminal's foreground process group (no Setpgid, no Setsid),
// so Ctrl-C and a sudo prompt reach it directly; the host form ignores INT and forwards TERM
// and HUP, so it outlives the child and delivers on every path (HX-D8).
func (h *hostRun) execute(program string, lookErr error, out, errs *spool) (exit *int, sig string, rc int) {
	if lookErr != nil {
		msg := fmt.Sprintf("yolo host-run: cannot start %s: %v\n", h.line.Argv[0], lookErr)
		_, _ = errs.Write([]byte(msg))
		return nil, "", 127
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	cmd := &exec.Cmd{Path: program, Args: h.line.Argv, Dir: h.startDir, Env: h.environ,
		Stdin: h.env.Stdin, Stdout: out, Stderr: errs, WaitDelay: 2 * time.Second}
	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf("yolo host-run: cannot start %s: %v\n", vis(program), err)
		_, _ = errs.Write([]byte(msg))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", 127
		}
		return nil, "", 126
	}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case s := <-sigs:
				if s != syscall.SIGINT {
					_ = cmd.Process.Signal(s)
				}
			}
		}
	}()
	_ = cmd.Wait()
	close(stop)
	st := cmd.ProcessState
	if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		s := ws.Signal()
		return nil, s.String(), 128 + int(s)
	}
	code := st.ExitCode()
	return &code, "", code
}

// deliver writes stdout, stderr, then result.json last, through the run directory's held root.
func (h *hostRun) deliver(r *os.Root, out, errs *spool, res Result) error {
	if err := writeExclusive(r, StdoutFile, out.file()); err != nil {
		return err
	}
	if err := writeExclusive(r, StderrFile, errs.file()); err != nil {
		return err
	}
	return writeJSONRename(r, ResultFile, res)
}

// spool tees a stream to the terminal in full and keeps the first cap bytes for delivery.
type spool struct {
	term  io.Writer
	cap   int64
	buf   bytes.Buffer
	total int64
}

func (s *spool) Write(p []byte) (int, error) {
	_, _ = s.term.Write(p) // a closed terminal must not stop the spool
	s.total += int64(len(p))
	if room := s.cap - int64(s.buf.Len()); room > 0 {
		s.buf.Write(p[:min(int64(len(p)), room)])
	}
	return len(p), nil
}

func (s *spool) truncated() bool { return s.total > int64(s.buf.Len()) }

// file is the delivered stream: the spool, then the truncation marker past the cap.
func (s *spool) file() []byte {
	if !s.truncated() {
		return s.buf.Bytes()
	}
	b := append([]byte(nil), s.buf.Bytes()...)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return append(b, fmt.Sprintf("[yolo host-run: truncated after %s; %d more bytes were shown only on the host terminal]\n",
		humanBytes(s.cap), s.total-int64(s.buf.Len()))...)
}

func humanBytes(n int64) string {
	const k, m = 1 << 10, 1 << 20
	switch {
	case n >= m && n%m == 0:
		return fmt.Sprintf("%d MiB", n/m)
	case n >= m:
		return fmt.Sprintf("%.1f MiB", float64(n)/m)
	case n >= k && n%k == 0:
		return fmt.Sprintf("%d KiB", n/k)
	case n >= k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}
