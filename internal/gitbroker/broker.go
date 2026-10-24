// Package gitbroker is the git-broker, `yolo internal daemon git-broker`: the host half of
// git as a brokered source (docs/design/git-broker.md §4, docs/design/git-broker-plan.md
// tasks 5 and 6). One runs per jail, behind yolo's loopback-TLS front, on the broker core
// (internal/brokercore).
//
// The jail's remote helper (internal/gitremote) sends one request frame naming the service
// and the remote URL, then git's bytes (ID-5). The broker checks the URL against the launch's
// live remote-url scope, connects to the approved URL over the host's own ssh (ID-7) or over
// smart HTTP with the host's own `git credential fill` (ID-8, https.go), and:
//
//   - for a fetch (git-upload-pack), relays both ways: a `read`;
//   - for a push (git-receive-pack), relays the upstream's advertisement, reads the push's ref
//     commands before forwarding anything, and judges every ref (ID-9). A push nothing covers
//     is refused whole in git's own report: over SSH the upstream is sent an empty command list and
//     nothing else, over HTTPS no POST at all, and git prints `! [remote rejected] <ref> (<reason>)` per ref.
//     A covered push (a new branch under a create-branch grant, a branch in the workspace's
//     working set, recorded or declared in config by a branch grant: policy.go) is forwarded, and the upstream's report updates the working set.
//
// Every call is audited with `service: git` (ID-13).
package gitbroker

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokercore"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/brokerstore"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/frameproto"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Source is the git source's name: the store directory, the scope file's source and the
// audit log's `service`.
const Source = "git"

// Name is the daemon's name, and every log line's prefix.
const Name = "git-broker"

const (
	serviceUploadPack  = "git-upload-pack"
	serviceReceivePack = "git-receive-pack"
)

// reasonNeedsApproval is a push ref's refusal until requests turn on (step 3), the design's
// own words (§6).
const reasonNeedsApproval = "needs approval, which this version cannot ask for"

// reasonOutsideRefs is the refusal of a ref no grant can ever cover (ID-9).
const reasonOutsideRefs = "only refs/heads/ and refs/tags/ cross the git-broker; push other refs from the host"

// reasonWithTheRest is a covered ref's line in a push refused whole (GB-D4: all or nothing).
const reasonWithTheRest = "refused with the rest of this push: a push through the git-broker goes through whole or not at all"

// request is the helper's frame (ID-5). Agent is carried and never trusted; the helper of
// this version sends none.
type request struct {
	Service  string
	URL      string
	Protocol string
	Agent    string
	Invalid  string
}

// Broker answers one jail's git connections.
type Broker struct {
	ctx       context.Context
	scopeFile string
	bound     brokerscope.File
	start     string
	spawnCwd  string
	getenv    func(string) string
	audit     *brokeraudit.Log
	log       io.Writer
	now       func() time.Time
	seq       atomic.Int64
	// store is the git source's host store, which holds every workspace's working set
	// (brokerstore.WorkingEntry).
	store *brokerstore.Store
	// cover, when set, replaces the push policy (coverFor) for every ref command. It is a test
	// seam and nothing else: nothing outside tests sets it.
	cover coverer
	// rootCAs is the HTTPS upstream's trust roots; nil, always outside tests, is the system's.
	// It is a test seam and nothing else: no config key or environment variable reaches it,
	// and nothing turns verification off (docs/design/git-broker-plan.md task 6's fence).
	rootCAs *x509.CertPool
}

// Main is `yolo internal daemon git-broker`: the core's daemon around this source's broker.
func Main(argv []string) int {
	return brokercore.Daemon{
		Name:           Name,
		Source:         Source,
		SelfCheck:      SelfCheck,
		SelfCheckUsage: "report the host ssh and git and exit (used by `yolo check`)",
		NewHandler: func(ctx context.Context, scopeFile string, sf brokerscope.File, spawnCwd string, stderr io.Writer) (brokercore.Launch, int) {
			b, cleanup, err := newBroker(ctx, scopeFile, sf, spawnCwd, stderr, os.Getenv)
			l := brokercore.Launch{Cleanup: cleanup, Log: stderr}
			if err != nil {
				fmt.Fprintln(stderr, Name+": refusing host authority:", err, "; relaunch the jail")
				return l, 1
			}
			l.Handle = b.Handle
			return l, 0
		},
	}.Main(argv)
}

