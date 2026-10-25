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
// that way and handed down). When path no longer names f's file — the log was moved or
// replaced — nothing is trimmed: trimming a file this writer is not writing would bound
// nothing it writes.
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
		if st, perr := os.Stat(w.path); perr == nil {
			if fst, ferr := w.f.Stat(); ferr == nil && os.SameFile(st, fst) {
				_ = Trim(w.path)
			}
		}
	}
	return n, err
}
