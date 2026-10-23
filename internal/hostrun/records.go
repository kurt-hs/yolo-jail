package hostrun

import (
	"io"
	"regexp"
	"strings"
	"time"
)

// The mailbox and the files of a run directory (design §5.2).
const (
	MailboxName   = "host-run"
	RequestFile   = "request.json"
	RefusedFile   = "refused.json"
	ResultFile    = "result.json"
	HeartbeatFile = "heartbeat"
	StdoutFile    = "stdout"
	StderrFile    = "stderr"
)

const idTimeLayout = "20060102T150405Z"

// base32 is RFC 4648's alphabet, lowercased.
const base32 = "abcdefghijklmnopqrstuvwxyz234567"

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-z2-7]{6}$`)

// NewRunID mints a run id: the UTC start second, then 30 bits from rnd in lowercase base32
// (HX-D38). Names sort by start time.
func NewRunID(now time.Time, rnd io.Reader) (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(rnd, b[:]); err != nil {
		return "", err
	}
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v >>= 2 // 30 bits
	suffix := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		suffix[i] = base32[v&31]
		v >>= 5
	}
	return now.UTC().Format(idTimeLayout) + "-" + string(suffix), nil
}

// ParseRunID reports whether name is a run id, and its start time.
func ParseRunID(name string) (time.Time, bool) {
	if !runIDPattern.MatchString(name) {
		return time.Time{}, false
	}
	t, err := time.Parse(idTimeLayout, name[:16])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// TmpName is the name a run directory is built under before its rename.
func TmpName(id string) string { return "." + id + ".tmp" }

// ParseTmpName reports whether name is a run directory's build name, and its id.
func ParseTmpName(name string) (string, bool) {
	inner, ok := strings.CutPrefix(name, ".")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(inner, ".tmp")
	if !ok {
		return "", false
	}
	if _, ok := ParseRunID(id); !ok {
		return "", false
	}
	return id, true
}

// Request is request.json: what the host is running, written before the run.
type Request struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Argv      []string `json:"argv"`
	Program   string   `json:"program"`
	Cwd       string   `json:"cwd"`
	Workspace string   `json:"workspace"`
	Started   string   `json:"started"`
}

// Refusal is refused.json: why the host ran nothing, and the agent's next step.
type Refusal struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Argv      []string `json:"argv"`
	Cwd       string   `json:"cwd"`
	Workspace string   `json:"workspace"`
	Step      int      `json:"step"`
	Reason    string   `json:"reason"`
	Next      string   `json:"next"`
	Time      string   `json:"time"`
}

// Result is result.json, written last: the completion signal.
type Result struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Argv            []string `json:"argv"`
	Program         string   `json:"program"`
	Cwd             string   `json:"cwd"`
	Workspace       string   `json:"workspace"`
	Exit            *int     `json:"exit"`
	Signal          string   `json:"signal"`
	Started         string   `json:"started"`
	Ended           string   `json:"ended"`
	ElapsedMs       int64    `json:"elapsed_ms"`
	StdoutBytes     int64    `json:"stdout_bytes"`
	StderrBytes     int64    `json:"stderr_bytes"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
	YoloVersion     string   `json:"yolo_version"`
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
