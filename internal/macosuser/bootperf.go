package macosuser

// bootperf.go reads the macos-user bootstrap's step timings back into the launch's collector. The
// bootstrap runs as the sandbox account, in another process, so its steps cannot be spanned here;
// it writes one line per boot step to <workspace>/.yolo/boot-perf.log (entrypoint.BootPerfPath),
// and the launcher records each as a completed interval, macos_user.bootstrap.<step>
// (docs/reference/perf-logging.md). Timing only: nothing here can refuse or change a launch.

import (
	"strconv"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// maxBootPerfSteps caps how many steps one bootstrap's file may add to the host perf log. The file
// is the sandbox account's writing, so the host reads it as untrusted: bounded, every name reduced
// to [a-z0-9_], and a line that does not parse dropped. The boot table has fewer steps than this.
const maxBootPerfSteps = 80

// bootPerfStep is one boot step read back: its name and how long it took.
type bootPerfStep struct {
	name string
	dur  time.Duration
}

// clearBootPerf removes a previous bootstrap's timings before this one runs, so they are never
// recorded as this launch's, and returns the path to read afterwards — "" when the collector
// records nothing or the stale file could not be removed, in which case nothing is read.
func clearBootPerf(deps Deps, workspace string) string {
	if !deps.Perf.Enabled() || deps.RemoveFile == nil || deps.ReadFile == nil {
		return ""
	}
	path := entrypoint.BootPerfPath(workspace)
	if !deps.RemoveFile(path) {
		return ""
	}
	return path
}

// recordBootPerf records each step the bootstrap wrote to path as macos_user.bootstrap.<step>,
// on success and failure alike: a refused bootstrap's steps up to the refusal are the ones worth
// reading. Each step is recorded once whatever the file repeats.
func recordBootPerf(deps Deps, path string) {
	if path == "" {
		return
	}
	body, ok := deps.ReadFile(path)
	if !ok {
		return
	}
	for _, s := range parseBootPerf(body) {
		deps.Perf.Record("macos_user.bootstrap."+s.name, s.dur)
	}
}

// parseBootPerf reads the bootstrap's `<seconds since start>\t<step>` lines into steps, each
// lasting from the mark before it. A line that does not parse, or whose time runs backwards, is
// dropped; at most maxBootPerfSteps are returned.
func parseBootPerf(body string) []bootPerfStep {
	var out []bootPerfStep
	prev := 0.0
	for _, line := range strings.Split(body, "\n") {
		if len(out) == maxBootPerfSteps {
			break
		}
		secs, label, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		at, err := strconv.ParseFloat(secs, 64)
		if err != nil || at < prev || at > 24*3600 {
			continue
		}
		name := bootPerfName(label)
		if name == "" {
			continue
		}
		out = append(out, bootPerfStep{name: name, dur: time.Duration((at - prev) * float64(time.Second))})
		prev = at
	}
	return out
}

// bootPerfName reduces a step's label to a span-name segment: lower case, every run of other
// characters one underscore, trimmed, at most 64 bytes.
func bootPerfName(label string) string {
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
