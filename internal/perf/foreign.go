package perf

// foreign.go turns step timings another process wrote — a boot that ran where no host span could
// be open — into completed intervals for Record. Both readers take a file the jail side can write,
// so the rules are the untrusted-input ones: bounded, every name reduced to a span-name segment,
// and a mark that does not parse or whose time runs backwards dropped. The macos-user bootstrap's
// boot-perf.log (macosuser/bootperf.go) and a container boot's ~/.yolo-perf.log block
// (run/jailperf.go) are its two callers; each parses its own line format into marks.

import (
	"strings"
	"time"
)

// MaxForeignSteps caps how many steps one foreign record may add to a launch's perf log. The boot
// table has fewer steps than this.
const MaxForeignSteps = 80

// ForeignMark is one checkpoint another process wrote: seconds since its start, and its label.
type ForeignMark struct {
	At    float64
	Label string
}

// ForeignStep is one step read back: its sanitized name and how long it took.
type ForeignStep struct {
	Name string
	Dur  time.Duration
}

// StepsFromMarks reads marks into steps, each lasting from the mark before it (the first from
// zero). A mark whose time runs backwards, exceeds a day or whose label sanitizes to nothing is
// dropped; at most MaxForeignSteps are returned.
func StepsFromMarks(marks []ForeignMark) []ForeignStep {
	var out []ForeignStep
	prev := 0.0
	for _, m := range marks {
		if len(out) == MaxForeignSteps {
			break
		}
		if m.At != m.At || m.At < prev || m.At > 24*3600 { // m.At != m.At: NaN
			continue
		}
		name := SegmentName(m.Label)
		if name == "" {
			continue
		}
		out = append(out, ForeignStep{Name: name, Dur: time.Duration((m.At - prev) * float64(time.Second))})
		prev = m.At
	}
	return out
}

// SegmentName reduces a label to a span-name segment: lower case, every run of other characters
// one underscore, trimmed, at most 64 bytes.
func SegmentName(label string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			b.WriteRune(r)
		} else {
			sep = true
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}
