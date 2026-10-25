package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

// agedCacheFile writes rel under the shared cache, 60 days old.
func agedCacheFile(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(paths.GlobalStorage(), "cache", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("xyz"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTheCachePassResumesFromItsSavedPlace is the call-site pin for the cursor: the slot reads
// the saved place of each walk and starts there, so a removal cursor on pip leaves uv (which sorts
// first, and which the cycle already covered) alone and reaches pip; a pass that finishes both
// cycles clears the cursors and stamps the debounce. It fails if the slot stops passing either
// cursor (uv's file goes, or the measurement counts it) or stops saving the progress.
func TestTheCachePassResumesFromItsSavedPlace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	o := goldenOptions(t.TempDir(), home)
	o.Now = time.Now
	uv := agedCacheFile(t, "uv/old")
	pip := agedCacheFile(t, "pip/old")
	progressPath := filepath.Join(paths.BuildDir(), prune.CachePurgeProgressName)
	prune.SaveCachePurgeProgress(progressPath, prune.CachePurgeProgress{
		Measure:      prune.CachePurgeCursor{Sub: "pip"},
		MeasureBytes: 1000, MeasureFiles: 10, // what earlier passes of this cycle measured
		Apply: prune.CachePurgeCursor{Sub: "pip"},
	})

	var c reclaimConsent
	c.grant(cachePurgeClass, true)
	o.measureAndPurgeCache(c, nil)

	if _, err := os.Stat(uv); err != nil {
		t.Errorf("uv/old was removed: the removal restarted at the first subdir instead of resuming at pip")
	}
	if _, err := os.Stat(pip); !os.IsNotExist(err) {
		t.Errorf("pip/old survived: the resumed removal did not reach pip")
	}
	if m := LastOfferMeasurement(cachePurgeClass); m.Partial || m.Bytes != 1003 {
		t.Errorf("measurement = %+v, want the cycle's 1000 B so far plus pip's 3 B, complete", m)
	}
	if p := prune.LoadCachePurgeProgress(progressPath); !p.Measure.IsStart() || !p.Apply.IsStart() || p.MeasureBytes != 0 {
		t.Errorf("progress after a finished cycle = %+v, want both cursors back at the start", p)
	}
	if due, _ := o.classDebounce(CacheHousekeepingClass); due {
		t.Errorf("a pass that finished both cycles left the class due")
	}
}

// TestTheCacheDebounceWaitsForTheWholeCycle: a pass the budget cuts short saves where it stopped
// and leaves the class due, so the next launch continues the cycle; the pass that finishes it
// stamps the debounce. Without the cursor the second pass would start again at uv.
func TestTheCacheDebounceWaitsForTheWholeCycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	o := goldenOptions(t.TempDir(), home)
	o.Now = time.Now
	agedCacheFile(t, "uv/old")
	agedCacheFile(t, "pex/old")
	progressPath := filepath.Join(paths.BuildDir(), prune.CachePurgeProgressName)

	orig := cacheWalkBudget
	cacheWalkBudget = -time.Second // every deadline already past: the pass is cut at once
	o.measureAndPurgeCache(reclaimConsent{}, nil)
	cacheWalkBudget = orig

	if p := prune.LoadCachePurgeProgress(progressPath); p.Measure.Sub != prune.CachePurgeDefaultSubdirs[0] {
		t.Errorf("a cut pass saved %+v, want its place in the first subdir", p)
	}
	if due, _ := o.classDebounce(CacheHousekeepingClass); !due {
		t.Fatalf("a cut pass stamped the debounce")
	}
	if m := LastOfferMeasurement(cachePurgeClass); !m.Partial {
		t.Errorf("a cut pass recorded a complete measurement")
	}
	log, _ := os.ReadFile(filepath.Join(o.Workspace, ".yolo", housekeepingLogName))
	if !strings.Contains(string(log), "left in this cycle: uv (the rest of it)") ||
		!strings.Contains(string(log), "nce") {
		t.Errorf("the log does not say what is left of the cycle:\n%s", log)
	}

	o.measureAndPurgeCache(reclaimConsent{}, nil)
	if due, _ := o.classDebounce(CacheHousekeepingClass); due {
		t.Errorf("the pass that finished the cycle did not stamp the debounce")
	}
	if m := LastOfferMeasurement(cachePurgeClass); m.Partial || m.Bytes != 6 {
		t.Errorf("measurement = %+v, want both files' 6 B, complete", m)
	}
}

// TestTheSlotsRunTheCopilotPkgReaper is the call-site pin for the Copilot self-update store:
// both slot bodies run it, and it deletes under the slot's guard. prune's own tests prove the rule;
// nothing there notices a slot that stopped calling it.
func TestTheSlotsRunTheCopilotPkgReaper(t *testing.T) {
	src, err := os.ReadFile("housekeeping.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"func (o *Options) runHousekeeping(", "func (o *Options) runMacosUserHousekeeping("} {
		if !strings.Contains(afterFunc(string(src), fn), "o.reapCopilotPkgVersions(guard)") {
			t.Errorf("%s no longer runs reapCopilotPkgVersions under the guard", fn)
		}
	}
}

// TestTheCopilotPkgReaperRemovesOldVersionsOncePerDay: the slot's class removes a version past the
// newest three that nothing used for a week, notes it, and is debounced; the opt-out skips it.
func TestTheCopilotPkgReaperRemovesOldVersionsOncePerDay(t *testing.T) {
	for _, optOut := range []bool{false, true} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		o := goldenOptions(t.TempDir(), home)
		o.Now = time.Now
		if optOut {
			o.Getenv = func(k string) string {
				if k == autoReapOptOutEnv {
					return "1"
				}
				return ""
			}
		}
		pkg := filepath.Join(paths.GlobalStorage(), "cache", "copilot", "pkg", "linux-x64")
		past := time.Now().Add(-60 * 24 * time.Hour)
		for _, v := range []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"} {
			f := filepath.Join(pkg, v, "index.js")
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			_ = os.Chtimes(f, past, past)
			_ = os.Chtimes(filepath.Dir(f), past, past)
		}
		o.reapCopilotPkgVersions(nil)
		_, err := os.Stat(filepath.Join(pkg, "1.0.1"))
		if gone := os.IsNotExist(err); gone == optOut {
			t.Errorf("optOut=%v: 1.0.1 removed=%v", optOut, gone)
		}
		if _, err := os.Stat(filepath.Join(pkg, "1.0.2")); err != nil {
			t.Errorf("optOut=%v: one of the newest three was removed", optOut)
		}
		if due, _ := o.classDebounce(CopilotPkgHousekeepingClass); due != optOut {
			t.Errorf("optOut=%v: due=%v after the pass", optOut, due)
		}
	}
}
