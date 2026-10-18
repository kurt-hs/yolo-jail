package cli

// features.go is `yolo features`: what this build of yolo can read in a pack, one capability per
// line (docs/reference/patched-forks.md PF-D71). It exists because a version string cannot answer
// that question — two different builds can print the same one — and the only other probe was
// linting a scratch pack on each host.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/outfmt"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/tty"
)

const featuresUsage = `Usage: yolo features [--format json]

List what this build of yolo can read in a pack, one capability per line, so a pack
author or a script can check a host before selecting a pack that needs one. On a
terminal each name is followed by what a pack uses it for; piped, each line is the
name alone, so a script can match it whole.

Three families of names:
  <name>         a field or mode of a pack, such as patch-series, patched-extensions
                 and skips-unreadable-contributions
  kind:<kind>    a contribution kind, such as kind:intercept
  via:<via>      a program delivery, such as via:source

A yolo with no ` + "`features`" + ` command predates every name it would list. The list is
what this build reads, not where it runs: a backend that cannot run a capability yet
says so at the launch.

Flags:
` + outputFormatUsage + `
  -h, --help               Show this help

With --format json, stdout is one document: {"features": [{"name", "summary"}, …]}.

Examples:
  yolo features                       # every capability this yolo reads, and what it is for
  yolo features --format json         # the same, with what each one is for
  yolo features | grep -qx patch-series || echo "this yolo predates patch series"`

// featuresDoc is `yolo features --format json`.
type featuresDoc struct {
	Features []packdecl.Feature `json:"features"`
}

// runFeatures runs `yolo features`.
func runFeatures(args []string) int {
	if answerHelp("features", args, os.Stdout) {
		return 0
	}
	format, ok := parseOutputFormat("features", args, os.Stderr)
	if !ok {
		return 2
	}
	if refuseUnknownFlags("features", args, []string{"--format", "--json", "--help", "-h", "features"},
		os.Stderr) {
		return 2
	}
	if extra := featuresPositional(args); extra != "" {
		fmt.Fprintf(os.Stderr, "yolo features: unexpected argument %q — it takes none.\n"+
			"Run `yolo features --help` for its flags.\n", extra)
		return 2
	}
	return featuresRun(os.Stdout, format, featuresStdoutIsTerminal())
}

// featuresStdoutIsTerminal decides whether the text form carries each name's summary: a person at
// a terminal reads what a name means, and a script reading a pipe gets the bare name it matches
// whole (the help's `grep -qx` example). A variable so the front-door test can stand in a terminal.
var featuresStdoutIsTerminal = func() bool { return tty.IsTerminalFile(os.Stdout) }

// featuresPositional is the first argument that is neither the command's own name, a flag, nor
// --format's value, "" when there is none: the command takes no argument, and a script that
// passed one (`yolo features patch-series`) must hear that it was not a query.
func featuresPositional(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case i == 0 && a == "features":
		case a == "--format":
			i++
		case len(a) > 1 && a[0] == '-':
		default:
			return a
		}
	}
	return ""
}

// featuresRun writes the capability list to w, as one JSON document or one name per line; with
// summaries set, each name is padded to one column and followed by what a pack uses it for.
func featuresRun(w io.Writer, format string, summaries bool) int {
	features := packdecl.Features()
	if outfmt.IsJSON(format) {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(featuresDoc{Features: features}); err != nil {
			return 1
		}
		return 0
	}
	width := 0
	for _, f := range features {
		width = max(width, len(f.Name))
	}
	for _, f := range features {
		if summaries {
			fmt.Fprintf(w, "%-*s  %s\n", width, f.Name, f.Summary)
			continue
		}
		fmt.Fprintln(w, f.Name)
	}
	return 0
}