// newBroker binds a fresh broker lifetime to the launch's scope file and returns the broker
// and what ends the binding.
//
// THE LOG NAMES NO WORKSPACE, NO REPOSITORY, NO REMOTE, NO HOST PATH AND NO REF: it is shared
// by every jail on the machine, under a directory a `mounts` entry commonly exposes
// (internal/ghbroker/daemon.go says why at length). Request numbers, services and outcomes
// only; the audit log holds the rest.
func newBroker(ctx context.Context, scopeFile string, sf brokerscope.File, spawnCwd string, log io.Writer, getenv func(string) string) (*Broker, func(), error) {
	start, err := brokerstore.NewStartID()
	if err != nil {
		return nil, func() {}, fmt.Errorf("no fresh broker lifetime identity: %w", err)
	}
	bound, lease, err := brokercore.BindLease(Source, scopeFile, sf, start)
	if err != nil {
		return nil, func() {}, err
	}
	b := &Broker{ctx: ctx, scopeFile: scopeFile, bound: bound, start: start, spawnCwd: spawnCwd, getenv: getenv,
		audit: brokeraudit.Open(paths.BrokerAuditLog(), func(msg string) { fmt.Fprintln(log, Name+":", msg) }),
		log:   log, now: time.Now, store: &brokerstore.Store{Dir: paths.BrokerSourceDir(Source), Source: Source}}
	fmt.Fprintln(log, Name+": serving this jail's git remotes through the host's ssh and git credentials; a push goes through when a create-branch grant or the workspace's working set (recorded, or declared in config) covers every ref")
	return b, func() { _ = brokercore.CloseLease(Source, lease) }, nil
}

// parseRequest reads the helper's frame.
func parseRequest(s *hostservice.Session) request {
	var r request
	str := func(key string) string {
		v, ok := s.Get(key)
		if !ok {
			return ""
		}
		sv, ok := v.(string)
		if !ok {
			r.Invalid = "malformed request frame"
		}
		return sv
	}
	r.Service, r.URL, r.Protocol, r.Agent = str("service"), str("url"), str("protocol"), str("agent")
	return r
}

// Handle is one jail connection: the request frame, then git's bytes as Stdout frames, an
// empty one when git closed its side (ID-5).
func (b *Broker) Handle(s *hostservice.Session) {
	req := parseRequest(s)
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		for {
			f, err := frameproto.ReadFrame(s.Conn())
			if err != nil {
				// The jail went away mid-call: end the upstream rather than leave it running.
				pw.CloseWithError(errors.New("the jail's connection ended"))
				cancel()
				return
			}
			if f.StreamID != frameproto.StreamStdout {
				continue
			}
			if len(f.Payload) == 0 {
				pw.Close()
				return
			}
			if _, err := pw.Write(f.Payload); err != nil {
				return
			}
		}
	}()
	s.Exit(b.serveRequest(ctx, req, s.JailID, pr, sessionWriter{s.StdoutBytes}, sessionWriter{func(p []byte) { s.Stderr(string(p)) }}))
}

// sessionWriter writes each chunk as one frame.
type sessionWriter struct{ send func([]byte) }

func (w sessionWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.send(p)
	}
	return len(p), nil
}

