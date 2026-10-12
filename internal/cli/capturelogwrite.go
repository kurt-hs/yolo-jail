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

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
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
	logCaptureChangeWith(overlay, s, writer, before, after, removedWhy, mode, nil)
}

// logCaptureChangeWith is logCaptureChange for a capture that refused the provider keys in drops
// (PP-D15): each is logged with that reason, and one the overlay did not hold before — taken in
// from the file and refused in the same write, so neither sidecar shows it — gets a line of its
// own (capturelog.Dropped), as the boot's capture logs it.
func logCaptureChangeWith(overlay captureFile, s manifest.Surface, writer string, before, after []byte,
	removedWhy string, mode fs.FileMode, drops []agentcfg.OwnedDrop) {
	if overlay.name == "" {
		return
	}
	now, surface := time.Now(), s.Agent+"/"+s.Name
	why := "not captured: " + s.Agent + "'s provider selection decides this key"
	reasons := capturelog.Reason{}
	var taken []string
	for _, d := range drops {
		key := providerKeyLabel(d.Pointer)
		reasons[key] = why
		if !capturelog.Held(before, key) {
			taken = append(taken, capturelog.Dropped(now, surface, writer, key, d.Value,
				"edited in the jail and "+why))
		}
	}
	lines := append(capturelog.Lines(now, surface, writer, before, after, reasons, removedWhy), taken...)
	if len(lines) == 0 {
		return
	}
	log := captureLogBeside(overlay)
	existing, _ := log.read()
	_ = log.replace(capturelog.Trim(existing, lines), mode)
}
