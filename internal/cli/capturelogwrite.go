package cli

// capturelogwrite.go is the host CLI's writer of the CAPTURE LOG (internal/capturelog): every
// change a `yolo config` verb or the capture at shutdown makes to a capture overlay appends its
// lines to capture.log beside the overlay, so "which keys were captured, when, and what were
// they" has an answer the next time an overlay surprises someone.
//
// THROUGH captureFile, never a plain path: the store is the workspace's .yolo/prism, which the
// jail can write, so the log is read and replaced beneath the same link-refusing root the overlay
// is (captureFile.replace). Best effort: the log is a record, and a verb whose real write
// succeeded is not failed for it.

import (
	"io/fs"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/capturelog"
)

// captureLogBeside is the capture log in the same store, and under the same root, as overlay.
func captureLogBeside(overlay captureFile) captureFile {
	f := overlay
	f.refusals = nil
	f.name = filepath.Join(filepath.Dir(overlay.name), capturelog.FileName)
	return f
}

// logCaptureChange appends the lines for the change from before to after in s's overlay, written
// by writer; removedWhy is said of every leaf the change took out.
func logCaptureChange(overlay captureFile, s manifest.Surface, writer string, before, after []byte,
	removedWhy string, mode fs.FileMode) {
	if overlay.name == "" {
		return
	}
	lines := capturelog.Lines(time.Now(), s.Agent+"/"+s.Name, writer, before, after, nil, removedWhy)
	if len(lines) == 0 {
		return
	}
	log := captureLogBeside(overlay)
	existing, _ := log.read()
	_ = log.replace(capturelog.Trim(existing, lines), mode)
}
