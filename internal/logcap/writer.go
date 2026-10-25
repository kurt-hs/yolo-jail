package logcap

import (
	"os"
	"sync"
)

// Writer bounds a log WHILE a long-lived process writes it, for a writer that holds the log's
// descriptor for its whole life and so never reaches another Open: the in-jail nix root
// watcher, whose stderr is nix-roots.log for as long as the jail runs. After every write that
// leaves the file over MaxBytes it runs Trim on path, the same copy-and-truncate every Open
// does, so the inode f writes to stays the log and f's O_APPEND lands the next line at the new
// end. A trim that fails leaves the log as it was; the write's own result is what Write returns.
//
// f must be the log at path, opened O_APPEND (Open's shape, or a descriptor a parent opened
// that way and handed down). When path no longer names f's file — the log was moved,
// replaced or deleted — the file at path is left alone, since trimming it would bound nothing
// this writer writes, and f's own file is emptied in place instead, so a writer left holding
// an unlinked log does not grow it for the rest of its life.
type Writer struct {
	path string
	f    *os.File
	mu   sync.Mutex
}

// NewWriter returns a Writer appending to f, the log at path.
func NewWriter(path string, f *os.File) *Writer { return &Writer{path: path, f: f} }

// Write appends p, then trims the log when p took it past MaxBytes.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.f.Write(p)
	if over, serr := overCap(w.f); serr == nil && over {
		st, perr := os.Stat(w.path)
		fst, ferr := w.f.Stat()
		switch {
		case ferr != nil:
		case perr == nil && os.SameFile(st, fst):
			_ = Trim(w.path)
		default:
			// path names another file, or none: what f writes to is unlinked or moved, and
			// no reader finds it through path any more. Empty it rather than let it grow for
			// the writer's life; the file at path, if any, is not this writer's to touch.
			_ = w.f.Truncate(0)
		}
	}
	return n, err
}
