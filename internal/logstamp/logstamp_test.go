package logstamp

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func pinClock(t *testing.T, at time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
}

func TestFormatIsLocalWithMillisecondsAndOffset(t *testing.T) {
	zone := time.FixedZone("EDT", -4*3600)
	at := time.Date(2026, 10, 11, 12, 17, 3, 7_000_000, zone)
	old := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = old })
	if got, want := Format(at), "2026-10-11T12:17:03.007-04:00"; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	time.Local = time.UTC
	if got, want := Format(at), "2026-10-11T16:17:03.007Z"; got != want {
		t.Fatalf("Format in UTC = %q, want %q", got, want)
	}
}

func TestWriterStampsEveryLineOnceAndHoldsAPartialLine(t *testing.T) {
	pinClock(t, time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local))
	stamp := Now()
	var out bytes.Buffer
	w := Writer(&out)
	_, _ = w.Write([]byte("one\ntw"))
	if got := out.String(); got != stamp+" one\n" {
		t.Fatalf("after a line and a half: %q", got)
	}
	_, _ = w.Write([]byte("o\n" + stamp + " already stamped\nthree"))
	_ = w.Flush()
	want := stamp + " one\n" + stamp + " two\n" + stamp + " already stamped\n" + stamp + " three\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestHasStamp(t *testing.T) {
	for line, want := range map[string]bool{
		"2026-10-11T12:17:03.007-04:00 x": true,
		"2026-10-11T16:17:03.007Z x":      true,
		"2026-10-11T16:17:03Z x":          false,
		"2026/10/11 12:16:42 INFO x":      false,
		"wire-bridge: binding":            false,
		"2026-10-11T12:17:03.007-04:00":   false,
	} {
		if got := HasStamp([]byte(line)); got != want {
			t.Errorf("HasStamp(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestLoggerLinesCarryTheStampAndNoLogFlags(t *testing.T) {
	var out bytes.Buffer
	Logger(&out, "").Printf("hello %d", 1)
	line := out.String()
	if !HasStamp([]byte(line)) || !strings.HasSuffix(line, " hello 1\n") {
		t.Fatalf("logger line %q", line)
	}
}
