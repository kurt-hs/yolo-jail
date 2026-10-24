package gitproto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxCommandBytes caps the bytes of a push's command list and push options together. The
// broker holds the whole section in memory before judging it, and the jail chooses its size;
// 32 MiB is a push of a few hundred thousand refs. Each packet is charged its wire bytes plus
// packetOverhead, so a flood of empty packets cannot hold far more memory than the cap.
const MaxCommandBytes = 32 << 20

// packetOverhead is the charge per packet beyond its wire bytes: roughly the string header and
// slice growth it costs to hold one push option or command.
const packetOverhead = 32

// Command is one ref update of a push: "<old-id> <new-id> <ref>".
type Command struct {
	Old string
	New string
	Ref string
}

// Create reports whether the command creates its ref (the old id is all zeros).
func (c Command) Create() bool { return isZeroID(c.Old) }

// Delete reports whether the command deletes its ref (the new id is all zeros).
func (c Command) Delete() bool { return isZeroID(c.New) }

func isZeroID(s string) bool { return s != "" && strings.Trim(s, "0") == "" }

// validID reports whether s is an object id as git writes one: 40 (SHA-1) or 64 (SHA-256)
// lower-case hex digits.
func validID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validRef reports whether s can be a ref name on the wire: non-empty, with no space, control
// byte or DEL. Which refs a push may name is policy, and the broker's.
func validRef(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] == 0x7f {
			return false
		}
	}
	return true
}

// NeedsPack reports whether a push of cmds carries a pack after its commands: every push does
// unless each command is a delete (an empty push included).
func NeedsPack(cmds []Command) bool {
	for _, c := range cmds {
		if !c.Delete() {
			return true
		}
	}
	return false
}

// ReadCommands reads a push's update request from the client side of receive-pack, up to and
// not including the pack: any "shallow" lines, the command list to its flush, the capabilities
// on its first line, and, when the client sent the push-options capability, the push options to
// their flush. An empty command list (a lone flush) is a push of nothing, with no options
// section. A signed push (push-cert) is refused: its commands are inside a certificate this
// package does not parse.
func ReadCommands(r *Reader) ([]Command, Caps, []string, error) {
	var (
		cmds  []Command
		caps  = Caps{}
		total int
	)
	next := func(what string) (Packet, error) {
		p, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return p, fmt.Errorf("%w: the push ended inside its %s: %w", ErrProtocol, what, io.ErrUnexpectedEOF)
			}
			return p, err
		}
		total += len(p.Data) + 4 + packetOverhead
		if total > MaxCommandBytes {
			return p, fmt.Errorf("%w: the push's %s exceeds %d bytes (each packet charged %d bytes over its length)",
				ErrProtocol, what, MaxCommandBytes, packetOverhead)
		}
		if p.Kind == Delim || p.Kind == ResponseEnd {
			return p, fmt.Errorf("%w: a %s packet inside the push's %s", ErrProtocol, p.Kind, what)
		}
		return p, nil
	}
	for {
		p, err := next("command list")
		if err != nil {
			return nil, nil, nil, err
		}
		if p.Kind == Flush {
			break
		}
		line := string(bytes.TrimSuffix(p.Data, []byte("\n")))
		if len(cmds) == 0 {
			if id, ok := strings.CutPrefix(line, "shallow "); ok {
				if !validID(id) {
					return nil, nil, nil, fmt.Errorf("%w: a shallow line names %q, not an object id", ErrProtocol, id)
				}
				continue
			}
			if line == "push-cert" || strings.HasPrefix(line, "push-cert\x00") {
				return nil, nil, nil, fmt.Errorf("%w: a signed push (git push --signed) cannot be judged; push without --signed", ErrProtocol)
			}
		}
		text, capList, hasCaps := strings.Cut(line, "\x00")
		if hasCaps {
			if len(cmds) != 0 {
				return nil, nil, nil, fmt.Errorf("%w: capabilities on a command other than the first", ErrProtocol)
			}
			caps = ParseCaps(capList)
		}
		c, err := parseCommand(text)
		if err != nil {
			return nil, nil, nil, err
		}
		cmds = append(cmds, c)
	}
	if len(cmds) == 0 {
		return nil, caps, nil, nil
	}
	var opts []string
	if caps.Has("push-options") {
		for {
			p, err := next("push options")
			if err != nil {
				return nil, nil, nil, err
			}
			if p.Kind == Flush {
				break
			}
			opts = append(opts, string(bytes.TrimSuffix(p.Data, []byte("\n"))))
		}
	}
	return cmds, caps, opts, nil
}

func parseCommand(text string) (Command, error) {
	f := strings.SplitN(text, " ", 3)
	if len(f) != 3 {
		return Command{}, fmt.Errorf("%w: %q is not \"<old-id> <new-id> <ref>\"", ErrProtocol, text)
	}
	c := Command{Old: f[0], New: f[1], Ref: f[2]}
	if !validID(c.Old) || !validID(c.New) || len(c.Old) != len(c.New) {
		return Command{}, fmt.Errorf("%w: %q does not carry two object ids of one hash", ErrProtocol, text)
	}
	if !validRef(c.Ref) {
		return Command{}, fmt.Errorf("%w: %q is not a ref name", ErrProtocol, c.Ref)
	}
	return c, nil
}

