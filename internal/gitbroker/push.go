package gitbroker

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokeraudit"
	"github.com/mschulkind-oss/yolo-jail/internal/brokercore"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// push.go is a push through the broker (docs/design/git-broker.md §4.2, git-broker-plan
// ID-9). receive-pack speaks first, so the upstream is open before the push's commands can be
// read; the broker relays the advertisement, reads the commands without forwarding a byte of
// them, judges every ref, and then either forwards the exact bytes it read (Reader.Tee's copy,
// never a re-encoding) followed by the rest of the client's stream, or refuses the push whole:
// the upstream gets an empty command list, which receive-pack treats as nothing to do, and
// the client gets git's own report with an `ng` line per ref.
//
// Once the advertisement has reached git, git ignores the helper's exit code, so every
// failure from there on is said inside git's protocol (the report) or ends the stream, never
// only as an Exit frame.

// receivePack runs one push and returns its exit code.
func (b *Broker) receivePack(u *upstream, in io.Reader, out io.Writer, ev *brokeraudit.Event, dest string, say func(string)) int {
	// 1. The advertisement, relayed packet by packet up to its flush.
	relay := bufio.NewWriterSize(out, 64<<10)
	adv := gitproto.NewReader(u.stdout)
	adv.Tee(relay)
	packets := 0
	for {
		p, err := adv.Read()
		if err != nil {
			_ = relay.Flush()
			_ = u.stdin.Close()
			_, _ = io.Copy(io.Discard, u.stdout)
			code := u.wait()
			if packets == 0 {
				ev.Set, ev.Outcome = "refused", "failed"
				ev.Reason = fmt.Sprintf("the upstream answered nothing (ssh exited %d)", code)
				return upstreamFailed(code, u.tail.String(), dest, say)
			}
			ev.Set, ev.Outcome, ev.Reason = "refused", "failed", "the upstream's advertisement ended early"
			say(fmt.Sprintf("the remote's advertisement ended before its end (ssh exited %d; its message, if any, is "+
				"above), so nothing was pushed. Try `ssh -T %s` on the host.", code, dest))
			return brokercore.ExitUnavailable
		}
		packets++
		if p.Kind == gitproto.Flush {
			break
		}
	}
	adv.Tee(nil)
	if err := relay.Flush(); err != nil {
		_ = u.stdin.Close()
		_, _ = io.Copy(io.Discard, u.stdout)
		u.wait()
		ev.Set, ev.Outcome, ev.Reason = "refused", "failed", "the jail stopped reading"
		return brokercore.ExitUnavailable
	}

	// 2. The commands, buffered exactly as they arrived and judged before a byte goes up.
	p, code, ok := b.readPush(in, ev, say)
	if !ok {
		_, _ = io.WriteString(u.stdin, "0000")
		_ = u.stdin.Close()
		_, _ = io.Copy(io.Discard, u.stdout)
		u.wait()
		return code
	}

	if !p.refused {
		// 3a. Forward: the judged bytes as read, then the rest of git's stream (the pack, when
		// there is one), and relay the upstream's answer.
		go func() {
			// A push of nothing ends at its flush: git sends nothing after it, and anything
			// that follows was never judged, so it never goes up.
			if _, err := u.stdin.Write(p.teed); err == nil && len(p.cmds) > 0 {
				_, _ = io.Copy(u.stdin, in)
			}
			_ = u.stdin.Close()
		}()
		_, _ = io.Copy(out, u.stdout)
		return u.wait()
	}

	// 3b. Refuse whole. The upstream gets an empty command list and nothing more.
	_, _ = io.WriteString(u.stdin, "0000")
	_ = u.stdin.Close()
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, u.stdout)
		close(drained)
	}()
	code = b.refuseToClient(out, in, p, dest, ev, say)
	<-drained
	u.wait()
	return code
}

// pushRead is a push's commands, read and judged.
type pushRead struct {
	cmds    []gitproto.Command
	caps    gitproto.Caps
	teed    []byte // the command section's bytes exactly as they arrived
	reasons []string
	refused bool
}

// readPush reads the push's commands from the client without forwarding a byte of them, and
// judges every ref. When they cannot be read it tells the user, records why, and returns ok
// false with the exit code; the caller has sent the upstream none of them. A push nothing
// refuses is recorded as run, in its set.
func (b *Broker) readPush(in io.Reader, ev *brokeraudit.Event, say func(string)) (pushRead, int, bool) {
	var teed bytes.Buffer
	cr := gitproto.NewReader(in)
	cr.Tee(&teed)
	cmds, caps, _, err := gitproto.ReadCommands(cr)
	cr.Tee(nil)
	if err != nil {
		ev.Set, ev.Outcome, ev.Reason = "refused", "refused", "unreadable push commands"
		if bytes.HasPrefix(firstPacket(teed.Bytes()), []byte("push-cert")) {
			ev.Reason = "signed push"
			say("a signed push (git push --signed) cannot be judged ref by ref, so nothing was forwarded and the " +
				"remote is unchanged. Run the same push without --signed.")
			return pushRead{}, brokercore.ExitUsage, false
		}
		reason := err.Error()
		if errors.Is(err, gitproto.ErrProtocol) {
			reason = strings.TrimPrefix(reason, gitproto.ErrProtocol.Error()+": ")
		}
		say("the push's commands could not be read (" + reason + "), so nothing was forwarded and the remote is " +
			"unchanged. Run the push again; if it repeats, push from the host.")
		return pushRead{}, brokercore.ExitUsage, false
	}
	refs, reasons, set := b.judge(cmds)
	ev.Refs = refs
	p := pushRead{cmds: cmds, caps: caps, teed: teed.Bytes(), reasons: reasons}
	for _, r := range reasons {
		p.refused = p.refused || r != ""
	}
	if !p.refused {
		ev.Set, ev.Outcome = set, "ran"
		if len(cmds) == 0 {
			ev.Set = "read" // a push of nothing writes nothing
		}
	}
	return p, 0, true
}

