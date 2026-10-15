package run

// baselagnote_test.go pins how a series' base lag (packsrc.BaseLag) is said: the releases newer than
// the base oldest first, elided past three to the oldest and the newest; which are known to take the
// series; and the rebase recipe that re-anchors it only when the newest is one of them.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

func TestBaseLagNoteNamesTheNewerReleases(t *testing.T) {
	const base = "1000000000000000000000000000000000000000"
	c := func(n byte) string { return string(n) + base[1:] }
	in := packsrc.CheckInputs{Repo: "r", Ref: "main", Base: base}
	rec := &packsrc.CheckRecord{Read: in, Check: &packsrc.CheckFound{RefKind: "branch", List: []packsrc.ListEntry{
		{Commit: c('6'), Tag: "v1.5.0"}, {Commit: c('5'), Tag: "v1.4.0"}, {Commit: c('4'), Tag: "v1.3.0"},
		{Commit: c('3'), Tag: "v1.2.0"}, {Commit: c('2'), Tag: "v1.1.0"}, {Commit: base, Tag: "v1.0.0"},
	}}, Outcomes: []packsrc.EntryOutcome{{Commit: c('3'), Kind: packsrc.OutcomeApplies, Series: "s"}}}
	lag := rec.BaseLag(in, "s")
	if lag == nil {
		t.Fatal("no base lag")
	}
	if got, want := BaseLagNote(lag), "its base v1.0.0 (10000000) is 5 releases behind: v1.1.0 … v1.5.0; "+
		"v1.2.0 takes the series as it stands"; got != want {
		t.Errorf("note:\n got %q\nwant %q", got, want)
	}
	f := packload.Fork{Pack: "forkpack", Bin: "tool", Source: "git+https://example.com/up.git?ref=main"}
	if steps := BaseLagSteps(f, lag, ""); steps != nil {
		t.Errorf("a step for a newest version not known to take the series: %q", steps)
	}

	rec.Good = &packsrc.GoodBuild{Commit: c('6'), Series: "s"}
	rec.Check.List = rec.Check.List[:1:1]
	rec.Check.List = append(rec.Check.List, packsrc.ListEntry{Commit: base, Tag: "v1.0.0"})
	lag = rec.BaseLag(in, "s")
	if got, want := BaseLagNote(lag), "its base v1.0.0 (10000000) is 1 release behind: v1.5.0, which takes the "+
		"series as it stands"; got != want {
		t.Errorf("note:\n got %q\nwant %q", got, want)
	}
	steps := strings.Join(BaseLagSteps(f, lag, ""), "\n")
	for _, w := range []string{"to re-anchor the series on v1.5.0, rebase it with git (it applies there as it stands):",
		"git clone https://example.com/up forkpack-tool-rebase", "git rebase " + c('6'), "--base=" + c('6')} {
		if !strings.Contains(steps, w) {
			t.Errorf("the re-anchoring steps lack %q:\n%s", w, steps)
		}
	}

	// No lag: another check's record, a tag ref, or nothing above the base.
	if rec.BaseLag(packsrc.CheckInputs{Repo: "r", Ref: "main", Base: c('6')}, "s") != nil {
		t.Error("a lag from a record of another base")
	}
	rec.Check.RefKind = "tag"
	if rec.BaseLag(in, "s") != nil {
		t.Error("a lag for a tag ref")
	}
}
