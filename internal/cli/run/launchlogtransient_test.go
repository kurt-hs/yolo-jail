package run

import (
	"bytes"
	"testing"
)

// A REDRAW STAYS TRANSIENT THROUGH STACKED TEES: a build jail's launch tees its stream into its own
// launch.log around the host launch's tee into host-launch.log, and a progress redraw handed to the
// outer tee reaches the terminal and neither log. Red if teeLog.WriteTransient writes its inner
// stream plainly, which put every frame into the inner log.
func TestARedrawStaysTransientThroughStackedTees(t *testing.T) {
	var term, inner, outer bytes.Buffer
	stacked := teeLog{w: teeLog{w: &term, log: &inner}, log: &outer}
	if n, err := stacked.WriteTransient([]byte("\r\x1b[K⠋ building")); err != nil || n != len("\r\x1b[K⠋ building") {
		t.Fatalf("WriteTransient = %d, %v", n, err)
	}
	if term.String() != "\r\x1b[K⠋ building" {
		t.Errorf("the terminal got %q, want the redraw", term.String())
	}
	if inner.Len() != 0 || outer.Len() != 0 {
		t.Errorf("a redraw reached a log: inner=%q outer=%q", inner.String(), outer.String())
	}
}