// refuseToClient answers a push refused whole: the user's message, git's own report with an
// `ng` line per ref, and the rest of git's stream read and dropped. The caller has sent the
// upstream nothing beyond, over SSH, an empty command list.
func (b *Broker) refuseToClient(out io.Writer, in io.Reader, p pushRead, dest string, ev *brokeraudit.Event, say func(string)) int {
	ev.Set, ev.Outcome, ev.Reason = "refused", "refused", refusalSummary(p.reasons)
	say("this push was refused whole, so nothing reached " + dest + " and the remote is unchanged; each ref's " +
		"reason is on its `! [remote rejected]` line. A push through the broker needs the user's approval, which " +
		"this version cannot ask for: push from the host instead (`git push` in this workspace there), where " +
		"`yolo audit --service git` shows what was refused.")
	if !p.caps.Has("report-status") && !p.caps.Has("report-status-v2") {
		for i, c := range p.cmds {
			say(c.Ref + ": " + p.reasons[i])
		}
	}
	_ = writeRefusal(out, p.caps, p.cmds, p.reasons)
	// git sends its pack after the commands and closes its side once the pack is written (or,
	// for a push of deletes only, once it has read the report); read it all and drop it, so
	// git never writes into a closed stream before it reads the report.
	_, _ = io.Copy(io.Discard, in)
	return brokercore.ExitNoPerm
}

// judge assigns every ref command its outcome: covered (reason ""), or the reason it is not.
// A ref outside refs/heads/ and refs/tags/ is refused always; any other ref nothing covers
// needs approval, which this version cannot ask for. A push is all or nothing (GB-D4), so
// when any ref is refused, a covered one is refused with the rest.
func (b *Broker) judge(cmds []gitproto.Command) ([]brokeraudit.RefUpdate, []string, string) {
	refs := make([]brokeraudit.RefUpdate, len(cmds))
	reasons := make([]string, len(cmds))
	set := ""
	anyRefused := false
	for i, c := range cmds {
		refs[i] = brokeraudit.RefUpdate{Ref: c.Ref, Old: c.Old, New: c.New}
		switch {
		case !strings.HasPrefix(c.Ref, "refs/heads/") && !strings.HasPrefix(c.Ref, "refs/tags/"):
			reasons[i] = reasonOutsideRefs
		case b.cover != nil:
			if s, by, ok := b.cover(c); ok {
				refs[i].CoveredBy = by
				if set == "" || s == "working-set" {
					set = s
				}
				continue
			}
			reasons[i] = reasonNeedsApproval
		default:
			reasons[i] = reasonNeedsApproval
		}
		anyRefused = true
	}
	if anyRefused {
		for i := range reasons {
			if reasons[i] == "" {
				reasons[i] = reasonWithTheRest
			}
		}
	}
	return refs, reasons, set
}

// refusalSummary is the audit line's reason for a refused push.
func refusalSummary(reasons []string) string {
	for _, r := range reasons {
		if r == reasonOutsideRefs {
			return "a ref outside refs/heads/ and refs/tags/"
		}
	}
	return reasonNeedsApproval
}

// writeRefusal is gitproto.WriteRefusal with a reason per ref: "unpack ok", then
// "ng <ref> <reason>" for every command, side-band wrapped when the client negotiated it, so
// git prints `! [remote rejected] <ref> (<reason>)` per ref and exits 1. A client that asked
// for no report gets none, as receive-pack would send it.
func writeRefusal(out io.Writer, caps gitproto.Caps, cmds []gitproto.Command, reasons []string) error {
	if !caps.Has("report-status") && !caps.Has("report-status-v2") {
		return nil
	}
	var report bytes.Buffer
	rw := gitproto.NewWriter(&report)
	if err := rw.Line("unpack ok\n"); err != nil {
		return err
	}
	for i, c := range cmds {
		reason := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == 0 {
				return ' '
			}
			return r
		}, reasons[i])
		if err := rw.Line("ng " + c.Ref + " " + reason + "\n"); err != nil {
			return err
		}
	}
	if err := rw.Flush(); err != nil {
		return err
	}
	w := gitproto.NewWriter(out)
	if !gitproto.UsesSideband(caps) {
		return w.Raw(report.Bytes())
	}
	if err := gitproto.WriteSideband(w, caps, gitproto.BandData, report.Bytes()); err != nil {
		return err
	}
	return w.Flush()
}

// firstPacket is the first data packet of a buffered command list after any shallow lines,
// or nil.
func firstPacket(teed []byte) []byte {
	r := gitproto.NewReader(bytes.NewReader(teed))
	for {
		p, err := r.Read()
		if err != nil || p.Kind != gitproto.Data {
			return nil
		}
		if !bytes.HasPrefix(p.Data, []byte("shallow ")) {
			return p.Data
		}
	}
}
