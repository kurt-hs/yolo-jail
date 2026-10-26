package run

import (
	"strings"
	"testing"
)

// ANSI escapes the shared internal/richtext renderer emits — mirrored here so
// these run-package wiring tests can assert on rendered output without reaching
// into richtext's unexported constants. The renderer itself (tag → ANSI, literal
// preservation) is unit-tested in internal/richtext; these tests only cover the
// run package's color-gating and diff-coloring wiring.
const (
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"
)

// TestPrinterColorGating verifies o.pr() — the launch-stream printer, which writes to
// stderr — emits ANSI only when Color AND STDERR is a TTY, and strips otherwise, so
// redirected output never carries escape codes. Stdout's tty-ness is not its question:
// `yolo run -- cmd | jq` pipes stdout while the launch lines still reach a terminal, and
// `yolo run -- cmd 2>log` the reverse.
func TestPrinterColorGating(t *testing.T) {
	mk := func(color, stderrTTY, stdoutTTY bool) string {
		var b strings.Builder
		// Getenv pinned: the gate reads NO_COLOR through it, so an unpinned (nil) seam
		// would make this test's answer the environment the suite runs in.
		o := &Options{Color: color, IsTTYStderr: func() bool { return stderrTTY },
			IsTTYStdout: func() bool { return stdoutTTY }, Stderr: &b,
			Getenv: func(string) string { return "" }}
		o.pr(&b).print("[green]hi[/green]")
		return b.String()
	}
	if got := mk(true, true, false); !strings.Contains(got, ansiGreen) {
		t.Errorf("color+stderr tty: expected ANSI, got %q", got)
	}
	for _, tc := range []struct {
		name                        string
		color, stderrTTY, stdoutTTY bool
	}{{"no-color", false, true, true}, {"no-tty", true, false, false}, {"stdout-tty-only", true, false, true}} {
		if got := mk(tc.color, tc.stderrTTY, tc.stdoutTTY); strings.Contains(got, "\x1b[") {
			t.Errorf("%s: expected NO ANSI, got %q", tc.name, got)
		} else if !strings.Contains(got, "hi") {
			t.Errorf("%s: text dropped: %q", tc.name, got)
		}
	}
}

// TestPayloadPrinterColorGating is prOut, the printer for a verb's stdout payload: its
// color asks whether STDOUT is a terminal.
func TestPayloadPrinterColorGating(t *testing.T) {
	mk := func(stdoutTTY, stderrTTY bool) string {
		var b strings.Builder
		o := &Options{Color: true, IsTTYStdout: func() bool { return stdoutTTY },
			IsTTYStderr: func() bool { return stderrTTY }, Getenv: func(string) string { return "" }}
		o.prOut(&b).print("[green]hi[/green]")
		return b.String()
	}
	if got := mk(true, false); !strings.Contains(got, ansiGreen) {
		t.Errorf("stdout tty: expected ANSI, got %q", got)
	}
	if got := mk(false, true); strings.Contains(got, "\x1b[") {
		t.Errorf("stderr tty only: expected NO ANSI on the stdout payload, got %q", got)
	}
}

// TestConfigDiffColored drives the changePrompter's diff rendering on a TTY and
// asserts +/- / hunk lines carry the expected ANSI colors (the reported bug: the
// config diff printed with no colors because the printer always stripped markup).
func TestConfigDiffColored(t *testing.T) {
	var out strings.Builder
	o := &Options{
		Color:       true,
		IsTTYStderr: func() bool { return true },
		IsTTYStdin:  func() bool { return true },
		Getenv:      func(string) string { return "" }, // no ambient NO_COLOR
		// The diff and its question are launch lines: on stderr, never the command's stdout.
		Stderr: &out,
		Stdin:  strings.NewReader("y\n"),
	}
	p := &changePrompter{o: o}
	ok := p.Prompt([]string{
		"--- last",
		"+++ current",
		"@@ -1 +1 @@",
		"-old: 1",
		"+new: 2",
	})
	if !ok {
		t.Fatal("Prompt should return true on 'y'")
	}
	s := out.String()
	if !strings.Contains(s, ansiGreen+"+new: 2") {
		t.Errorf("added line not green: %q", s)
	}
	if !strings.Contains(s, ansiRed+"-old: 1") {
		t.Errorf("removed line not red: %q", s)
	}
	if !strings.Contains(s, ansiCyan+"@@ -1 +1 @@") {
		t.Errorf("hunk header not cyan: %q", s)
	}
}