// WriteCommands writes a push's command list, as ReadCommands reads one: caps on the first
// command, a flush, then, when caps carry push-options, the options and a flush. No commands
// is a lone flush. Lines are written without a trailing newline, as git's send-pack writes
// them; the reader accepts either. It is not how a judged push is forwarded (it drops shallow
// lines and re-orders the capabilities); forward the bytes Reader.Tee recorded.
func WriteCommands(w *Writer, cmds []Command, caps Caps, opts []string) error {
	for i, c := range cmds {
		line := c.Old + " " + c.New + " " + c.Ref
		if i == 0 && len(caps) > 0 {
			line += "\x00" + caps.String()
		}
		if err := w.Line(line); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if len(cmds) == 0 || !caps.Has("push-options") {
		return nil
	}
	for _, o := range opts {
		if err := w.Line(o); err != nil {
			return err
		}
	}
	return w.Flush()
}

// WriteRefusal answers a push the broker refuses whole, in git's own report-status: "unpack
// ok", then "ng <ref> <reason>" for every command, so git prints
// "! [remote rejected] <ref> (<reason>)" per ref and exits 1. It is side-band wrapped when the
// client negotiated side-band. A client that asked for neither report-status nor
// report-status-v2 gets nothing, as receive-pack would send it; the caller reports the reason
// on stderr. A newline in reason would end the line early, so it becomes a space.
func WriteRefusal(out io.Writer, caps Caps, cmds []Command, reason string) error {
	if !caps.Has("report-status") && !caps.Has("report-status-v2") {
		return nil
	}
	reason = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == 0 {
			return ' '
		}
		return r
	}, reason)
	var report bytes.Buffer
	rw := NewWriter(&report)
	if err := rw.Line("unpack ok\n"); err != nil {
		return err
	}
	for _, c := range cmds {
		if err := rw.Line("ng " + c.Ref + " " + reason + "\n"); err != nil {
			return err
		}
	}
	if err := rw.Flush(); err != nil {
		return err
	}
	w := NewWriter(out)
	if !UsesSideband(caps) {
		return w.Raw(report.Bytes())
	}
	if err := WriteSideband(w, caps, BandData, report.Bytes()); err != nil {
		return err
	}
	return w.Flush()
}

// Report is a parsed report-status.
type Report struct {
	// Unpack is the unpack status: "ok", or the reason the pack failed.
	Unpack string
	// Refs maps each reported ref to "" when it was updated ("ok <ref>") or to its reason
	// ("ng <ref> <reason>").
	Refs map[string]string
	// Order lists the reported refs in the order the report gave them.
	Order []string
	// Progress is the side-band progress text (band 2) read alongside the report.
	Progress []byte
}

// OK reports whether ref was reported updated.
func (r Report) OK(ref string) bool {
	reason, ok := r.Refs[ref]
	return ok && reason == ""
}

// ReadReport reads receive-pack's report-status (or report-status-v2, whose option lines it
// skips) to its flush. With sideband, the report is read out of band 1 up to the outer flush;
// band 2 is kept as Progress and band 3 ends the read with the remote's error.
func ReadReport(r *Reader, sideband bool) (Report, error) {
	var rep Report
	if sideband {
		var inner bytes.Buffer
		for {
			p, err := r.Read()
			if err != nil {
				return rep, reportReadErr(err)
			}
			if p.Kind == Flush {
				break
			}
			if p.Kind != Data || len(p.Data) == 0 {
				return rep, fmt.Errorf("%w: a %s packet in a side-band report", ErrProtocol, p.Kind)
			}
			switch p.Data[0] {
			case BandData:
				inner.Write(p.Data[1:])
			case BandProgress:
				rep.Progress = append(rep.Progress, p.Data[1:]...)
			case BandError:
				return rep, fmt.Errorf("the remote reported an error: %s", strings.TrimSpace(string(p.Data[1:])))
			default:
				return rep, fmt.Errorf("%w: side-band %d does not exist", ErrProtocol, p.Data[0])
			}
		}
		r = NewReader(&inner)
	}
	rep.Refs = map[string]string{}
	first := true
	for {
		p, err := r.Read()
		if err != nil {
			return rep, reportReadErr(err)
		}
		if p.Kind == Flush {
			break
		}
		if p.Kind != Data {
			return rep, fmt.Errorf("%w: a %s packet in a report", ErrProtocol, p.Kind)
		}
		line := string(bytes.TrimSuffix(p.Data, []byte("\n")))
		if first {
			status, ok := strings.CutPrefix(line, "unpack ")
			if !ok {
				return rep, fmt.Errorf("%w: a report opening with %q, not \"unpack\"", ErrProtocol, line)
			}
			rep.Unpack = status
			first = false
			continue
		}
		switch {
		case strings.HasPrefix(line, "ok "):
			rep.add(strings.TrimPrefix(line, "ok "), "")
		case strings.HasPrefix(line, "ng "):
			ref, reason, _ := strings.Cut(strings.TrimPrefix(line, "ng "), " ")
			if reason == "" {
				reason = "(no reason given)"
			}
			rep.add(ref, reason)
		case strings.HasPrefix(line, "option "):
			// report-status-v2 detail about the ref before it; nothing to judge.
		default:
			return rep, fmt.Errorf("%w: %q is not a report line", ErrProtocol, line)
		}
	}
	if first {
		return rep, fmt.Errorf("%w: an empty report", ErrProtocol)
	}
	return rep, nil
}

func (r *Report) add(ref, reason string) {
	if _, seen := r.Refs[ref]; !seen {
		r.Order = append(r.Order, ref)
	}
	r.Refs[ref] = reason
}

func reportReadErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: the report ended before its flush: %w", ErrProtocol, io.ErrUnexpectedEOF)
	}
	return err
}
