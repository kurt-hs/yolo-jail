package run

// sessionholders.go names who still holds a jail's session lock, for the diagnostics a jail that
// stays up because sessions remain prints (docs/design/jail-lifetime-last-session-wins.md JL-P3):
// the count a session reads from the runtime (jailSessionCount) is not the count the jail drains
// on — the kernel-held session lock is (JL-P2) — so when the count cannot be read the phantom
// "1 other session" is replaced by the real holders. The holders are read back with the same
// non-blocking probe a quit already uses (readSessionLockHolder), and named from the host's lock
// table where the host has one (sessionholders_linux.go).

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// couldNotDetermineSessions is what a jail whose count AND holders both cannot be read says, in
// place of a number it does not have (JL-P3). It is never a count.
const couldNotDetermineSessions = "could not determine remaining sessions"

// sessionLockHolders names the processes holding cname's session lock. holder is what
// readSessionLockHolder sees: heldShared when a session holds it, heldByNobody or heldUnknown when
// no session can be named. names holds one line per holder, and is populated only where the host
// exposes its lock table (Linux /proc/locks); a holder the host cannot name is still a holder, so a
// caller must never read an empty names list as "no sessions" (JL-P3).
func sessionLockHolders(cname string) (names []string, holder sessionLockHolder) {
	f, err := os.OpenFile(sessionLockPath(cname), os.O_RDWR, 0)
	if err != nil {
		return nil, heldUnknown
	}
	defer f.Close()
	holder = readSessionLockHolder(f)
	if holder != heldShared {
		return nil, holder
	}
	for _, h := range procLockHolders(f) {
		names = append(names, h.String())
	}
	return names, holder
}

// sessionLockHolderFacts words cname's session-lock holders as a clause a keeper's log line can end
// with, or "" when the host names none. It never says "no sessions": an unnamed holder is not an
// absent one (JL-P3).
func sessionLockHolderFacts(cname string) string {
	names, _ := sessionLockHolders(cname)
	if len(names) == 0 {
		return ""
	}
	return "; still in it: " + strings.Join(names, ", ")
}

// lockHolder is one process holding a lock, as the host's lock table names it.
type lockHolder struct {
	PID   int
	Comm  string
	Since time.Time // zero when it could not be read
}

// String words a holder for a diagnostic line, naming its start time only when it could be read.
func (h lockHolder) String() string {
	switch {
	case h.Comm == "":
		return fmt.Sprintf("pid %d", h.PID)
	case h.Since.IsZero():
		return fmt.Sprintf("pid %d (%s)", h.PID, h.Comm)
	default:
		return fmt.Sprintf("pid %d (%s, since %s)", h.PID, h.Comm, h.Since.Format("15:04:05"))
	}
}
