// Package logstamp is the ONE timestamp every yolo daemon log line carries, and the writer
// that puts it there (docs/reference/wire-bridge.md, "The daemon log's line format").
//
// # WHY ONE PACKAGE
//
// The daemon logs were written by four hands in four shapes: Go's `log.LstdFlags`
// (`2026/10/11 12:16:42`, local, to the second), RFC 3339 in UTC to the second, an RFC 3339
// local stamp inside a bracket, and — for the wire bridge and both in-jail credential
// adapters — no stamp at all. A 401 the AWS credential adapter logged during a 13-minute agent
// hang could not be placed inside or outside the hang, and lines from two logs could not be
// interleaved to the second, let alone to the request. One layout, one place.
//
// # THE LAYOUT: LOCAL TIME, MILLISECONDS, NUMERIC OFFSET
//
// `2026-10-11T12:16:42.123-04:00` — RFC 3339 with exactly three fractional digits, in the
// process's LOCAL zone with its offset spelled out (or `Z` where local is UTC). Local because
// the human reading the log thinks in the clock on the wall ("the hang was 12:17–12:30"), and
// the offset because that makes every line unambiguous anyway: a reader comparing a UTC log
// converts by the offset on the line, never by a guess about the writer's zone. Fixed width
// (three digits always) so lines sort and align.
//
// # NEVER STAMPED TWICE
//
// A line can pass through two stampers: a daemon that stamps its own lines, running under the
// in-jail supervisor, which stamps every line its children write so that a bare fmt print or a
// panic trace is placed in time too. Writer therefore leaves a line that already BEGINS with a
// stamp in this layout alone (HasStamp), which is what lets both layers stamp without
// coordinating.
package logstamp

import (
	"bytes"
	"io"
	"log"
	"sync"
	"time"
)

// Layout is the stamp's time layout: RFC 3339, milliseconds, the local zone's numeric offset.
const Layout = "2006-01-02T15:04:05.000Z07:00"

// now is the clock, a variable so a test can pin a stamp; production never rebinds it.
var now = time.Now

// Format renders t in Layout, in the local zone.
func Format(t time.Time) string { return t.Local().Format(Layout) }

// Now is the current time in Layout.
func Now() string { return Format(now()) }

// HasStamp reports whether line begins with a stamp in Layout followed by a space.
func HasStamp(line []byte) bool {
	// 2006-01-02T15:04:05.000 is 23 bytes; then Z or ±hh:mm, then a space.
	if len(line) < 25 {
		return false
	}
	digits := func(b []byte) bool {
		for _, c := range b {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	if !digits(line[0:4]) || line[4] != '-' || !digits(line[5:7]) || line[7] != '-' ||
		!digits(line[8:10]) || line[10] != 'T' || !digits(line[11:13]) || line[13] != ':' ||
		!digits(line[14:16]) || line[16] != ':' || !digits(line[17:19]) || line[19] != '.' ||
		!digits(line[20:23]) {
		return false
	}
	rest := line[23:]
	switch {
	case rest[0] == 'Z':
		return rest[1] == ' '
	case rest[0] == '+' || rest[0] == '-':
		return len(rest) >= 7 && digits(rest[1:3]) && rest[3] == ':' && digits(rest[4:6]) && rest[6] == ' '
	}
	return false
}

// Writer returns an io.Writer that writes everything to w with each LINE prefixed by the
// stamp of the moment its first byte arrived, unless the line already carries one (HasStamp).
// Safe for concurrent use; a partial line is held until its newline arrives, so two
// goroutines' writes never tear one another's record. A held partial line is written by
// Flush, and by the next write when it completes.
func Writer(w io.Writer) *LineWriter { return &LineWriter{w: w} }

// LineWriter is Writer's result.
type LineWriter struct {
	mu      sync.Mutex
	w       io.Writer
	pending []byte
	stamp   string
}

// Write implements io.Writer. It always reports len(p) consumed: the sink is diagnostics, and
// a short count would make a caller's own logging retry or fail over a log it cannot write.
func (lw *LineWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	rest := p
	var err error
	for len(rest) > 0 {
		if len(lw.pending) == 0 {
			lw.stamp = Now()
		}
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			lw.pending = append(lw.pending, rest...)
			break
		}
		lw.pending = append(lw.pending, rest[:i+1]...)
		rest = rest[i+1:]
		if werr := lw.emit(); werr != nil && err == nil {
			err = werr
		}
	}
	return len(p), err
}

// Flush writes a held partial line, newline-terminated.
func (lw *LineWriter) Flush() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if len(lw.pending) == 0 {
		return nil
	}
	lw.pending = append(lw.pending, '\n')
	return lw.emit()
}

// SetOutput replaces the underlying writer, under the lock (a rotating sink reopens its file).
func (lw *LineWriter) SetOutput(w io.Writer) {
	lw.mu.Lock()
	lw.w = w
	lw.mu.Unlock()
}

func (lw *LineWriter) emit() error {
	line := lw.pending
	lw.pending = lw.pending[:0]
	var buf []byte
	if HasStamp(line) {
		buf = line
	} else {
		buf = make([]byte, 0, len(lw.stamp)+1+len(line))
		buf = append(buf, lw.stamp...)
		buf = append(buf, ' ')
		buf = append(buf, line...)
	}
	_, err := lw.w.Write(buf)
	return err
}

// Logger is a *log.Logger whose every line carries the stamp and nothing else of log's own
// (no flags): the one logger setup a daemon's diagnostics take.
func Logger(w io.Writer, prefix string) *log.Logger { return log.New(Writer(w), prefix, 0) }