// countingWriter counts what reaches the client's stdout.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// serveRequest answers one request and returns its exit code. in is git's bytes from the
// jail, ending when git closed its side; stdout reaches git, stderr the user. It appends the
// audit line whatever happened.
func (b *Broker) serveRequest(ctx context.Context, req request, jail string, in io.Reader, stdout, stderr io.Writer) int {
	if in == nil {
		in = bytes.NewReader(nil)
	}
	started := b.now()
	n := b.seq.Add(1)
	out := &countingWriter{w: stdout}
	ev := brokeraudit.Event{Event: "call", Service: Source, Jail: jail, Workspace: b.bound.Workspace,
		Argv: []string{req.Service}, Remote: req.URL}
	say := func(msg string) { fmt.Fprint(stderr, Name+": "+msg+"\n") }
	finish := func(code int) int {
		ev.Exit = &code
		ev.BytesOut = out.n
		ev.ElapsedMs = b.now().Sub(started).Milliseconds()
		b.audit.AppendContext(ctx, ev)
		service := "an unknown service"
		if req.Service == serviceUploadPack || req.Service == serviceReceivePack {
			service = req.Service
		}
		fmt.Fprintf(b.log, "%s: request %d: %s, %s/%s, exit %d\n", Name, n, service, ev.Set, ev.Outcome, code)
		return code
	}
	refuse := func(set, reason, msg string, code int) int {
		ev.Set, ev.Outcome, ev.Reason = set, "refused", reason
		say(msg)
		return finish(code)
	}

	if req.Invalid != "" {
		return refuse("refused", req.Invalid, req.Invalid+"; nothing ran. Run git again; if it repeats, the jail's "+
			"`yolo git-remote` does not match this host's yolo: start a fresh jail.", brokercore.ExitUsage)
	}
	if req.Service != serviceUploadPack && req.Service != serviceReceivePack {
		return refuse("refused", "service not brokered", fmt.Sprintf("%q does not cross the git-broker: only fetch "+
			"(git-upload-pack) and push (git-receive-pack) do, so nothing ran. Run it on the host instead.", req.Service),
			brokercore.ExitUsage)
	}

	// THE LIVE SCOPE, AS THE FILE READS NOW: a host `yolo config apply` that rewrote it reaches
	// this broker at this request.
	f, err := brokercore.Live(Source, b.scopeFile, b.bound, b.start)
	if err != nil {
		return refuse("refused", err.Error(), "current host authority refused: "+err.Error()+"; nothing ran. Relaunch the jail.",
			brokercore.ExitNoPerm)
	}
	if f.Form != brokerscope.FormRemoteURL {
		return refuse("refused", "scope file is not a remote-url scope", "this launch's scope file does not hold "+
			"remote URLs, so no remote is approved; nothing ran. Relaunch the jail.", brokercore.ExitNoPerm)
	}
	// A WORKSPACE HOLDING NO READ GRANT AT ALL is told so before anything about its remotes: its
	// launch recorded none, since a source with no read grant stays out of the gate
	// (docs/design/boundary-broker.md BB-D110), so "not in the approved scope" would name the
	// wrong next step.
	if !brokerscope.HoldsRead(f.Grants) {
		ev.Repo, _ = gitproto.Identity(req.URL)
		return refuse("refused", "no read grant", config.BrokerNoReadRefusal(Source, req.URL, f.LiveScope),
			brokercore.ExitUsage)
	}
	if !slices.Contains(f.Repos, req.URL) {
		return refuse("out-of-scope", "remote not in the approved scope", outOfScope(req.URL, f), brokercore.ExitUsage)
	}
	if err := brokerscope.ValidateCurrent(b.scopeFile, b.bound, req.URL); err != nil {
		return refuse("refused", err.Error(), "refused: "+err.Error()+"; nothing ran.", brokercore.ExitUsage)
	}
	ev.Repo, _ = gitproto.Identity(req.URL)
	// READ IS THE FLOOR (docs/design/boundary-broker.md BB-D108): a selected pack's broker grants
	// nothing until a read grant in the live scope file covers the remote, so a fetch and a push
	// alike are refused here. The gate approved the grant into the file, and only the gate takes
	// it out: a `yolo config apply` after the user config drops it.
	if !brokerscope.ReadCovers(f.Grants, ev.Repo, func(g, r string) bool { return g == r }) {
		return refuse("refused", "no read grant", config.BrokerNoReadRefusal(Source, req.URL, f.LiveScope),
			brokercore.ExitUsage)
	}
	transport, err := transportOf(req.URL)
	if err != nil {
		return refuse("refused", "transport not brokered", "the approved remote "+req.URL+" does not cross the git-broker: "+
			err.Error()+". Only ssh://, https:// and scp-style user@host:path remotes reach the host, so nothing ran. "+
			"Give the remote its SSH or HTTPS URL (`git remote set-url`), or fetch it on the host.", brokercore.ExitUsage)
	}
	var cover coverer
	if req.Service == serviceReceivePack {
		// The live scope's grants and the user's grant kinds, as they read at this push.
		cover = b.coverFor(f, ev.Repo, say)
	}
	if transport == transportHTTPS {
		return finish(b.serveHTTPS(ctx, req, in, out, &ev, cover, say))
	}

	argv, err := sshArgv(req.URL, req.Service, req.Protocol)
	if err != nil {
		return refuse("refused", "remote not usable over ssh", "the approved remote "+req.URL+" cannot be reached over ssh: "+
			err.Error()+"; nothing ran. Fix the remote's URL with `git remote set-url`.", brokercore.ExitUsage)
	}
	ssh, err := lookPath("ssh", b.getenv("PATH"))
	if err != nil {
		return refuse("refused", "no ssh on the host", "the host has no ssh on the PATH yolo was launched with, so "+
			"the broker cannot reach the remote; nothing ran. Install OpenSSH on the host and start a fresh jail.",
			brokercore.ExitUnavailable)
	}
	if why := brokercore.PlacementRefusal("ssh", b.bound.Workspace, b.spawnCwd)(ssh); why != "" {
		return refuse("refused", "host ssh refused", why, brokercore.ExitUnavailable)
	}
	dir := paths.BrokerSourceDir(Source)
	_ = os.MkdirAll(dir, 0o700)
	u, err := startUpstream(ctx, ssh, argv, sshEnv(b.getenv, req.Protocol), dir, stderr)
	if err != nil {
		return refuse("refused", "host ssh did not start", "the host's ssh did not start: "+err.Error()+"; nothing ran. "+
			"Check that it runs on the host (`ssh -V`).", brokercore.ExitUnavailable)
	}
	dest := argv[len(argv)-2]
	if req.Service == serviceUploadPack {
		ev.Set = "read"
		return finish(b.uploadPack(u, in, out, &ev, dest, say))
	}
	return finish(b.receivePack(u, in, out, &ev, dest, cover, say))
}

