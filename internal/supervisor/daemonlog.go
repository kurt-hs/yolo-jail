package supervisor

// daemonlog.go is how a supervised daemon's output reaches <name>.log: through a pipe the
// supervisor reads, never as the child's own descriptor on the file. Two things follow, and
// both are about reading the log after the fact:
//
//   - EVERY LINE IS PLACED IN TIME. The copier stamps each line with logstamp's layout (local
//     time, milliseconds, offset) as it arrives, unless the daemon already stamped it
//     (logstamp.HasStamp). Before this, the wire bridge and both credential adapters wrote no
//     time at all, so a 401 in aws-auth.log could not be placed inside or outside the agent
//     hang it might explain; a bare fmt print, a third-party library's line and a panic trace
//     are stamped now too, without each daemon having to remember.
//   - THE LOG IS BOUNDED WHILE THE DAEMON RUNS. With the child holding the file, the 5 MB
//     rotation could only happen at the next spawn, and a daemon that logs one line per request
//     (the bridge, the adapters) lives as long as the jail. The supervisor is now the only
//     writer of the file a running child's output lands in, so it rotates by rename, to the
//     same single .log.1 generation, the moment a write would cross the cap.
//
// The pipe's other end is the child's stdout and stderr. A child that outlives the supervisor
// would get EPIPE on its next write; that is the existing posture (Run terminates every child
// on the way out), not a new one.

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
)

// copyDrainTimeout bounds how long a child's exit waits for its output to finish copying. The
// copier ends at EOF, which a grandchild still holding the pipe postpones indefinitely; the
// exit line must not wait on that, so after this the copier carries on alone.
const copyDrainTimeout = 2 * time.Second

// daemonSink is the rotating file under one daemon's output.
type daemonSink struct {
	name string
	mu   sync.Mutex
	f    *os.File
	size int64
	// rotates is false for a log that is not a regular file (a FIFO a test reads): there is
	// nothing to measure, and renaming it would not bound anything.
	rotates bool
}

func newDaemonSink(name string, f *os.File) *daemonSink {
	s := &daemonSink{name: name, f: f}
	if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
		s.size, s.rotates = st.Size(), true
	}
	return s
}

// Write appends p, first rotating the log when p would take it past logMaxBytes.
func (s *daemonSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return len(p), nil
	}
	if s.rotates && s.size > 0 && s.size+int64(len(p)) > logMaxBytes {
		s.rotate()
	}
	n, err := s.f.Write(p)
	s.size += int64(n)
	return n, err
}

// rotate retires the active log to the one archived generation and opens a fresh one. On any
// failure it keeps writing to what it has: the log is diagnostics, never a reason to stop.
func (s *daemonSink) rotate() {
	p := logPath(s.name)
	if err := os.Rename(p, p+".1"); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return // keep appending to the renamed file rather than lose the lines
	}
	_ = s.f.Close()
	s.f, s.size = f, 0
}

func (s *daemonSink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}

// copyDaemonOutput copies a child's output pipe into sink, one stamped line at a time, until
// EOF; then it flushes a final unterminated line, closes the pipe and the log, and closes done.
func copyDaemonOutput(pipe *os.File, sink *daemonSink, done chan<- struct{}) {
	defer close(done)
	lw := logstamp.Writer(sink)
	_, _ = io.Copy(lw, pipe)
	_ = lw.Flush()
	_ = pipe.Close()
	sink.close()
}

// awaitCopy waits for a copier to finish, at most copyDrainTimeout. nil is a spawn that never
// started one.
func awaitCopy(done <-chan struct{}) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(copyDrainTimeout):
	}
}
