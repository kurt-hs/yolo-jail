package cli

// patchedrebasekeep_test.go pins `yolo pack rebase --keep` (patchedrebase.go, docs/design/
// patched-forks.md §8.4 step 5): a target the series takes as it stands is rebased onto all the
// same, the finished clone kept, and the export printed — the one a resolved conflict's clone prints
// — whose series names the target as its base-commit; the pack is never written; and without the
// flag the clean answer is what it was, the clone removed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// THE SCRATCH REBASE WITH --keep, on the host: a series that applies to v1.1.0 is rebased onto it,
// the clone stays finished, the pack's bytes do not move, and the printed export, run as printed,
// gives a series whose base is v1.1.0. The same run without --keep removes the clone and says so.
func TestPackRebaseKeepKeepsACleanCloneAndPrintsItsExport(t *testing.T) {
	f := newPatchedFixture(t, "")
	v11 := f.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	t.Setenv("TMPDIR", t.TempDir())
	before := treeDigest(t, f.forkDir)

	// WITHOUT THE FLAG, the old answer.
	plain := filepath.Join(t.TempDir(), "plain")
	rc, out, errw := rebaseVerb(t, "forkpack/tool", "--pack", f.forkDir, "--onto", "v1.1.0", "--into", plain)
	if rc != 0 || !strings.Contains(out, "takes the series as it stands (2 patches") ||
		!strings.Contains(out, "; nothing to rebase, so the clone is removed\n") || strings.Contains(out, "--keep") ||
		len(printedCommands(out)) != 0 {
		t.Errorf("without --keep: rc=%d\n%s\n%s", rc, out, errw)
	}
	if _, err := os.Stat(plain); err == nil {
		t.Error("a clean rebase without --keep left its clone")
	}
	if treeDigest(t, f.forkDir) != before {
		t.Fatal("the rebase without --keep wrote into the pack")
	}

	// WITH IT, the clone kept and the export printed.
	dir := filepath.Join(t.TempDir(), "kept clone")
	rc, out, errw = rebaseVerb(t, "forkpack/tool", "--pack", f.forkDir, "--onto", "v1.1.0", "--into", dir, "--keep")
	if rc != 0 {
		t.Fatalf("--keep rc=%d\n%s\n%s", rc, out, errw)
	}
	q := shquote.QuoteDisplay
	patches := filepath.Join(f.forkDir, "patches")
	for _, w := range []string{
		"fork forkpack/tool: upstream v1.1.0 (" + shortSHA(v11) + ") takes the series as it stands (2 patches",
		"; --keep rebased it all the same\n",
		"  the clone is kept in " + dir + ", the series rebased onto upstream v1.1.0 (" + shortSHA(v11) + ")\n",
		"  the rebase there is done\n",
		"mkdir " + q(patches+".new") + " && git -C " + q(dir) + " -c format.noprefix=false format-patch --no-signature " +
			"--suffix=.patch --base=" + v11 + " -o " + q(patches+".new") + " " + v11 + ".." + packsrc.RebaseBranchRef,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("--keep lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "rebase --continue") || strings.Contains(out, "clone is removed") {
		t.Errorf("--keep printed a continue or said the clone is removed:\n%s", out)
	}
	if packsrc.RebaseInProgress(dir) || !(&packsrc.Store{}).RebaseFinished(dir, mustMarker(t, dir)) {
		t.Fatal("the kept clone holds no finished rebase")
	}
	if treeDigest(t, f.forkDir) != before {
		t.Fatal("the rebase with --keep wrote into the pack")
	}

	// RUN AS PRINTED: the exported series is anchored on v1.1.0.
	cmds := printedCommands(out)
	if len(cmds) != 1 {
		t.Fatalf("the printed commands are %q, want the export alone", cmds)
	}
	runPrinted(t, cmds[0])
	series, err := packsrc.ReadSeries(f.forkDir, "patches")
	if err != nil || series.Base != v11 || series.Len() != 2 {
		t.Fatalf("the exported series: %v, want 2 members at v1.1.0 (%+v)", err, series)
	}
	data, _ := os.ReadFile(filepath.Join(patches, series.Members[0].Name))
	if !strings.Contains(string(data), "base-commit: "+v11) {
		t.Errorf("the exported member names no base-commit %s:\n%s", v11, data)
	}
}

// --keep is a flag of its own, kept on the host line a jail's refusal names.
func TestPackRebaseKeepIsParsedAndCarried(t *testing.T) {
	ra, rc := parseRebaseArgs([]string{"forkpack/tool", "--keep", "--into", "d"}, new(strings.Builder))
	if rc != 0 || !ra.keep || ra.into != "d" {
		t.Fatalf("parse: rc=%d %+v", rc, ra)
	}
	if got := rebaseHostLine(ra); got != "yolo pack rebase forkpack/tool --into d --keep" {
		t.Errorf("the host line is %q", got)
	}
}
