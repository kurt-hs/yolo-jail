package gitproto

import (
	"fmt"
	"sort"
	"strings"
)

// Side-band bands (gitprotocol-pack(5), "Packfile Data").
const (
	// BandData carries the protocol's own bytes: a pack, or a report.
	BandData byte = 1
	// BandProgress carries progress text the client shows on stderr.
	BandProgress byte = 2
	// BandError carries a fatal error message, after which the stream ends.
	BandError byte = 3
)

// Caps is a set of capability tokens as one side sent them, keyed by the whole token, so
// "agent=git/2.55.0" is one key.
type Caps map[string]bool

// ParseCaps reads a space-separated capability list.
func ParseCaps(s string) Caps {
	c := Caps{}
	for _, tok := range strings.Fields(s) {
		c[tok] = true
	}
	return c
}

// Has reports whether name was sent, bare or as name=value.
func (c Caps) Has(name string) bool {
	if c[name] {
		return true
	}
	_, ok := c.Value(name)
	return ok
}

// Value returns the value of a name=value capability.
func (c Caps) Value(name string) (string, bool) {
	for tok := range c {
		if v, ok := strings.CutPrefix(tok, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

// String renders the set sorted, space-separated, for a capability list.
func (c Caps) String() string {
	toks := make([]string, 0, len(c))
	for tok := range c {
		toks = append(toks, tok)
	}
	sort.Strings(toks)
	return strings.Join(toks, " ")
}

// sidebandChunk is the largest payload one side-band packet carries under the negotiated
// capability, or 0 when none was negotiated: side-band-64k fills a full pkt-line and side-band
// a 1000-byte one, each less its band byte.
func sidebandChunk(c Caps) int {
	switch {
	case c.Has("side-band-64k"):
		return MaxData - 1
	case c.Has("side-band"):
		return 1000 - 4 - 1
	}
	return 0
}

// UsesSideband reports whether the capabilities negotiate side-band multiplexing.
func UsesSideband(c Caps) bool { return sidebandChunk(c) > 0 }

// WriteSideband writes data on band, chunked to the size caps negotiate. With no side-band
// negotiated, band 1's data is written raw and any other band's is dropped: such a client has
// no channel for it.
func WriteSideband(w *Writer, caps Caps, band byte, data []byte) error {
	if band < BandData || band > BandError {
		return fmt.Errorf("%w: side-band %d does not exist", ErrProtocol, band)
	}
	chunk := sidebandChunk(caps)
	if chunk == 0 {
		if band != BandData {
			return nil
		}
		return w.Raw(data)
	}
	buf := make([]byte, 0, min(len(data), chunk)+1)
	for len(data) > 0 {
		n := min(len(data), chunk)
		buf = append(buf[:0], band)
		buf = append(buf, data[:n]...)
		if err := w.Data(buf); err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}
