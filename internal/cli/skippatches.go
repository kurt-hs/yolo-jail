package cli

// skippatches.go is YOLO_SKIP_PATCHES (patchrecovery.SkipPatchesEnv; docs/reference/patched-forks.md §8.5
// §6.5): for a covered patched fork or patched extension, this run delivers its UPSTREAM BUILD — the
// upstream's newest list entry, as the fork's follow rule picks it, built with no series under the
// fork's own build recipe — in the series' place.
//
// THE UPSTREAM BUILD IS ITS OWN FORK (packload.Fork.UpstreamBuild): the same pack, bin, source,
// build and produces, the empty series, and a key of its own ("<pack>/<bin>#upstream"). It runs
// through the one advance every patched build runs through, so it is checked under the same
// throttle, built in the same sealed jail, cached in the capture store and reused by the next run
// with the variable. Everything that advance keys by the fork's key is the upstream build's own:
// its check record, its build lock (the build id carries the key), its receipts, the reap after its
// move (onlyThisFork matches receipts by key), and the record of what a launch handed. So the
// series' check record — its good build, recorded failures, walk-back hold and back-offs — is
// neither read for a decision nor written, nothing of the series is reaped by the upstream build's
// move, and no upstream build is reaped by the series' move: with the variable unset, the next
// launch is the one it would have been.
//
// WHO READS IT: a fresh jail launch's fork-build slot and tree arm, `yolo host -- <bin>` and `yolo
// host apply --assert` (the floor's program and the host's extensions), and `yolo capture`. The
// detached advances and the preparation scheduler never do: their environment is an allowlist
// (preparationEnvironment) that carries no hatch, and they advance only the series' keys.

import (
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// skipsPatches reports whether YOLO_SKIP_PATCHES, as this process reads it, covers f: a patched fork
// or a patched extension. Never an unmodified extension, which has no patches to skip, nor an
// upstream build itself.
func skipsPatches(f packload.Fork) bool {
	if f.Upstream || !f.Patched() || f.Unmodified() {
		return false
	}
	return patchrecovery.Read(nil, patchrecovery.SkipPatchesEnv).Covers(recoverySubject(f))
}

// upstreamBuildOf is what this run builds and delivers for f: its upstream build when
// YOLO_SKIP_PATCHES covers it, f itself otherwise.
func upstreamBuildOf(f packload.Fork) packload.Fork {
	if skipsPatches(f) {
		return f.UpstreamBuild()
	}
	return f
}

// advanceUpstreamBuild runs f's upstream build's advance and, when it hands a build, says the one
// disclosure line on o.out. Nothing serving leaves the delivery with no key and the hatches that
// remain (advance.recoveryHatches' upstream arm), which the launch's missing-build refusal lists.
func advanceUpstreamBuild(f packload.Fork, o advanceOptions) advanceResult {
	r := advancePatchedFork(f.UpstreamBuild(), o)
	if r.delivery.Key != "" {
		sayUpstreamBuild(o.out, o.color, "", "running", f, r.good)
	}
	return r
}

// sayUpstreamBuild prints f's disclosure line, prefix first: "fork pi-fork/pi: running upstream
// v1.4.0 without its 3 patches". A disclosure, so on every run and behind no flag.
func sayUpstreamBuild(w io.Writer, color bool, prefix, verb string, f packload.Fork, g *packsrc.GoodBuild) {
	if w == nil {
		return
	}
	richtext.Printer{W: w, Color: color}.Printf("[yellow]%s[/yellow]", richtext.Escape(prefix+upstreamBuildLine(verb, f, g)))
}

// upstreamBuildLine is the disclosure line's text for f's upstream build g (nil: the record's), verb
// "running" where it is delivered, "built" where `yolo capture` built it.
func upstreamBuildLine(verb string, f packload.Fork, g *packsrc.GoodBuild) string {
	if g == nil {
		g = upstreamGood(f)
	}
	version := "build"
	if g != nil {
		version = packsrc.ListEntry{Commit: g.Commit, Tag: g.Tag}.ShortLabel()
	}
	return f.Label() + ": " + verb + " upstream " + version + " without " + seriesNoun(f)
}

// seriesNoun is what an upstream build leaves out of f, as the disclosure line names it: "its 3
// patches", "its patch branch".
func seriesNoun(f packload.Fork) string {
	if f.PatchBranch != "" {
		return "its patch branch"
	}
	s, err := f.ReadSeries()
	if err != nil {
		return "its patches"
	}
	return "its " + run.PatchCount(s.Len())
}

// upstreamGood is the good build f's upstream build's own record names, nil for none.
func upstreamGood(f packload.Fork) *packsrc.GoodBuild {
	rec, err := (&packsrc.Store{Dir: paths.PacksDir()}).LoadCheckRecord(f.UpstreamBuild().Key())
	if err != nil || rec == nil {
		return nil
	}
	return rec.Good
}

// upstreamBuildBackingOff reports whether f's upstream build has nothing to run and its last build
// failed under this yolo and is still inside its back-off: YOLO_SKIP_PATCHES would not get a launch
// running, so a refusal does not list it (§6.8). Reads the upstream build's record, never the series'.
func upstreamBuildBackingOff(f packload.Fork) bool {
	if f.Upstream {
		return false
	}
	u := f.UpstreamBuild()
	rec, err := (&packsrc.Store{Dir: paths.PacksDir()}).LoadCheckRecord(u.Key())
	if err != nil || rec == nil {
		return false
	}
	recipe := forkBuild{Fork: u, Series: packsrc.EmptySeries()}.recipe()
	if g := rec.Good; g != nil && g.Recipe == recipe {
		return false
	}
	a := &advance{f: u, rec: rec, series: packsrc.EmptySeries(), recipe: recipe, yolo: patchedYoloVersion()}
	for _, o := range rec.Outcomes {
		if o.Kind == packsrc.OutcomeBuildFailed && a.backingOff(o.Commit) {
			return true
		}
	}
	return false
}

// noteSkipPatchesFresh is the one line where YOLO_SKIP_PATCHES covers f and the launch asked for
// --update, whose fresh preparation is of the series as it stands: the hatch does nothing there.
func noteSkipPatchesFresh(w io.Writer, color bool, f packload.Fork) {
	if skipsPatches(f) {
		richtext.Printer{W: w, Color: color}.Printf("[dim]%s[/dim]", richtext.Escape(
			f.Label()+": "+patchrecovery.SkipPatchesEnv+" has no effect with --update"))
	}
}
