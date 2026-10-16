package broker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// legacyhome.go answers one question about a machine-wide copy an older yolo left running
// (LegacyDeps): does it work on THIS home's credentials? The copies of one loophole share the
// credentials file and the refresh lock under the home they run in, never under the machine, so
// a copy started under another HOME (another user's, or a test's private one) shares nothing
// with a launch here, even though its rendezvous paths, which are keyed by the loophole name in
// /tmp, are the same. A launch names only a copy of its own home, and leaves it running
// (internal/cli/run/launchcheck.go; docs/design/host-daemon-ownership.md).

// LegacyCopyServesHome reports whether the machine-wide copy deps names runs under home, read from
// its process's HOME in /proc/<pid>/environ. Where that cannot be read (no PID file, no /proc, as
// on macOS, or a process this user may not inspect) it answers TRUE: on a single-user host the one
// copy there is always the user's own, and a refusal that names a stop the user can run is the
// safe side of a doubt.
func LegacyCopyServesHome(deps Deps, home string) bool {
	pid, ok := BrokerReadPID(deps)
	if !ok {
		return true
	}
	got, ok := processHome(pid)
	if !ok {
		return true
	}
	return sameDir(got, home)
}

// processHome is pid's HOME as its environment holds it, and false when that cannot be read.
func processHome(pid int) (string, bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return "", false
	}
	for _, kv := range strings.Split(string(raw), "\x00") {
		if v, ok := strings.CutPrefix(kv, "HOME="); ok {
			return v, true
		}
	}
	return "", false
}

// sameDir compares two directory paths after cleaning them and resolving symlinks where they
// resolve, so a home reached through a link reads as itself.
func sameDir(a, b string) bool {
	resolve := func(p string) string {
		p = filepath.Clean(p)
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return resolve(a) == resolve(b)
}
