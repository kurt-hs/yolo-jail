package prune

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// CachePurgeProgressName is the file, under the build dir, that carries the shared-cache age
// purge's place from one bounded pass to the next (CachePurgeProgress).
const CachePurgeProgressName = "cache-purge-progress.json"

// CachePurgeProgress is where the cache age purge's two walks stand between passes.
//
// TWO CURSORS, because the walks are two cycles. The measurement (a dry run, for the next
// launch's offer) runs on every due pass; the removal runs only with consent, and goes as far as
// its own budget lets it. Each resumes where it stopped, so successive passes cover every subdir
// in turn rather than restarting at the first one each time.
type CachePurgeProgress struct {
	// Measure is where the measurement resumes; MeasureBytes and MeasureFiles are what the cycle in
	// progress has measured so far (zero when Measure is at the start).
	Measure      CachePurgeCursor `json:"measure"`
	MeasureBytes int64            `json:"measure_bytes,omitempty"`
	MeasureFiles int              `json:"measure_files,omitempty"`
	// Apply is where the consented removal resumes.
	Apply CachePurgeCursor `json:"apply"`
}

// LoadCachePurgeProgress reads the progress at path. A missing or unreadable file is the start
// of both cycles: the worst a lost file costs is walking again what a pass already walked.
func LoadCachePurgeProgress(path string) CachePurgeProgress {
	var p CachePurgeProgress
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &p) != nil {
			return CachePurgeProgress{}
		}
	}
	return p
}

// SaveCachePurgeProgress writes p to path, best-effort (a failed write only means the next pass
// starts again from where the file last said).
func SaveCachePurgeProgress(path string, p CachePurgeProgress) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// AddMeasurement folds one measuring pass into the cycle in progress and reports the cycle's
// totals so far; complete is true when this pass finished the cycle, which then starts again at
// the next pass.
func (p *CachePurgeProgress) AddMeasurement(r CachePurgeResult) (bytes int64, files int, complete bool) {
	if p.Measure.IsStart() {
		p.MeasureBytes, p.MeasureFiles = 0, 0
	}
	bytes, files = p.MeasureBytes+r.Bytes, p.MeasureFiles+r.Files
	p.Measure = r.Next
	if r.Partial {
		p.MeasureBytes, p.MeasureFiles = bytes, files
		return bytes, files, false
	}
	p.MeasureBytes, p.MeasureFiles = 0, 0
	return bytes, files, true
}

// Describe is one line saying which subdirs a pass covered and, when it stopped, where the next
// one resumes and what is left: what a log line or report says about a bounded pass.
func (r CachePurgeResult) Describe() string {
	covered := "nothing"
	if len(r.Covered) > 0 {
		covered = strings.Join(r.Covered, ", ")
	}
	if !r.Partial {
		return "covered " + covered + "; the cycle is complete"
	}
	at := r.Next.Sub
	if r.Next.Path != "" {
		at += "/" + r.Next.Path
	}
	left := append([]string{r.Remaining[0] + " (the rest of it)"}, r.Remaining[1:]...)
	return "covered " + covered + "; stopped at " + at + ", where the next pass resumes; left in this cycle: " +
		strings.Join(left, ", ")
}