// uploadPack relays a fetch both ways, and returns its exit code.
func (b *Broker) uploadPack(u *upstream, in io.Reader, out io.Writer, ev *brokeraudit.Event, dest string, say func(string)) int {
	go func() {
		_, _ = io.Copy(u.stdin, in)
		_ = u.stdin.Close()
	}()
	sent, _ := io.Copy(out, u.stdout)
	code := u.wait()
	if sent == 0 {
		ev.Outcome, ev.Reason = "failed", fmt.Sprintf("the upstream answered nothing (ssh exited %d)", code)
		return upstreamFailed(code, u.tail.String(), dest, say)
	}
	if code != 0 {
		// git has had protocol bytes, so it ignores this exit code and judges the stream
		// itself; the audit and the user still hear that the upstream failed.
		ev.Outcome, ev.Reason = "failed", fmt.Sprintf("the upstream exited %d after answering", code)
		say(fmt.Sprintf("the remote's ssh exited %d partway through (its message, if any, is above), so this fetch may "+
			"be incomplete; run it again, or try `ssh -T %s` on the host.", code, dest))
		return code
	}
	ev.Outcome = "ran"
	return code
}

// upstreamFailed tells the user why an upstream that never answered failed, and returns the
// exit code the helper hands git. No protocol bytes reached git, so the helper prints no
// connected line and git reports the failure (ID-5).
func upstreamFailed(code int, tail, dest string, say func(string)) int {
	if code == 255 {
		lead := "the host's ssh could not connect to " + dest + " (ssh exited 255; its own message is above)."
		if strings.Contains(tail, "Permission denied") {
			lead = "the host has no SSH key " + dest + " accepts (ssh exited 255: Permission denied)."
		}
		say(lead + " Run `ssh-add -l` on the host to list the keys its agent holds, and `ssh -T " + dest +
			"` there to try the connection; add a key with `ssh-add`. The broker never prompts for a password, " +
			"and the jail holds no key.")
		return brokercore.ExitUnavailable
	}
	say(fmt.Sprintf("the remote answered nothing (ssh exited %d; its message, if any, is above), so nothing was "+
		"transferred. Try `ssh -T %s` on the host.", code, dest))
	return brokercore.ExitUnavailable
}

// outOfScope is the refusal of a remote the launch did not approve (§6): it names the scope
// and the config-change gate, the one way a remote joins.
func outOfScope(remote string, f brokerscope.File) string {
	scope := "empty: this workspace had no remote when its scope was approved"
	if len(f.Repos) > 0 {
		scope = strings.Join(f.Repos, ", ")
	}
	how := "restart the jail and approve the new remote at the launch's config-change gate"
	if f.LiveScope {
		how = "ask the user to run `yolo config apply` on the host, in this workspace, and approve the new remote at " +
			"its config-change gate; this jail's running broker takes it at its next call"
	}
	return "the remote " + remote + " is not in this workspace's approved scope (" + scope + "), so nothing ran and " +
		"the host's ssh was not started. A remote joins the scope only at the config-change gate on the host: add it " +
		"with `git remote add <name> <url>` in the workspace, then " + how + ". Nothing sent through git adds a remote."
}
